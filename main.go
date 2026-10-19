package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/Carsten-Leue/fp-go-harness/env"
	"github.com/Carsten-Leue/fp-go-harness/http"
	"github.com/Carsten-Leue/fp-go-harness/mode"
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/session"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	L "github.com/IBM/fp-go/v2/optics/lens"
	OL "github.com/IBM/fp-go/v2/optics/optional/lens"
	P "github.com/IBM/fp-go/v2/optics/prism"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
)

// maxIterations bounds the number of chat completion requests per prompt.
const maxIterations = 25

// maxToolOutput bounds the bytes of a single tool result sent to the model.
// The recorded sessions contain results of up to 37 KB.
const maxToolOutput = 50_000

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

// makeDeepSeekDeps reads the environment from the process and from the .env
// file in the working directory, which fills in what the process doesn't set.
func makeDeepSeekDeps() ioresult.IOResult[oai.DeepSeekDeps] {
	return F.Pipe2(
		A.Of(env.DotEnvFile),
		env.MakeDotEnvEnvironmentDeps(),
		ioresult.Map(newDeepSeekDeps),
	)
}

// makeRegistry is wiring: it builds the registry of the tools of a mode,
// with the output of each tool limited to [maxToolOutput].
func makeRegistry(m mode.Mode) reader.Reader[tools.ToolsDeps, tools.ToolRegistry] {
	return F.Flow2(
		mode.Tools()(m),
		tools.TruncateTools()(maxToolOutput),
	)
}

// withPrompt adds the user's prompt to the request of a mode and sets the
// model.
func withPrompt() func(string) mode.Endomorphism[openai.ChatCompletionNewParams] {
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
// with the registry the program built.
//
// session.Run is reused unchanged: [effect.Local] widens it from SessionDeps to
// the provider's ChatCompletionDeps, and [effect.LocalEffectK] builds those
// from DeepSeekDeps with the effectful [oai.MakeDeepSeekChatCompletionDeps].
func answer(registry tools.ToolRegistry) effect.Kleisli[oai.DeepSeekDeps, openai.ChatCompletionNewParams, session.FinalResult] {
	toSessionDeps := F.Bind23of3(session.MakeSessionDeps)(
		tools.MakeRegistryToolDeps()(registry),
		session.MakeLoopDeps(maxIterations),
	)

	return F.Flow4(
		session.MakeSession,
		session.Run(),
		effect.Local[session.FinalResult](toSessionDeps),
		effect.LocalEffectK[session.FinalResult](F.Constant1[oai.DeepSeekDeps](oai.MakeDeepSeekChatCompletionDeps())),
	)
}

// ask is wiring: it answers a prompt in a mode, in the workspace of the tools.
// The request of the mode reads its system prompt from that workspace.
func ask(m mode.Mode, deps tools.ToolsDeps) effect.Kleisli[oai.DeepSeekDeps, string, session.FinalResult] {
	request := F.Pipe1(
		mode.Request()(m),
		effect.Local[openai.ChatCompletionNewParams](F.Constant1[oai.DeepSeekDeps](mode.MakeDefaultModeDeps(deps))),
	)

	return F.Flow4(
		withPrompt(),
		effect.Map[oai.DeepSeekDeps, openai.ChatCompletionNewParams, openai.ChatCompletionNewParams],
		reader.Read[effect.Effect[oai.DeepSeekDeps, openai.ChatCompletionNewParams]](request),
		effect.Chain(answer(makeRegistry(m)(deps))),
	)
}

// finalContent extracts the content of the first choice of a completion, or
// the empty string.
func finalContent() func(*openai.ChatCompletion) string {
	choicesLens := oai.MakeChatCompletionChoicesRefLens()
	messageLens := oai.MakeChatCompletionChoiceMessageLens()
	contentLens := oai.MakeChatCompletionMessageContentLens()

	content := F.Pipe2(
		choicesLens,
		L.ComposePrism[*openai.ChatCompletion](P.Head[openai.ChatCompletionChoice]()),
		OL.Compose[*openai.ChatCompletion](messageLens.Compose(contentLens)),
	)

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

func main() {
	modeID := flag.String("mode", mode.AskID, "agent (all tools, changes the workspace), plan or ask (read-only tools)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: fp-go-harness [-mode %s] <prompt>\n", strings.Join(mode.IDs(), "|"))
		flag.PrintDefaults()
	}
	flag.Parse()

	prompt := strings.Join(flag.Args(), " ")
	if prompt == "" {
		flag.Usage()
		os.Exit(2)
	}

	m, err := result.Unwrap(mode.Lookup()(*modeID))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	// The working directory is the workspace the tools work in.
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	final, err := effect.RunSync(F.Pipe2(
		ask(m, tools.MakeDefaultToolsDeps(root, filepath.Join(root, logDir))())(prompt),
		effect.LocalIOResultK[session.FinalResult](F.Constant1[F.Void](makeDeepSeekDeps())),
		effect.Provide[session.FinalResult](F.VOID),
	))(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println(formatResult()(final))
}
