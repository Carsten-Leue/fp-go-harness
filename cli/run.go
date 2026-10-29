package cli

import (
	"context"
	"fmt"
	goio "io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Carsten-Leue/fp-go-harness/env"
	"github.com/Carsten-Leue/fp-go-harness/http"
	"github.com/Carsten-Leue/fp-go-harness/mode"
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/prompt"
	"github.com/Carsten-Leue/fp-go-harness/session"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/either"
	"github.com/IBM/fp-go/v2/endomorphism"
	ER "github.com/IBM/fp-go/v2/errors"
	"github.com/IBM/fp-go/v2/file"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/ioresult"
	P "github.com/IBM/fp-go/v2/optics/prism"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	RRIOE "github.com/IBM/fp-go/v2/readerreaderioeither"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	C "github.com/urfave/cli/v3"
)

// logDir is where background commands write their output, relative to the
// workspace: read_file only reads inside the workspace.
var logDir = filepath.Join(".fp-go-harness", "logs")

type deepSeekDeps struct {
	http.HttpDeps
	env.EnvironmentDeps
}

// newDeepSeekDeps is wiring: it combines the default HTTP client and the
// environment into [oai.DeepSeekDeps].
func newDeepSeekDeps(e env.EnvironmentDeps) oai.DeepSeekDeps {
	return &deepSeekDeps{http.MakeDefaultHttpDeps(), e}
}

// makeToolsDeps is wiring: it builds the deps of the tools for the workspace
// of a configuration. Background commands log to [logDir] inside it, so the
// workspace is read twice: as the root and for the log directory.
func makeToolsDeps() Reader[Config, io.IO[tools.ToolsDeps]] {
	workspaceLens := MakeConfigWorkspaceLens()

	return F.Flow2(
		workspaceLens.Get,
		F.Pipe1(
			F.Curry2(tools.MakeDefaultToolsDeps),
			reader.Ap[io.IO[tools.ToolsDeps]](file.Join(logDir)),
		),
	)
}

// makeRegistry is wiring: it builds the registry of the tools of the mode of
// an environment over its deps, with the output of each tool limited to the
// MaxToolOutput of its configuration.
func makeRegistry() Reader[modeEnv, tools.ToolRegistry] {
	configLens := MakemodeEnvConfigLens()
	modeLens := MakemodeEnvModeLens()
	depsLens := MakemodeEnvDepsLens()
	maxToolOutputLens := MakeConfigMaxToolOutputLens()

	registry := F.Pipe1(
		F.Flow2(modeLens.Get, mode.Tools()),
		reader.Ap[tools.ToolRegistry](F.Flow2(depsLens.Get, tools.AsToolsDeps[mode.ModeDeps])),
	)

	return F.Pipe1(
		F.Flow3(configLens.Get, maxToolOutputLens.Get, tools.TruncateTools()),
		reader.Ap[tools.ToolRegistry](registry),
	)
}

// withModel sets the model of a request.
func withModel() endomorphism.Endomorphism[openai.ChatCompletionNewParams] {
	modelLens := oai.MakeChatCompletionNewParamsModelLens()

	return modelLens.Set(oai.DeepSeekModelFlash)
}

// withPrompt adds the user's prompt to the request of a mode and sets the
// model.
func withPrompt() func(string) endomorphism.Endomorphism[openai.ChatCompletionNewParams] {
	messagesLens := oai.MakeChatCompletionNewParamsMessagesLens()

	return F.Flow4(
		openai.UserMessage[string],
		A.Push[openai.ChatCompletionMessageParamUnion],
		messagesLens.Modify,
		reader.Map[openai.ChatCompletionNewParams](withModel()),
	)
}

// toSessionDeps is the change of a provider's ChatCompletionDeps to the
// [session.SessionDeps] of a turn.
type toSessionDeps = Reader[oai.ChatCompletionDeps, session.SessionDeps]

