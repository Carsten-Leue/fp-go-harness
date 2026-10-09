package tools

import (
	"context"
	"errors"
	"strings"
	"time"

	A "github.com/IBM/fp-go/v2/array"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/endomorphism"
	"github.com/IBM/fp-go/v2/eq"
	ER "github.com/IBM/fp-go/v2/errors"
	F "github.com/IBM/fp-go/v2/function"
	J "github.com/IBM/fp-go/v2/json"
	N "github.com/IBM/fp-go/v2/number"
	"github.com/IBM/fp-go/v2/optics/iso"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/ord"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	openai "github.com/openai/openai-go/v3"
)

// ExecuteCommandName is the name under which the model calls the
// execute_command tool.
const ExecuteCommandName = "execute_command"

const (
	// defaultTimeoutSeconds applies when a call sets no valid timeout.
	defaultTimeoutSeconds = 30
	// maxTimeoutSeconds caps the timeout a call may set.
	maxTimeoutSeconds = 270
)

// noOutput is the result of a successful command that printed nothing.
const noOutput = "(no output)"

var errEmptyCommand = errors.New("command must not be empty")

// ExecuteCommandDeps is what execute_command needs: the workspace that is
// the default working directory and the processes it runs.
type ExecuteCommandDeps interface {
	WorkspaceDeps
	ProcessDeps
}

type executeCommandDeps struct {
	WorkspaceDeps
	ProcessDeps
}

func MakeExecuteCommandDeps(ws WorkspaceDeps, p ProcessDeps) ExecuteCommandDeps {
	return &executeCommandDeps{ws, p}
}

func AsExecuteCommandDeps[T ExecuteCommandDeps](r T) ExecuteCommandDeps {
	return r
}

