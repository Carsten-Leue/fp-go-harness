package cli

import (
	"context"
	"fmt"
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
// of the configuration. Background commands log to [logDir] inside it.
func makeToolsDeps(cfg Config) io.IO[tools.ToolsDeps] {
	root := MakeConfigWorkspaceLens().Get(cfg)

	return tools.MakeDefaultToolsDeps(root, filepath.Join(root, logDir))
}

// makeRegistry is wiring: it builds the registry of the tools of a mode, with
// the output of each tool limited to the MaxToolOutput of the configuration.
func makeRegistry(cfg Config, m mode.Mode) reader.Reader[tools.ToolsDeps, tools.ToolRegistry] {
	return F.Flow2(
		mode.Tools()(m),
		tools.TruncateTools()(MakeConfigMaxToolOutputLens().Get(cfg)),
	)
}

// withPrompt adds the user's prompt to the request of a mode and sets the
// model.
func withPrompt() func(string) endomorphism.Endomorphism[openai.ChatCompletionNewParams] {
	modelLens := oai.MakeChatCompletionNewParamsModelLens()
	messagesLens := oai.MakeChatCompletionNewParamsMessagesLens()

	return F.Flow4(
		openai.UserMessage[string],
		A.Push[openai.ChatCompletionMessageParamUnion],
		messagesLens.Modify,
		reader.Map[openai.ChatCompletionNewParams](modelLens.Set(oai.DeepSeekModelFlash)),
	)
}

// answer is wiring: it runs the agent loop for a request against DeepSeek,
// with the registry the program built and at most MaxIterations requests.
// A one-shot prompt has no way to steer a running turn, hence
// [session.NoSteering].
//
// session.Run is reused unchanged: [effect.Local] widens it from SessionDeps to
// the provider's ChatCompletionDeps, and [effect.LocalEffectK] builds those
// from DeepSeekDeps with the effectful [oai.MakeDeepSeekChatCompletionDeps].
func answer(cfg Config, registry tools.ToolRegistry) effect.Kleisli[oai.DeepSeekDeps, openai.ChatCompletionNewParams, session.TurnResult] {
	toSessionDeps := F.Bind234of4(session.MakeSessionDeps)(
		tools.MakeRegistryToolDeps()(registry),
		session.MakeLoopDeps(MakeConfigMaxIterationsLens().Get(cfg)),
		session.MakeSteeringDeps(session.NoSteering()),
	)

	return F.Flow4(
		session.MakeSession,
		session.Run(),
		effect.Local[session.TurnResult](toSessionDeps),
		effect.LocalEffectK[session.TurnResult](F.Constant1[oai.DeepSeekDeps](oai.MakeDeepSeekChatCompletionDeps())),
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

// withDetails adds a prompt, wrapped by the environment_details of a turn, to
// a request. It is named because [effect.Of] cannot infer it.
type withDetails = func(endomorphism.Endomorphism[string]) endomorphism.Endomorphism[openai.ChatCompletionNewParams]

// ask is wiring: it answers a prompt in a mode, in the workspace of the tools.
// The request of the mode reads its system prompt from that workspace, and
// the prompt is sent with the environment_details of the mode and workspace:
// the prompt's change to the request is applied to the details, then to the
// request, both read concurrently. After a cancelled turn the background
// processes of the tools are stopped. The context is cancelled by then, so the
// tap is one that does not check it (readerreaderioeither's TapIOK, same type
// as [effect.Effect]).
func ask(cfg Config, m mode.Mode, deps mode.ModeDeps) effect.Kleisli[oai.DeepSeekDeps, string, session.TurnResult] {
	modeDeps := F.Constant1[oai.DeepSeekDeps](deps)
	nameLens := mode.MakeModeNameLens()

	request := F.Pipe1(
		mode.Request()(m),
		effect.Local[openai.ChatCompletionNewParams](modeDeps),
	)

	details := F.Pipe2(
		nameLens.Get(m),
		prompt.EnvironmentDetails(),
		effect.Local[endomorphism.Endomorphism[string]](F.Flow2(modeDeps, prompt.AsEnvDetailsDeps[mode.ModeDeps])),
	)

	return F.Flow7(
		reader.Read[string, string],
		reader.Map[endomorphism.Endomorphism[string]](withPrompt()),
		effect.Of[oai.DeepSeekDeps, withDetails],
		effect.Ap[endomorphism.Endomorphism[openai.ChatCompletionNewParams]](details),
		effect.Ap[openai.ChatCompletionNewParams](request),
		effect.Chain(answer(cfg, makeRegistry(cfg, m)(deps))),
		RRIOE.TapIOK[oai.DeepSeekDeps, context.Context, error](stopWhenCancelled()(deps.GetProcessTable())),
	)
}

// modeDirs is wiring: the directories of the custom modes, lowest
// precedence first: the home directory's, then the workspace's.
func modeDirs(home, root string) []string {
	return A.From(filepath.Join(home, mode.ModesDir), filepath.Join(root, mode.ModesDir))
}

// readModes is wiring: it reads the table of the modes for a workspace root:
// the built-in modes, overridden by those of the home directory, overridden
// by those of the workspace.
func readModes(root string) IOResult[[]mode.Mode] {
	return F.Pipe2(
		ioresult.Eitherize0(os.UserHomeDir)(),
		ioresult.Map(F.Bind2nd(modeDirs, root)),
		ioresult.Chain(mode.ReadModeDirs()(os.DirFS)),
	)
}

// makeModeDeps is wiring: it builds the deps of the modes from the deps of
// the tools and the table of the modes of the workspace, read concurrently.
func makeModeDeps(cfg Config) IOResult[mode.ModeDeps] {
	return F.Pipe2(
		ioresult.FromIO(makeToolsDeps(cfg)),
		ioresult.Map(F.Curry2(mode.MakeDefaultModeDeps)),
		ioresult.Ap[mode.ModeDeps](readModes(MakeConfigWorkspaceLens().Get(cfg))),
	)
}

// modeAnswer is wiring: it looks the mode of the configuration up in the
// table of the deps and builds the agent loop of that mode over them. The
// deps are read twice: for the lookup and for the agent loop.
func modeAnswer(cfg Config) result.Kleisli[mode.ModeDeps, effect.Kleisli[oai.DeepSeekDeps, string, session.TurnResult]] {
	return F.Pipe2(
		F.Flip(F.Curry2(F.Bind1of3(ask)(cfg))),
		reader.Map[mode.ModeDeps](result.Map[mode.Mode, effect.Kleisli[oai.DeepSeekDeps, string, session.TurnResult]]),
		reader.Ap[Result[effect.Kleisli[oai.DeepSeekDeps, string, session.TurnResult]]](F.Flow3(
			mode.AsModeTableDeps[mode.ModeDeps],
			mode.Lookup(),
			reader.Read[Result[mode.Mode]](MakeConfigModeLens().Get(cfg)),
		)),
	)
}

// makeAnswer is wiring: it reads the modes, looks the mode of the
// configuration up and builds the agent loop of that mode.
func makeAnswer(cfg Config) IOResult[effect.Kleisli[oai.DeepSeekDeps, string, session.TurnResult]] {
	return F.Pipe1(
		makeModeDeps(cfg),
		ioresult.ChainResultK(modeAnswer(cfg)),
	)
}

// makeProgram is wiring: it decodes the [Config] from the variables of a run
// and returns the agent loop of its mode, with the DeepSeek deps reading
// their API key from the same variables.
func makeProgram(vars Record[string, string]) IOResult[func(string) ReaderIOResult[session.TurnResult]] {
	provide := F.Pipe2(
		vars,
		env.MakeVarsEnvironmentDeps(),
		F.Flow2(newDeepSeekDeps, effect.Provide[session.TurnResult, oai.DeepSeekDeps]),
	)

	return F.Pipe3(
		decodeConfig()(vars),
		ioresult.FromResult[Config],
		ioresult.Chain(makeAnswer),
		ioresult.Map(reader.Map[string](provide)),
	)
}

// promptOf reads the prompt from the arguments of the command, all of them
// joined by blanks. An empty prompt fails.
func promptOf() result.Kleisli[*C.Command, string] {
	return F.Flow5(
		(*C.Command).Args,
		C.Args.Slice,
		F.Bind2nd(strings.Join, " "),
		option.FromNonZero[string](),
		result.FromOption[string](ER.OnNone("missing prompt, usage: fp-go-harness [--mode agent|plan|ask] <prompt>")),
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
			ioresult.Chain(makeProgram),
		)),
		reader.Map[*C.Command](F.Flow2(
			thunk.FromIOResult[ReaderIOResult[session.TurnResult]],
			thunk.Flatten[session.TurnResult],
		)),
	)
}

// printLine is a leaf: [fmt.Fprintln] is variadic. It writes to the writer of
// the root command, the standard output unless a test replaces it.
func printLine(cmd *C.Command, s string) (int, error) {
	return fmt.Fprintln(cmd.Root().Writer, s)
}

// printErrLine is a leaf: [fmt.Fprintln] is variadic. It writes to the error
// writer of the root command, the standard error unless a test replaces it.
func printErrLine(cmd *C.Command, s string) (int, error) {
	return fmt.Fprintln(cmd.Root().ErrWriter, s)
}

// printOut prints a line on the standard output of a command.
func printOut() Reader[*C.Command, ioresult.Kleisli[string, int]] {
	return F.Curry2(ioresult.Eitherize2(printLine))
}

// printErr prints a line on the standard error of a command.
func printErr() Reader[*C.Command, ioresult.Kleisli[string, int]] {
	return F.Curry2(ioresult.Eitherize2(printErrLine))
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

// printErrorLine is a leaf: [fmt.Fprintf] is variadic. It writes an error to
// the error writer of the root command; %+v lists each error of a failed
// decoding with its value.
func printErrorLine(cmd *C.Command, err error) (int, error) {
	return fmt.Fprintf(cmd.Root().ErrWriter, "%+v\n", err)
}

// printError prints an error on the standard error of a command.
func printError() Reader[*C.Command, io.Kleisli[error, Result[int]]] {
	return F.Curry2(ioresult.Eitherize2(printErrorLine))
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

// joinParagraphs is a leaf: one formatter over the statistics and the content.
func joinParagraphs(stats, content string) string {
	return content + "\n\n" + stats
}

// formatResult renders the final assistant message followed by the session
// statistics.
func formatResult() func(session.FinalResult) string {
	return F.Flow2(
		pair.BiMap(sessionStats, finalContent()),
		pair.Paired(joinParagraphs),
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