// sessionDeps is wiring: the [session.SessionDeps] of a turn of the mode of an
// environment over any provider's ChatCompletionDeps: the registry of
// [makeRegistry], at most MaxIterations requests and the user messages of a
// poll steering the turn.
func sessionDeps() Reader[modeEnv, Reader[session.Thunk[[]string], toSessionDeps]] {
	configLens := MakemodeEnvConfigLens()
	maxIterationsLens := MakeConfigMaxIterationsLens()

	return F.Pipe2(
		F.Flow3(
			makeRegistry(),
			tools.MakeRegistryToolDeps(),
			F.Curry3(F.Bind234of4(session.MakeSessionDeps)),
		),
		reader.Ap[func(session.SteeringDeps) toSessionDeps](F.Flow3(configLens.Get, maxIterationsLens.Get, session.MakeLoopDeps)),
		reader.Map[modeEnv](reader.Local[toSessionDeps](session.MakeSteeringDeps)),
	)
}

// runSession is wiring: one turn of a session of the mode of an environment,
// steered by the user messages of a poll, against any provider's
// ChatCompletionDeps, see [sessionDeps].
//
// session.RunPrompt is reused unchanged: [effect.Local] widens it from
// SessionDeps to the provider's ChatCompletionDeps.
func runSession() Reader[modeEnv, Reader[session.Thunk[[]string], effect.Kleisli[oai.ChatCompletionDeps, session.Session, session.TurnResult]]] {
	return F.Pipe1(
		sessionDeps(),
		reader.Map[modeEnv](reader.Map[session.Thunk[[]string]](F.Flow2(
			effect.Local[session.TurnResult, oai.ChatCompletionDeps, session.SessionDeps],
			reader.Compose[Effect[oai.ChatCompletionDeps, session.TurnResult]](session.RunPrompt()),
		))),
	)
}

// answer is wiring: it runs the agent loop for a request against DeepSeek,
// see [runSession]. A one-shot prompt has no way to steer a running turn,
// hence [session.NoSteering]. [effect.LocalEffectK] builds the
// ChatCompletionDeps from DeepSeekDeps with the effectful
// [oai.MakeDeepSeekChatCompletionDeps].
func answer() Reader[modeEnv, effect.Kleisli[oai.DeepSeekDeps, openai.ChatCompletionNewParams, session.TurnResult]] {
	return F.Pipe1(
		runSession(),
		reader.Map[modeEnv](F.Flow3(
			reader.Read[effect.Kleisli[oai.ChatCompletionDeps, session.Session, session.TurnResult]](session.NoSteering()),
			reader.Local[Effect[oai.ChatCompletionDeps, session.TurnResult]](session.MakeSession),
			reader.Map[openai.ChatCompletionNewParams](effect.LocalEffectK[session.TurnResult](F.Constant1[oai.DeepSeekDeps](oai.MakeDeepSeekChatCompletionDeps()))),
		)),
	)
}

// isCancelledTurn matches the turns that a cancellation ended.
func isCancelledTurn() predicate.Predicate[session.TurnResult] {
	return F.Flow2(
		pair.Tail[session.Session, session.Outcome],
		result.Fold(session.IsCancelled(), F.Constant1[*openai.ChatCompletion](false)),
	)
}

// stopWhenCancelled stops the background processes of a table after a
// cancelled turn, see [tools.StopProcesses], and does nothing after any
// other turn.
func stopWhenCancelled() reader.Reader[tools.IORef[tools.ProcessTable], io.Kleisli[session.TurnResult, F.Void]] {
	return F.Flow4(
		tools.StopProcesses(),
		F.Constant1[session.TurnResult, io.IO[F.Void]],
		F.Curry2(predicate.Fold[session.TurnResult, io.IO[F.Void]])(F.Constant1[session.TurnResult](io.Of(F.VOID))),
		reader.Read[io.Kleisli[session.TurnResult, F.Void]](isCancelledTurn()),
	)
}

// stopProcesses is the tap that stops the background processes of the tools
// of an environment after a cancelled turn, see [stopWhenCancelled], under
// any environment T. The context is cancelled by then, so the tap is one
// that does not check it (readerreaderioeither's TapIOK, same type as
// [effect.Effect]).
func stopProcesses[T any]() Reader[modeEnv, endomorphism.Endomorphism[Effect[T, session.TurnResult]]] {
	depsLens := MakemodeEnvDepsLens()

	return F.Flow4(
		depsLens.Get,
		mode.ModeDeps.GetProcessTable,
		stopWhenCancelled(),
		RRIOE.TapIOK[T, context.Context, error, session.TurnResult, F.Void],
	)
}

