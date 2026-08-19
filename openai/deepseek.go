package openai

import (
	"github.com/Carsten-Leue/fp-go-harness/env"
	A "github.com/IBM/fp-go/v2/array"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/openai/openai-go/v3"
	opt "github.com/openai/openai-go/v3/option"
)

const (
	deepSeekBaseURL      = "https://api.deepseek.com"
	deepSeekAPIKeyEnvVar = "DEEPSEEK_API_KEY"
)

type deepSeekChatCompletionDeps struct {
	client *openai.Client
	apiKey string
}

func (d *deepSeekChatCompletionDeps) GetChatCompletionService() *openai.ChatCompletionService {
	return &d.client.Chat.Completions
}

func (d *deepSeekChatCompletionDeps) GetRequestOptions() Thunk[[]opt.RequestOption] {
	return thunk.Of(A.Of(opt.WithAPIKey(d.apiKey)))
}

func newDeepSeekChatCompletionDeps(apiKey string) ChatCompletionDeps {
	client := openai.NewClient(opt.WithBaseURL(deepSeekBaseURL))
	return &deepSeekChatCompletionDeps{
		client: &client,
		apiKey: apiKey,
	}
}

// MakeDeepSeekChatCompletionDeps builds a ChatCompletionDeps wired to DeepSeek's
// OpenAI-compatible API. The API key is read lazily from the DEEPSEEK_API_KEY
// environment variable and attached to every request via GetRequestOptions,
// rather than baked into the client at construction time.
func MakeDeepSeekChatCompletionDeps() Effect[env.EnvironmentDeps, ChatCompletionDeps] {
	return F.Pipe1(
		env.LookupEnvThunk(deepSeekAPIKeyEnvVar),
		effect.Map[env.EnvironmentDeps](newDeepSeekChatCompletionDeps),
	)
}
