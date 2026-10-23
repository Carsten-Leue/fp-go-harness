package cli

import (
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
	"github.com/IBM/fp-go/v2/endomorphism"
	ER "github.com/IBM/fp-go/v2/errors"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/ioresult"
	P "github.com/IBM/fp-go/v2/optics/prism"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
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
//
// session.Run is reused unchanged: [effect.Local] widens it from SessionDeps to
// the provider's ChatCompletionDeps, and [effect.LocalEffectK] builds those
// from DeepSeekDeps with the effectful [oai.MakeDeepSeekChatCompletionDeps].
func answer(cfg Config, registry tools.ToolRegistry) effect.Kleisli[oai.DeepSeekDeps, openai.ChatCompletionNewParams, session.FinalResult] {
	toSessionDeps := F.Bind23of3(session.MakeSessionDeps)(
		tools.MakeRegistryToolDeps()(registry),
		session.MakeLoopDeps(MakeConfigMaxIterationsLens().Get(cfg)),
	)

	return F.Flow4(
		session.MakeSession,
		session.Run(),
		effect.Local[session.FinalResult](toSessionDeps),
		effect.LocalEffectK[session.FinalResult](F.Constant1[oai.DeepSeekDeps](oai.MakeDeepSeekChatCompletionDeps())),
	)
}

// withDetails adds a prompt, wrapped by the environment_details of a turn, to
// a request. It is named because [effect.Of] cannot infer it.
type withDetails = func(endomorphism.Endomorphism[string]) endomorphism.Endomorphism[openai.ChatCompletionNewParams]

// ask is wiring: it answers a prompt in a mode, in the workspace of the tools.
// The request of the mode reads its system prompt from that workspace, and
// the prompt is sent with the environment_details of the mode and workspace:
// the prompt's change to the request is applied to the details, then to the
// request, both read concurrently.
func ask(cfg Config, m mode.Mode, deps mode.ModeDeps) effect.Kleisli[oai.DeepSeekDeps, string, session.FinalResult] {
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

	return F.Flow6(
		reader.Read[string, string],
		reader.Map[endomorphism.Endomorphism[string]](withPrompt()),
		effect.Of[oai.DeepSeekDeps, withDetails],
		effect.Ap[endomorphism.Endomorphism[openai.ChatCompletionNewParams]](details),
		effect.Ap[openai.ChatCompletionNewParams](request),
		effect.Chain(answer(cfg, makeRegistry(cfg, m)(deps))),
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
func modeAnswer(cfg Config) result.Kleisli[mode.ModeDeps, effect.Kleisli[oai.DeepSeekDeps, string, session.FinalResult]] {
	return F.Pipe2(
		F.Flip(F.Curry2(F.Bind1of3(ask)(cfg))),
		reader.Map[mode.ModeDeps](result.Map[mode.Mode, effect.Kleisli[oai.DeepSeekDeps, string, session.FinalResult]]),
		reader.Ap[Result[effect.Kleisli[oai.DeepSeekDeps, string, session.FinalResult]]](F.Flow3(
			mode.AsModeTableDeps[mode.ModeDeps],
			mode.Lookup(),
			reader.Read[Result[mode.Mode]](MakeConfigModeLens().Get(cfg)),
		)),
	)
}

// makeAnswer is wiring: it reads the modes, looks the mode of the
// configuration up and builds the agent loop of that mode.
func makeAnswer(cfg Config) IOResult[effect.Kleisli[oai.DeepSeekDeps, string, session.FinalResult]] {
	return F.Pipe1(
		makeModeDeps(cfg),
		ioresult.ChainResultK(modeAnswer(cfg)),
	)
}

// makeProgram is wiring: it decodes the [Config] from the variables of a run
// and returns the agent loop of its mode, with the DeepSeek deps reading
// their API key from the same variables.
func makeProgram(vars Record[string, string]) IOResult[func(string) ReaderIOResult[session.FinalResult]] {
	provide := F.Pipe2(
		vars,
		env.MakeVarsEnvironmentDeps(),
		F.Flow2(newDeepSeekDeps, effect.Provide[session.FinalResult, oai.DeepSeekDeps]),
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
func runPrompt() Reader[*C.Command, ReaderIOResult[session.FinalResult]] {
	return F.Pipe2(
		F.Flow3(
			promptOf(),
			ioresult.FromResult[string],
			ioresult.Ap[ReaderIOResult[session.FinalResult], string],
		),
		reader.Ap[IOResult[ReaderIOResult[session.FinalResult]]](F.Flow2(
			loadEnv(),
			ioresult.Chain(makeProgram),
		)),
		reader.Map[*C.Command](F.Flow2(
			thunk.FromIOResult[ReaderIOResult[session.FinalResult]],
			thunk.Flatten[session.FinalResult],
		)),
	)
}

// printLine is a leaf: [fmt.Fprintln] is variadic. It writes to the writer of
// the root command, the standard output unless a test replaces it.
func printLine(cmd *C.Command, s string) (int, error) {
	return fmt.Fprintln(cmd.Root().Writer, s)
}

// printResult answers the prompt of the command and prints the result.
func printResult() Reader[*C.Command, ReaderIOResult[int]] {
	return F.Pipe1(
		F.Flow2(
			F.Curry2(ioresult.Eitherize2(printLine)),
			thunk.ChainIOResultK[string, int],
		),
		reader.Ap[ReaderIOResult[int]](F.Flow2(
			runPrompt(),
			thunk.Map(formatResult()),
		)),
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
