package openai

import (
	HTTP "net/http"

	"github.com/Carsten-Leue/fp-go-harness/env"
	"github.com/Carsten-Leue/fp-go-harness/http"
	A "github.com/IBM/fp-go/v2/array"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/openai/openai-go/v3"
	opt "github.com/openai/openai-go/v3/option"
)

const (
	deepSeekBaseURL      = "https://api.deepseek.com"
	deepSeekAPIKeyEnvVar = "DEEPSEEK_API_KEY"
)

// DeepSeek chat model identifiers usable as ChatCompletionNewParams.Model
// against DeepSeek's OpenAI-compatible API.
// See: https://api-docs.deepseek.com/quick_start/pricing
const (
	DeepSeekModelFlash = "deepseek-v4-flash" // replaces deepseek-chat / deepseek-reasoner
	DeepSeekModelPro   = "deepseek-v4-pro"
)

type DeepSeekDeps interface {
	env.EnvironmentDeps
	http.HttpDeps
}

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

func newDeepSeekChatCompletionDeps(client *openai.Client) func(string) ChatCompletionDeps {
	return func(apiKey string) ChatCompletionDeps {
		return &deepSeekChatCompletionDeps{
			client: client,
			apiKey: apiKey,
		}
	}
}

func asHTTPClient(c *HTTP.Client) opt.HTTPClient {
	return c
}

// MakeDeepSeekChatCompletionDeps builds a ChatCompletionDeps wired to DeepSeek's
// OpenAI-compatible API. The API key is read lazily from the DEEPSEEK_API_KEY
// environment variable and attached to every request via GetRequestOptions,
// rather than baked into the client at construction time.
func MakeDeepSeekChatCompletionDeps() Effect[DeepSeekDeps, ChatCompletionDeps] {

	newClient := F.Unvariadic0(openai.NewClient)

	apiKey := F.Pipe1(
		env.LookupEnvThunk(deepSeekAPIKeyEnvVar),
		effect.Local[string, DeepSeekDeps](env.AsEnvironmentDeps),
	)

	baseUrlOpts := F.Pipe1(
		deepSeekBaseURL,
		opt.WithBaseURL,
	)

	httpOpts := F.Pipe1(
		DeepSeekDeps.GetHttpClient,
		reader.Map[DeepSeekDeps](F.Flow2(
			asHTTPClient,
			opt.WithHTTPClient,
		)),
	)

	openaiClient := F.Pipe4(
		baseUrlOpts,
		A.Of,
		reader.Of[DeepSeekDeps],
		reader.ApS(A.Push, httpOpts),
		reader.Map[DeepSeekDeps](F.Flow2(
			newClient,
			F.Ref,
		)),
	)

	return F.Pipe3(
		openaiClient,
		reader.Map[DeepSeekDeps](newDeepSeekChatCompletionDeps),
		effect.Asks,
		effect.Ap[ChatCompletionDeps](apiKey),
	)
}