// ExecuteCommandArgs are the arguments of an execute_command call. Dir is
// not part of the call; it is filled in while the call runs.
//
// fp-go:Lens
type ExecuteCommandArgs struct {
	Command        string `json:"command"`
	Cwd            string `json:"cwd"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	Dir            string `json:"-"`
}

// timeoutOf turns the timeout argument into a duration: a positive value is
// capped at 270 seconds, anything else is 30 seconds.
func timeoutOf() func(int) time.Duration {
	return F.Flow2(
		P.Fold(
			F.Constant1[int](defaultTimeoutSeconds),
			ord.Clamp(ord.FromStrictCompare[int]())(0, maxTimeoutSeconds),
		)(N.MoreThan(0)),
		iso.SecondsDuration().Get,
	)
}

// isDeadlineExceeded tests for the error of a context that ran out of time.
func isDeadlineExceeded() P.Predicate[error] {
	return F.Bind2nd(errors.Is, context.DeadlineExceeded)
}

// deadlineError is a leaf: [ER.OnError] is variadic and needs the duration
// as argument.
func deadlineError(d time.Duration) Endomorphism[error] {
	return ER.OnError("command did not finish within %s", d)
}

// timeoutError names the time limit in the error of a command that hit it,
// and leaves every other error alone.
func timeoutError() Reader[time.Duration, Endomorphism[error]] {
	return F.Flow3(
		deadlineError,
		F.Bind1st(P.Fold[error, error], ER.Identity),
		reader.Read[Endomorphism[error]](isDeadlineExceeded()),
	)
}

// withTimeLimit runs a command under the timeout of the call, in a context
// derived inside the thunk, so that the limit starts when the command does.
// Both stages are endomorphisms read from the duration; the monoid composes
// them right to left, so the timeout applies first and its error is renamed.
func withTimeLimit() Reader[int, thunk.Operator[ProcessOutput, ProcessOutput]] {
	compose := reader.ApplicativeMonoid[time.Duration](endomorphism.Monoid[Thunk[ProcessOutput]]())

	return F.Flow2(
		timeoutOf(),
		compose.Concat(
			F.Flow2(timeoutError(), thunk.MapLeft[ProcessOutput]),
			thunk.WithTimeout[ProcessOutput],
		),
	)
}

// workingDir reads the workspace root and resolves the cwd argument against
// it; an empty one is the root. Unlike the file tools, a directory outside
// the workspace is allowed, as in the recordings: the command line could
// change to it anyway.
func workingDir() effect.Kleisli[ExecuteCommandDeps, string, string] {
	return F.Flow2(
		F.Flip(F.Flow2(ExecuteCommandDeps.GetWorkspaceRoot, absolutePath())),
		effect.Asks[ExecuteCommandDeps, string],
	)
}

// invokeProcess turns a call into the effect that runs it: it hands the
// command and the working directory to the [RunProcess] it depends on and
// limits the time the process may take.
func invokeProcess() effect.Kleisli[RunProcess, ExecuteCommandArgs, ProcessOutput] {
	commandLens := MakeExecuteCommandArgsCommandLens()
	dirLens := MakeExecuteCommandArgsDirLens()
	timeoutLens := MakeExecuteCommandArgsTimeoutSecondsLens()

	process := F.Pipe1(
		F.Flow2(commandLens.Get, F.Curry2(MakeProcess)),
		reader.Ap[Process](dirLens.Get),
	)

	return F.Pipe1(
		F.Flow3(timeoutLens.Get, withTimeLimit(), reader.Map[RunProcess, Thunk[ProcessOutput], Thunk[ProcessOutput]]),
		reader.Ap[Effect[RunProcess, ProcessOutput]](F.Flow2(process, reader.Read[Thunk[ProcessOutput], Process])),
	)
}

// runCommand runs a call with the [RunProcess] of the [ProcessDeps].
func runCommand() effect.Kleisli[ExecuteCommandDeps, ExecuteCommandArgs, ProcessOutput] {
	return F.Flow2(
		invokeProcess(),
		effect.Local[ProcessOutput](ExecuteCommandDeps.GetRunProcess),
	)
}

// isSuccess is a leaf: a command succeeds with exit code 0.
func isSuccess(o ProcessOutput) bool {
	return o.ExitCode == 0
}

// section renders an output stream under its label, without its trailing
// line breaks. An empty stream has no section.
func section(label string) func(string) Option[string] {
	return F.Flow3(
		F.Bind2nd(strings.TrimRight, "\r\n"),
		option.FromPredicate(S.IsNonEmpty),
		option.Map(S.Prepend(label)),
	)
}

// exitSection names a non-zero exit code.
func exitSection() Reader[ProcessOutput, Option[string]] {
	exitCodeLens := MakeProcessOutputExitCodeLens()

	return F.Flow3(
		exitCodeLens.Get,
		option.FromPredicate(P.Not(eq.Equals(eq.FromStrictEquals[int]())(0))),
		option.Map(S.Format[int]("Exit code: %d")),
	)
}

// stdoutSection renders the standard output: bare for a successful command,
// under "Stdout:" for a failed one, as in the recordings.
func stdoutSection() Reader[ProcessOutput, Option[string]] {
	stdoutLens := MakeProcessOutputStdoutLens()

	label := P.Fold(F.Constant1[ProcessOutput]("Stdout:\n"), F.Constant1[ProcessOutput](""))(isSuccess)

	return F.Pipe1(
		F.Flow2(label, section),
		reader.Ap[Option[string]](stdoutLens.Get),
	)
}

// stderrSection renders the standard error under "Stderr:".
func stderrSection() Reader[ProcessOutput, Option[string]] {
	stderrLens := MakeProcessOutputStderrLens()

	return F.Flow2(stderrLens.Get, section("Stderr:\n"))
}

// outputText renders the exit code, if it is not 0, standard output and
// standard error, each one only when it is not empty.
func outputText() Reader[ProcessOutput, string] {
	return F.Flow4(
		reader.SequenceArray(A.From(exitSection(), stdoutSection(), stderrSection())),
		option.CompactArray[string],
		S.Join("\n\n"),
		P.Fold(F.Identity[string], F.Constant1[string](noOutput))(S.IsEmpty),
	)
}

// renderOutput renders what a command produced. A successful command gives
// the text; a failed one fails with it, so that the model sees it as the
// error of the tool, as in the recordings.
func renderOutput() result.Kleisli[ProcessOutput, string] {
	fail := F.Flow2(errors.New, result.Left[string])

	return F.Pipe1(
		P.Fold(F.Constant1[ProcessOutput](fail), F.Constant1[ProcessOutput](result.Of[string]))(isSuccess),
		reader.Ap[Result[string]](outputText()),
	)
}

// ExecuteCommand runs an execute_command call in the foreground: it decodes
// the JSON arguments, resolves the working directory against the workspace
// (the workspace root when cwd is empty), runs the command line in the shell
// and waits for it, at most timeout_seconds (30 by default, at most 270).
//
// The result is the standard output followed by a "Stderr:" section. A
// non-zero exit code, invalid arguments, a command that cannot be started
// and one that runs out of time fail the effect; [MakeToolCall] turns the
// failure into a tool message for the model.
func ExecuteCommand() effect.Kleisli[ExecuteCommandDeps, string, string] {
	commandLens := MakeExecuteCommandArgsCommandLens()
	cwdLens := MakeExecuteCommandArgsCwdLens()
	dirLens := MakeExecuteCommandArgsDirLens()

	nonEmpty := result.FromPredicate(
		F.Flow2(commandLens.Get, S.IsNonEmpty),
		F.Constant1[ExecuteCommandArgs](errEmptyCommand),
	)

	return F.Flow4(
		F.Flow4(S.ToBytes, J.Unmarshal[ExecuteCommandArgs], result.Chain(nonEmpty), effect.FromResult[ExecuteCommandDeps, ExecuteCommandArgs]),
		effect.Bind(dirLens.Set, F.Flow2(cwdLens.Get, workingDir())),
		effect.Chain(runCommand()),
		effect.ChainResultK[ExecuteCommandDeps](renderOutput()),
	)
}

// executeCommandDefinition describes execute_command to the model, following
// the recorded definition. It leaves out background, which is not supported yet.
func executeCommandDefinition(root string) openai.FunctionDefinitionParam {
	return openai.FunctionDefinitionParam{
		Name:        ExecuteCommandName,
		Description: openai.String("Request to execute a CLI command on the system. Use this when you need to perform system operations or run specific commands to accomplish any step in the user's task. You must tailor your command to the user's system and provide a clear explanation of what the command does. Prefer to execute complex CLI commands over creating executable scripts, as they are more flexible and easier to run. Avoid changing directories inside the command, use the `cwd` parameter instead. For commands known to take longer than the default timeout (e.g. large installs or builds), use the `timeout_seconds` parameter, set to the minimum time reasonably needed, not an arbitrarily large value."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{
					"type":        "string",
					"description": "The CLI command to execute. This should be valid for the current operating system. Ensure the command is properly formatted and does not contain any harmful instructions.",
				},
				"cwd": map[string]any{
					"type":        "string",
					"description": "The working directory to execute the command in (absolute, or relative to the workspace). Omit to use the workspace root (" + root + ").",
				},
				"timeout_seconds": map[string]any{
					"type":        "integer",
					"description": "Override the default 30s timeout for known slow commands (e.g. large installs, builds, test suites). Must be set to the minimum time reasonably needed, do not set arbitrarily large values. Maximum allowed: 270s. Invalid or missing values default to 30s.",
				},
			},
			"required": []string{"command"},
		},
	}
}

// MakeExecuteCommandTool builds the execute_command [Tool] over the given
// dependencies.
func MakeExecuteCommandTool() Reader[ExecuteCommandDeps, Tool] {
	return F.Pipe2(
		F.Flow2(ExecuteCommandDeps.GetWorkspaceRoot, executeCommandDefinition),
		reader.Map[ExecuteCommandDeps](F.Curry2(MakeTool)),
		reader.Ap[Tool](F.Flip(ExecuteCommand())),
	)
}