// withDetails adds a prompt, wrapped by the environment_details of a turn, to
// a request. It is named because [effect.Of] cannot infer it.
type withDetails = func(endomorphism.Endomorphism[string]) endomorphism.Endomorphism[openai.ChatCompletionNewParams]

// modeRequest is wiring: the request the mode of an environment starts with,
// its system prompt read from the workspace of the deps, under any
// environment T, which it ignores.
func modeRequest[T any]() Reader[modeEnv, Effect[T, openai.ChatCompletionNewParams]] {
	modeLens := MakemodeEnvModeLens()
	depsLens := MakemodeEnvDepsLens()

	return F.Pipe1(
		F.Flow3(
			depsLens.Get,
			F.Constant1[T, mode.ModeDeps],
			effect.Local[openai.ChatCompletionNewParams, T, mode.ModeDeps],
		),
		reader.Ap[Effect[T, openai.ChatCompletionNewParams]](F.Flow2(modeLens.Get, mode.Request())),
	)
}

// modeDetails is wiring: the environment_details of the mode and the
// workspace of an environment, which wrap the prompt of a turn, under any
// environment T, which it ignores.
func modeDetails[T any]() Reader[modeEnv, Effect[T, endomorphism.Endomorphism[string]]] {
	modeLens := MakemodeEnvModeLens()
	depsLens := MakemodeEnvDepsLens()
	nameLens := mode.MakeModeNameLens()

	return F.Pipe1(
		F.Flow4(
			depsLens.Get,
			prompt.AsEnvDetailsDeps[mode.ModeDeps],
			F.Constant1[T, prompt.EnvDetailsDeps],
			effect.Local[endomorphism.Endomorphism[string], T, prompt.EnvDetailsDeps],
		),
		reader.Ap[Effect[T, endomorphism.Endomorphism[string]]](F.Flow3(modeLens.Get, nameLens.Get, prompt.EnvironmentDetails())),
	)
}

// The values and stages of [ask]: an effect against DeepSeek, the request,
// and the prompt's change to it.
type (
	askEffect[T any] = Effect[oai.DeepSeekDeps, T]
	askParams        = openai.ChatCompletionNewParams
	askChange        = endomorphism.Endomorphism[askParams]
)

// ask is wiring: it answers a prompt in the mode of an environment, in the
// workspace of its tools. The request of the mode reads its system prompt
// from that workspace, and the prompt is sent with the environment_details of
// the mode and workspace: the prompt's change to the request is applied to
// the details, then to the request, both read concurrently. After a cancelled
// turn the background processes of the tools are stopped, see
// [stopProcesses].
//
// Each stage reads the environment; they are composed in data-flow order:
// the change of the prompt, the details, the request, the turn and the tap.
func ask() Reader[modeEnv, effect.Kleisli[oai.DeepSeekDeps, string, session.TurnResult]] {
	start := reader.Of[modeEnv](F.Flow3(
		reader.Read[string, string],
		reader.Map[endomorphism.Endomorphism[string]](withPrompt()),
		effect.Of[oai.DeepSeekDeps, withDetails],
	))

	return F.Pipe8(
		start,
		reader.Map[modeEnv](reader.Compose[askEffect[askChange], string, askEffect[withDetails]]),
		reader.Ap[func(string) askEffect[askChange]](F.Flow2(
			modeDetails[oai.DeepSeekDeps](),
			effect.Ap[askChange, oai.DeepSeekDeps, endomorphism.Endomorphism[string]],
		)),
		reader.Map[modeEnv](reader.Compose[askEffect[askParams], string, askEffect[askChange]]),
		reader.Ap[func(string) askEffect[askParams]](F.Flow2(
			modeRequest[oai.DeepSeekDeps](),
			effect.Ap[askParams, oai.DeepSeekDeps, askParams],
		)),
		reader.Map[modeEnv](reader.Compose[askEffect[session.TurnResult], string, askEffect[askParams]]),
		reader.Ap[func(string) askEffect[session.TurnResult]](F.Flow2(
			answer(),
			effect.Chain[oai.DeepSeekDeps, askParams, session.TurnResult],
		)),
		reader.Map[modeEnv](reader.Compose[askEffect[session.TurnResult], string, askEffect[session.TurnResult]]),
		reader.Ap[func(string) askEffect[session.TurnResult]](stopProcesses[oai.DeepSeekDeps]()),
	)
}

