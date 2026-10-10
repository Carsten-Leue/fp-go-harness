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

func newDeepSeekChatCompletionDeps(apiKey string, client *openai.Client) ChatCompletionDeps {
	return &deepSeekChatCompletionDeps{
		client: client,
		apiKey: apiKey,
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

	apiKey := F.Pipe2(
		deepSeekAPIKeyEnvVar,
		env.LookupEnvThunk,
		effect.Local[string, DeepSeekDeps](env.AsEnvironmentDeps),
	)

	baseUrlOpts := F.Pipe2(
		deepSeekBaseURL,
		opt.WithBaseURL,
		A.Of,
	)

	// only the HTTP client is read from the deps, so the options are a plain
	// function of it: the base URL first, then the client
	openaiClient := F.Flow6(
		DeepSeekDeps.GetHttpClient,
		asHTTPClient,
		opt.WithHTTPClient,
		F.Bind1st(A.Append[opt.RequestOption], baseUrlOpts),
		newClient,
		F.Ref,
	)

	return F.Pipe3(
		openaiClient,
		reader.Map[DeepSeekDeps](reader.Curry(newDeepSeekChatCompletionDeps)),
		effect.Asks,
		effect.Ap[ChatCompletionDeps](apiKey),
	)
}
