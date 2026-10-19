package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Carsten-Leue/fp-go-harness/env"
	"github.com/Carsten-Leue/fp-go-harness/http"
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/session"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
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

// makeRegistry is wiring: it builds the registry of a mode's tools for the
// workspace of the configuration, with the output of each tool limited to its
// MaxToolOutput. Background commands log to [logDir] inside the workspace.
func makeRegistry(cfg Config, modeTools reader.Reader[tools.ToolsDeps, tools.ToolRegistry]) io.IO[tools.ToolRegistry] {
	root := MakeConfigWorkspaceLens().Get(cfg)

	return F.Pipe1(
		tools.MakeDefaultToolsDeps(root, filepath.Join(root, logDir)),
		io.Map(F.Flow2(modeTools, tools.TruncateTools()(MakeConfigMaxToolOutputLens().Get(cfg)))),
	)
}

// makeRequest builds the request of a mode for a single user prompt,
// including the tool definitions from the registry.
func makeRequest(request openai.ChatCompletionNewParams, registry tools.ToolRegistry) func(string) openai.ChatCompletionNewParams {
	modelLens := oai.MakeChatCompletionNewParamsModelLens()
	messagesLens := oai.MakeChatCompletionNewParamsMessagesLens()

	base := F.Pipe2(
		request,
		modelLens.Set(oai.DeepSeekModelFlash),
		tools.WithTools()(registry),
	)

	return F.Flow4(
		openai.UserMessage[string],
		A.Push[openai.ChatCompletionMessageParamUnion],
		messagesLens.Modify,
		reader.Read[openai.ChatCompletionNewParams](base),
	)
}

// answer is wiring: it runs the agent loop for a prompt against DeepSeek, with
// the registry the program built and at most MaxIterations requests.
//
// session.Run is reused unchanged: [effect.Local] widens it from SessionDeps to
// the provider's ChatCompletionDeps, and [effect.LocalEffectK] builds those
// from DeepSeekDeps with the effectful [oai.MakeDeepSeekChatCompletionDeps].
func answer(cfg Config, request openai.ChatCompletionNewParams, registry tools.ToolRegistry) effect.Kleisli[oai.DeepSeekDeps, string, session.FinalResult] {
	toSessionDeps := F.Bind23of3(session.MakeSessionDeps)(
		tools.MakeRegistryToolDeps()(registry),
		session.MakeLoopDeps(MakeConfigMaxIterationsLens().Get(cfg)),
	)

	return F.Flow5(
		makeRequest(request, registry),
		session.MakeSession,
		session.Run(),
		effect.Local[session.FinalResult](toSessionDeps),
		effect.LocalEffectK[session.FinalResult](F.Constant1[oai.DeepSeekDeps](oai.MakeDeepSeekChatCompletionDeps())),
	)
}

// modeAnswer is wiring: it builds the registry of a mode and the agent loop
// over it. The fields of [mode] are read directly at this boundary.
func modeAnswer(cfg Config) io.Kleisli[mode, effect.Kleisli[oai.DeepSeekDeps, string, session.FinalResult]] {
	return func(m mode) io.IO[effect.Kleisli[oai.DeepSeekDeps, string, session.FinalResult]] {
		return F.Pipe1(
			makeRegistry(cfg, m.tools),
			io.Map(F.Bind12of3(answer)(cfg, m.request)),
		)
	}
}

// makeAnswer is wiring: it looks the mode of the configuration up and builds
// the agent loop of that mode.
func makeAnswer(cfg Config) IOResult[effect.Kleisli[oai.DeepSeekDeps, string, session.FinalResult]] {
	return F.Pipe2(
		lookupMode()(MakeConfigModeLens().Get(cfg)),
		ioresult.FromResult[mode],
		ioresult.ChainIOK(modeAnswer(cfg)),
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
		result.FromOption[string](ER.OnNone("missing prompt, usage: fp-go-harness [--mode ask|agent] <prompt>")),
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