// modesDirOf is the directory of the custom modes in a directory, as a slice
// of one.
func modesDirOf() A.Kleisli[string, string] {
	return F.Flow2(file.Join(mode.ModesDir), A.Of[string])
}

// modeDirs is wiring: the directories of the custom modes of a workspace root
// and a home directory, lowest precedence first: the home directory's, then
// the workspace's.
func modeDirs() Reader[string, A.Kleisli[string, string]] {
	return F.Flow3(
		modesDirOf(),
		A.Concat[string],
		reader.Local[[]string](modesDirOf()),
	)
}

// readModes is wiring: it reads the table of the modes for a workspace root:
// the built-in modes, overridden by those of the home directory, overridden
// by those of the workspace.
func readModes() ioresult.Kleisli[string, []mode.Mode] {
	return F.Flow4(
		modeDirs(),
		ioresult.Map[string, []string],
		reader.Read[IOResult[[]string]](ioresult.Eitherize0(os.UserHomeDir)()),
		ioresult.Chain(mode.ReadModeDirs()(os.DirFS)),
	)
}

// makeModeDeps is wiring: it builds the deps of the modes of a configuration
// from the deps of the tools and the table of the modes of the workspace,
// read concurrently.
func makeModeDeps() ioresult.Kleisli[Config, mode.ModeDeps] {
	workspaceLens := MakeConfigWorkspaceLens()

	return F.Pipe1(
		F.Flow3(workspaceLens.Get, readModes(), ioresult.Ap[mode.ModeDeps, []mode.Mode]),
		reader.Ap[IOResult[mode.ModeDeps]](F.Flow3(
			makeToolsDeps(),
			ioresult.FromIO[tools.ToolsDeps],
			ioresult.Map(F.Curry2(mode.MakeDefaultModeDeps)),
		)),
	)
}

// lookupMode is wiring: the mode of a configuration, looked up in the table
// of the deps.
func lookupMode() Reader[Config, result.Kleisli[mode.ModeDeps, mode.Mode]] {
	modeLens := MakeConfigModeLens()

	return F.Flow2(
		modeLens.Get,
		F.Flip(F.Flow2(mode.AsModeTableDeps[mode.ModeDeps], mode.Lookup())),
	)
}

// modeEnvOf is wiring: the [modeEnv] of a configuration over the deps of the
// modes, with the mode of the configuration looked up in them. The
// configuration and the deps are read twice: for the lookup and for the
// environment.
func modeEnvOf() Reader[Config, result.Kleisli[mode.ModeDeps, modeEnv]] {
	return F.Pipe1(
		F.Flow2(lookupMode(), reader.Ap[Result[modeEnv], mode.ModeDeps, Result[mode.Mode]]),
		reader.Ap[result.Kleisli[mode.ModeDeps, modeEnv]](F.Flow2(
			F.Curry3(newModeEnv),
			reader.Map[mode.ModeDeps](result.Map[mode.Mode, modeEnv]),
		)),
	)
}

// makeModeEnv is wiring: it reads the modes of a configuration and builds its
// [modeEnv], see [modeEnvOf].
func makeModeEnv() ioresult.Kleisli[Config, modeEnv] {
	return F.Pipe1(
		F.Flow2(modeEnvOf(), ioresult.ChainResultK[mode.ModeDeps, modeEnv]),
		reader.Ap[IOResult[modeEnv]](makeModeDeps()),
	)
}

// makeAnswer is wiring: it reads the modes, looks the mode of a configuration
// up and builds the agent loop of that mode.
func makeAnswer() ioresult.Kleisli[Config, effect.Kleisli[oai.DeepSeekDeps, string, session.TurnResult]] {
	return F.Flow2(makeModeEnv(), ioresult.Map(ask()))
}

