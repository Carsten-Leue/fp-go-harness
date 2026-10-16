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
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/session"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/io"
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

// makeRegistry is wiring: it builds the registry of a mode's tools for the
// workspace at root, with the output of each tool limited to [maxToolOutput].
// Background commands log to [logDir] inside it.
func makeRegistry(root string, modeTools reader.Reader[tools.ToolsDeps, tools.ToolRegistry]) io.IO[tools.ToolRegistry] {
	return F.Pipe1(
		tools.MakeDefaultToolsDeps(root, filepath.Join(root, logDir)),
		io.Map(F.Flow2(modeTools, tools.TruncateTools()(maxToolOutput))),
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
// the registry the program built.
//
// session.Run is reused unchanged: [effect.Local] widens it from SessionDeps to
// the provider's ChatCompletionDeps, and [effect.LocalEffectK] builds those
// from DeepSeekDeps with the effectful [oai.MakeDeepSeekChatCompletionDeps].
func answer(request openai.ChatCompletionNewParams, registry tools.ToolRegistry) effect.Kleisli[oai.DeepSeekDeps, string, session.FinalResult] {
	toSessionDeps := F.Bind23of3(session.MakeSessionDeps)(
		tools.MakeRegistryToolDeps()(registry),
		session.MakeLoopDeps(maxIterations),
	)

	return F.Flow5(
		makeRequest(request, registry),
		session.MakeSession,
		session.Run(),
		effect.Local[session.FinalResult](toSessionDeps),
		effect.LocalEffectK[session.FinalResult](F.Constant1[oai.DeepSeekDeps](oai.MakeDeepSeekChatCompletionDeps())),
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
	modeName := flag.String("mode", askMode, "ask (read-only tools) or agent (all tools, changes the workspace)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: fp-go-harness [-mode ask|agent] <prompt>")
		flag.PrintDefaults()
	}
	flag.Parse()

	prompt := strings.Join(flag.Args(), " ")
	if prompt == "" {
		flag.Usage()
		os.Exit(2)
	}

	m, err := result.Unwrap(lookupMode()(*modeName))
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
		answer(m.request, makeRegistry(root, m.tools)())(prompt),
		effect.LocalIOResultK[session.FinalResult](F.Constant1[F.Void](makeDeepSeekDeps())),
		effect.Provide[session.FinalResult](F.VOID),
	))(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println(formatResult()(final))
}
