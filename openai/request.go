package openai

import (
	"context"

	A "github.com/IBM/fp-go/v2/array"
	CR "github.com/IBM/fp-go/v2/context/reader"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/openai/openai-go/v3"
	opt "github.com/openai/openai-go/v3/option"
)

type (
	ChatCompletionDeps interface {
		GetChatCompletionService() *openai.ChatCompletionService
	}
	chatCompletionDeps struct {
		client *openai.Client
	}

	requestOptions struct{}
)

var keyRequestOptions = requestOptions{}

func WithRequestOptions() CR.Kleisli[[]opt.RequestOption, context.Context] {
	return CR.WithValue[[]opt.RequestOption](keyRequestOptions)
}

func GetRequestOptions() ReaderOption[context.Context, []opt.RequestOption] {
	return F.Flow2(
		F.Bind2nd(context.Context.Value, any(keyRequestOptions)),
		option.InstanceOf[[]opt.RequestOption],
	)
}

func (c *chatCompletionDeps) GetChatCompletionService() *openai.ChatCompletionService {
	return &c.client.Chat.Completions
}

func MakeChatCompletionDeps(client *openai.Client) ChatCompletionDeps {
	return &chatCompletionDeps{client}
}

func getNew(svc *openai.ChatCompletionService) func(context.Context, openai.ChatCompletionNewParams) (res *openai.ChatCompletion, err error) {
	getApiKey := F.Flow2(
		GetRequestOptions(),
		option.GetOrElse(A.Empty[opt.RequestOption]),
	)

	return func(ctx context.Context, ccnp openai.ChatCompletionNewParams) (res *openai.ChatCompletion, err error) {
		return svc.New(ctx, ccnp, getApiKey(ctx)...)
	}
}

func ChatCompletion() effect.Kleisli[ChatCompletionDeps, openai.ChatCompletionNewParams, *openai.ChatCompletion] {
	return F.Pipe3(
		getNew,
		effect.FromIdiomatic,
		reader.Local[Effect[openai.ChatCompletionNewParams, *openai.ChatCompletion]](ChatCompletionDeps.GetChatCompletionService),
		reader.Sequence,
	)
}