// provideDeepSeek is wiring: it runs an effect against DeepSeek, with the
// DeepSeek deps reading their API key from the variables of a run.
func provideDeepSeek[T any]() Reader[Record[string, string], func(Effect[oai.DeepSeekDeps, T]) ReaderIOResult[T]] {
	return F.Flow3(
		env.MakeVarsEnvironmentDeps(),
		newDeepSeekDeps,
		effect.Provide[T, oai.DeepSeekDeps],
	)
}

// makeProgram is wiring: it decodes the [Config] from the variables of a run
// and returns the agent loop of its mode, with the DeepSeek deps reading
// their API key from the same variables, see [provideDeepSeek]. The variables
// are read twice: for the configuration and for the deps.
func makeProgram() ioresult.Kleisli[Record[string, string], func(string) ReaderIOResult[session.TurnResult]] {
	return F.Pipe1(
		F.Flow3(
			provideDeepSeek[session.TurnResult](),
			reader.Map[string, Effect[oai.DeepSeekDeps, session.TurnResult], ReaderIOResult[session.TurnResult]],
			ioresult.Map[effect.Kleisli[oai.DeepSeekDeps, string, session.TurnResult], func(string) ReaderIOResult[session.TurnResult]],
		),
		reader.Ap[IOResult[func(string) ReaderIOResult[session.TurnResult]]](F.Flow3(
			decodeConfig(),
			ioresult.FromResult[Config],
			ioresult.Chain(makeAnswer()),
		)),
	)
}

// argsPrompt reads the prompt from the arguments of the command, all of them
// joined by blanks, None when it is empty.
func argsPrompt() option.Kleisli[*C.Command, string] {
	return F.Flow4(
		(*C.Command).Args,
		C.Args.Slice,
		F.Bind2nd(strings.Join, " "),
		option.FromNonZero[string](),
	)
}

// promptOf reads the prompt from the arguments of the command, see
// [argsPrompt]. An empty prompt fails.
func promptOf() result.Kleisli[*C.Command, string] {
	return F.Flow2(
		argsPrompt(),
		result.FromOption[string](ER.OnNone("missing prompt, usage: fp-go-harness [--mode agent|plan|ask] [--interactive] [<prompt>]")),
	)
}

// runPrompt answers the prompt of the command under the configuration of its
// run. The prompt and the program are read from the command independently.
func runPrompt() Reader[*C.Command, ReaderIOResult[session.TurnResult]] {
	return F.Pipe2(
		F.Flow3(
			promptOf(),
			ioresult.FromResult[string],
			ioresult.Ap[ReaderIOResult[session.TurnResult], string],
		),
		reader.Ap[IOResult[ReaderIOResult[session.TurnResult]]](F.Flow2(
			loadEnv(),
			ioresult.Chain(makeProgram()),
		)),
		reader.Map[*C.Command](F.Flow2(
			thunk.FromIOResult[ReaderIOResult[session.TurnResult]],
			thunk.Flatten[session.TurnResult],
		)),
	)
}

// rootWriter is a leaf: the writer of the root command, the standard output
// unless a test replaces it. [C.Command] has function-typed fields, so the
// lens generator can't build its lenses.
func rootWriter(cmd *C.Command) goio.Writer {
	return cmd.Root().Writer
}

// rootErrWriter is a leaf: the error writer of the root command, the
// standard error unless a test replaces it, see [rootWriter].
func rootErrWriter(cmd *C.Command) goio.Writer {
	return cmd.Root().ErrWriter
}

// writeString writes a string to a writer.
func writeString() Reader[goio.Writer, ioresult.Kleisli[string, int]] {
	return F.Curry2(ioresult.Eitherize2(goio.WriteString))
}

// writeLine writes a string and a line break to a writer.
func writeLine() Reader[goio.Writer, ioresult.Kleisli[string, int]] {
	return F.Flow2(
		writeString(),
		reader.Local[IOResult[int]](S.Append("\n")),
	)
}

// printOut prints a line on the standard output of a command.
func printOut() Reader[*C.Command, ioresult.Kleisli[string, int]] {
	return F.Flow2(rootWriter, writeLine())
}

// printErr prints a line on the standard error of a command.
func printErr() Reader[*C.Command, ioresult.Kleisli[string, int]] {
	return F.Flow2(rootErrWriter, writeLine())
}

