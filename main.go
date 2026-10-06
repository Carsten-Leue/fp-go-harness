package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
	L "github.com/IBM/fp-go/v2/optics/lens"
	OL "github.com/IBM/fp-go/v2/optics/optional/lens"
	P "github.com/IBM/fp-go/v2/optics/prism"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/joho/godotenv"
	"github.com/openai/openai-go/v3"
)

// maxIterations bounds the number of chat completion requests per prompt.
const maxIterations = 25

type deepSeekDeps struct {
	http.HttpDeps
	env.EnvironmentDeps
}

func makeDeepSeekDeps() oai.DeepSeekDeps {
	return &deepSeekDeps{http.MakeDefaultHttpDeps(), env.MakeEnvironmentDeps()}
}

// makeRequest builds the Ask mode request for a single user prompt, including
// the tool definitions from the registry.
func makeRequest(registry tools.ToolRegistry) func(string) openai.ChatCompletionNewParams {
	modelLens := oai.MakeChatCompletionNewParamsModelLens()
	messagesLens := oai.MakeChatCompletionNewParamsMessagesLens()

	base := F.Pipe1(
		oai.ForAskMode(),
		modelLens.Set(oai.DeepSeekModelFlash),
	)

	return F.Flow5(
		openai.UserMessage[string],
		A.Push[openai.ChatCompletionMessageParamUnion],
		messagesLens.Modify,
		reader.Read[openai.ChatCompletionNewParams](base),
		tools.WithTools(registry),
	)
}

// answer runs the agent loop for a prompt against DeepSeek.
func answer(registry tools.ToolRegistry) effect.Kleisli[oai.DeepSeekDeps, string, session.FinalResult] {
	toSessionDeps := F.Bind23of3(session.MakeSessionDeps)(
		tools.MakeRegistryToolDeps(registry),
		session.MakeLoopDeps(maxIterations),
	)

	// reuse session.Run unchanged under the provider's ChatCompletionDeps
	runWith := F.Flow3(
		session.MakeSession,
		session.Run(),
		effect.Local[session.FinalResult](toSessionDeps),
	)

	// build the provider deps first, then run the loop against them
	return F.Flow4(
		makeRequest(registry),
		runWith,
		effect.ChainThunkK[oai.DeepSeekDeps, oai.ChatCompletionDeps, session.FinalResult],
		reader.Read[effect.Effect[oai.DeepSeekDeps, session.FinalResult]](oai.MakeDeepSeekChatCompletionDeps()),
	)
}

// formatResult renders the final assistant message followed by the
// accumulated token usage.
func formatResult(final session.FinalResult) string {
	usageLens := session.MakeSessionusageLens()
	iterLens := session.MakeSessioniterationsLens()

	content := F.Pipe2(
		oai.MakeChatCompletionChoicesRefLens(),
		L.ComposePrism[*openai.ChatCompletion](P.Head[openai.ChatCompletionChoice]()),
		OL.Compose[*openai.ChatCompletion](oai.MakeChatCompletionChoiceMessageLens().Compose(oai.MakeChatCompletionMessageContentLens())),
	)

	usage := usageLens.Get(pair.Head(final))

	return fmt.Sprintf(
		"%s\n\n[iterations: %d, prompt tokens: %d, completion tokens: %d, total tokens: %d]",
		F.Pipe2(pair.Tail(final), content.GetOption, option.GetOrElse(F.Constant(""))),
		iterLens.Get(pair.Head(final)),
		usage.PromptTokens,
		usage.CompletionTokens,
		usage.TotalTokens,
	)
}

func main() {
	if err := godotenv.Load(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	prompt := strings.Join(os.Args[1:], " ")
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "usage: fp-go-harness <prompt>")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	final, err := effect.RunSync(F.Pipe1(
		answer(tools.MakeToolRegistry())(prompt),
		effect.Provide[session.FinalResult](makeDeepSeekDeps()),
	))(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println(formatResult(final))
}
