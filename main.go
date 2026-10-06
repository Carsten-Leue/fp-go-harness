package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/Carsten-Leue/fp-go-harness/env"
	"github.com/Carsten-Leue/fp-go-harness/http"
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
	"github.com/openai/openai-go/v3"
)

// maxIterations bounds the number of chat completion requests per prompt.
const maxIterations = 25

type deepSeekDeps struct {
	http.HttpDeps
	env.EnvironmentDeps
}

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

// makeRequest builds the Ask mode request for a single user prompt, including
// the tool definitions from the registry.
func makeRequest(registry tools.ToolRegistry) func(string) openai.ChatCompletionNewParams {
	modelLens := oai.MakeChatCompletionNewParamsModelLens()
	messagesLens := oai.MakeChatCompletionNewParamsMessagesLens()

	base := F.Pipe2(
		oai.ForAskMode(),
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

// answer runs the agent loop for a prompt against DeepSeek.
//
// session.Run is reused unchanged: [effect.Local] widens it from SessionDeps to
// the provider's ChatCompletionDeps, and [effect.LocalEffectK] builds those
// from DeepSeekDeps with the effectful [oai.MakeDeepSeekChatCompletionDeps].
func answer(registry tools.ToolRegistry) effect.Kleisli[oai.DeepSeekDeps, string, session.FinalResult] {
	toSessionDeps := F.Bind23of3(session.MakeSessionDeps)(
		tools.MakeRegistryToolDeps()(registry),
		session.MakeLoopDeps(maxIterations),
	)

	return F.Flow5(
		makeRequest(registry),
		session.MakeSession,
		session.Run(),
		effect.Local[session.FinalResult](toSessionDeps),
		effect.LocalEffectK[session.FinalResult](F.Constant1[oai.DeepSeekDeps](oai.MakeDeepSeekChatCompletionDeps())),
	)
}

// finalContent extracts the content of the first choice of a completion, or
// the empty string.
func finalContent() func(*openai.ChatCompletion) string {
	content := F.Pipe2(
		oai.MakeChatCompletionChoicesRefLens(),
		L.ComposePrism[*openai.ChatCompletion](P.Head[openai.ChatCompletionChoice]()),
		OL.Compose[*openai.ChatCompletion](oai.MakeChatCompletionChoiceMessageLens().Compose(oai.MakeChatCompletionMessageContentLens())),
	)

	return F.Flow2(
		content.GetOption,
		option.GetOrElse(F.Constant("")),
	)
}

// sessionStats renders the iteration count and accumulated token usage of a
// session. It is a leaf: one formatter over several fields.
func sessionStats(s session.Session) string {
	usage := session.MakeSessionusageLens().Get(s)

	return fmt.Sprintf(
		"[iterations: %d, prompt tokens: %d, completion tokens: %d, total tokens: %d]",
		session.MakeSessioniterationsLens().Get(s),
		usage.PromptTokens,
		usage.CompletionTokens,
		usage.TotalTokens,
	)
}

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
	prompt := strings.Join(os.Args[1:], " ")
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "usage: fp-go-harness <prompt>")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	final, err := effect.RunSync(F.Pipe2(
		answer(tools.MakeToolRegistry())(prompt),
		effect.LocalIOResultK[session.FinalResult](F.Constant1[F.Void](makeDeepSeekDeps())),
		effect.Provide[session.FinalResult](F.VOID),
	))(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println(formatResult()(final))
}