// failure is the report of a failed turn and the error that ended it.
type failure = pair.Pair[string, error]

// turnReport is what is printed of a turn: the [failure] of a failed turn, or
// the answer and the statistics of an answered one.
type turnReport = either.Either[failure, string]

// failAfter prints the report of a failure with a printer, then fails with
// its error, whatever the printer returned. The failure is read twice, for
// the report and for the error.
func failAfter() Reader[ioresult.Kleisli[string, int], ioresult.Kleisli[failure, int]] {
	failing := F.Pipe1(
		F.Flow2(pair.Tail[string, error], ioresult.Left[int]),
		reader.Map[failure](io.ChainTo[Result[int], Result[int]]),
	)

	return F.Flow3(
		reader.Local[IOResult[int]](pair.Head[string, error]),
		reader.Ap[IOResult[int], failure, IOResult[int]],
		reader.Read[ioresult.Kleisli[failure, int]](failing),
	)
}

// printTurn prints the report of a turn: a failure on the standard error,
// then it fails with the failure's error; an answer on the standard output.
// Both printers are read from the command.
func printTurn() Reader[*C.Command, ioresult.Kleisli[turnReport, int]] {
	return F.Pipe2(
		F.Flow2(printErr(), failAfter()),
		reader.Map[*C.Command](F.Curry2(either.Fold[failure, string, IOResult[int]])),
		reader.Ap[ioresult.Kleisli[turnReport, int]](printOut()),
	)
}

// reportTurn prints the result of a turn, see [turnText] and [printTurn].
func reportTurn() Reader[*C.Command, ioresult.Kleisli[session.TurnResult, int]] {
	return F.Pipe1(
		printTurn(),
		reader.Map[*C.Command](reader.Local[IOResult[int]](turnText())),
	)
}

// printError prints an error on the standard error of a command; %+v lists
// each error of a failed decoding with its value.
func printError() Reader[*C.Command, io.Kleisli[error, Result[int]]] {
	return F.Flow2(
		printErr(),
		reader.Local[IOResult[int]](S.Format[error]("%+v")),
	)
}

// runReported answers the prompt of the command, and prints the error on the
// standard error when there is no turn to report. The tap does not check the
// context, so the error of a cancelled run is printed too.
func runReported() Reader[*C.Command, ReaderIOResult[session.TurnResult]] {
	return F.Pipe1(
		F.Flow2(printError(), thunk.TapLeftIOK[session.TurnResult, Result[int]]),
		reader.Ap[ReaderIOResult[session.TurnResult]](runPrompt()),
	)
}

// printResult answers the prompt of the command and prints the result. Every
// error is printed once, on the standard error: a turn that failed prints its
// report and fails with its error, a run that failed before its turn prints
// the error.
func printResult() Reader[*C.Command, ReaderIOResult[int]] {
	return F.Pipe1(
		F.Flow2(
			reportTurn(),
			thunk.ChainIOResultK[session.TurnResult, int],
		),
		reader.Ap[ReaderIOResult[int]](runReported()),
	)
}

// finalContent extracts the content of the first choice of a completion, or
// the empty string.
func finalContent() func(*openai.ChatCompletion) string {
	choicesLens := oai.MakeChatCompletionChoicesRefLens()
	messageLens := oai.MakeChatCompletionChoiceMessageLens()
	contentLens := oai.MakeChatCompletionMessageContentLens()

	content := choicesLens.
		ComposePrism(P.Head[openai.ChatCompletionChoice]()).
		ComposeLens(messageLens.Compose(contentLens))

	return F.Flow2(
		content.GetOption,
		option.GetOrElse(F.Constant("")),
	)
}

// assistantText is the text of an assistant message of a request, None for
// any other message and for an assistant message without text.
func assistantText() option.Kleisli[openai.ChatCompletionMessageParamUnion, string] {
	assistantPrism := oai.MakeChatCompletionMessageParamUnionOfAssistantPrism()
	contentLens := oai.MakeChatCompletionAssistantMessageParamContentRefLens()
	textLens := oai.MakeChatCompletionAssistantMessageParamContentUnionOfStringLens()

	return F.Flow3(
		assistantPrism.GetOption,
		option.Map(F.Flow3(contentLens.Get, textLens.Get, F.Bind2nd(param.Opt[string].Or, ""))),
		option.Chain(option.FromNonZero[string]()),
	)
}

// partialAnswer is the text of the last assistant message of a session that
// has text, searched in the completions of its history, then in the messages
// of its request; None when there is none. The session is read twice, for the
// history and for the request.
func partialAnswer() func(session.Session) Option[string] {
	historyLens := session.MakeSessionhistoryLens()
	currentLens := session.MakeSessioncurrentLens()
	messagesLens := oai.MakeChatCompletionNewParamsMessagesLens()

	answers := F.Flow2(
		historyLens.Get,
		A.FilterMap(F.Flow3(
			pair.Tail[openai.ChatCompletionNewParams, *openai.ChatCompletion],
			finalContent(),
			option.FromNonZero[string](),
		)),
	)

	messages := F.Flow3(
		currentLens.Get,
		messagesLens.Get,
		A.FilterMap(assistantText()),
	)

	// the texts of the history first, those of the request last, so the
	// request wins
	return F.Pipe1(
		reader.ApplicativeMonoid[session.Session](A.Monoid[string]()).Concat(answers, messages),
		reader.Map[session.Session](A.Last[string]),
	)
}

// sessionStats renders the iteration count and accumulated token usage of a
// session. It is a leaf: one formatter over several fields.
func sessionStats(s session.Session) string {
	usageLens := session.MakeSessionusageLens()
	iterationsLens := session.MakeSessioniterationsLens()

	usage := usageLens.Get(s)

	return fmt.Sprintf(
		"[iterations: %d, prompt tokens: %d, completion tokens: %d, total tokens: %d]",
		iterationsLens.Get(s),
		usage.PromptTokens,
		usage.CompletionTokens,
		usage.TotalTokens,
	)
}

// paragraphBreak separates the paragraphs of a report.
const paragraphBreak = "\n\n"

// formatResult renders the final assistant message followed by the session
// statistics, one paragraph each.
func formatResult() func(session.FinalResult) string {
	return F.Flow3(
		pair.BiMap(sessionStats, finalContent()),
		pair.Swap[string, string],
		pair.Paired(S.Intersperse(paragraphBreak)),
	)
}

// joinFailure is a leaf: one formatter over the partial answer, the
// statistics and the error, one paragraph each. Without a partial answer its
// paragraph is left out.
func joinFailure(partial Option[string], stats string, err error) string {
	return strings.Join(A.Flatten(A.From(
		A.Of(err.Error()),
		A.FromOption(partial),
		A.Of(stats),
	)), "\n\n")
}

// failureText renders a failed turn from its session: the error, the partial
// answer and the statistics. The session is read twice, for the partial
// answer and for the statistics.
func failureText() Reader[session.Session, func(error) string] {
	return F.Pipe2(
		partialAnswer(),
		reader.Map[session.Session](F.Curry3(joinFailure)),
		reader.Ap[func(error) string](sessionStats),
	)
}

// failureOf pairs the report of a failed turn with the error that ended it.
func failureOf() Reader[session.Session, func(error) failure] {
	return F.Flow3(
		failureText(),
		pair.MapHead[error, error, string],
		reader.Local[failure](pair.Of[error]),
	)
}

// answerOf renders an answered turn: the answer and the statistics.
func answerOf() Reader[session.Session, func(*openai.ChatCompletion) turnReport] {
	return F.Flow2(
		F.Curry2(pair.MakePair[session.Session, *openai.ChatCompletion]),
		reader.Map[*openai.ChatCompletion](F.Flow2(formatResult(), either.Right[failure, string])),
	)
}

// turnText renders a turn by its outcome: the report and the error of a
// failed turn, by one rule whatever ended it, or the answer of an answered
// one. The session is read by both cases.
func turnText() func(session.TurnResult) turnReport {
	return pair.Paired(F.Uncurry2(F.Pipe2(
		F.Flow2(failureOf(), reader.Map[error](either.Left[string, failure])),
		reader.Map[session.Session](F.Curry2(result.Fold[*openai.ChatCompletion, turnReport])),
		reader.Ap[func(session.Outcome) turnReport](answerOf()),
	)))
}
