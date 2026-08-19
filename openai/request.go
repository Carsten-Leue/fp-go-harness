package openai

import (
	"context"

	A "github.com/IBM/fp-go/v2/array"
	CR "github.com/IBM/fp-go/v2/context/reader"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	opt "github.com/openai/openai-go/v3/option"
)

type (
	ChatCompletionDeps interface {
		GetChatCompletionService() *openai.ChatCompletionService
		GetRequestOptions() Thunk[[]opt.RequestOption]
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

func (c *chatCompletionDeps) GetRequestOptions() Thunk[[]opt.RequestOption] {
	return thunk.FromLazy(A.Empty[opt.RequestOption])
}

func MakeChatCompletionDeps(client *openai.Client) ChatCompletionDeps {
	return &chatCompletionDeps{client}
}

func getNew() func(openai.ChatCompletionNewParams) func(*openai.ChatCompletionService) func([]opt.RequestOption) Thunk[*openai.ChatCompletion] {
	return func(req openai.ChatCompletionNewParams) func(*openai.ChatCompletionService) func([]opt.RequestOption) Thunk[*openai.ChatCompletion] {
		return func(svc *openai.ChatCompletionService) func([]opt.RequestOption) Thunk[*openai.ChatCompletion] {
			return func(opts []opt.RequestOption) Thunk[*openai.ChatCompletion] {
				return func(ctx context.Context) IOResult[*openai.ChatCompletion] {
					return func() Result[*openai.ChatCompletion] {
						return result.TryCatchError(svc.New(
							ctx,
							req,
							opts...,
						))
					}
				}
			}
		}
	}
}

func ChatCompletion() effect.Kleisli[ChatCompletionDeps, openai.ChatCompletionNewParams, *openai.ChatCompletion] {
	return F.Flow3(
		getNew(),
		reader.Local[Effect[[]opt.RequestOption, *openai.ChatCompletion]](ChatCompletionDeps.GetChatCompletionService),
		reader.Chain(F.Flow3(
			thunk.Chain,
			reader.Of[ChatCompletionDeps],
			reader.Ap[Thunk[*openai.ChatCompletion]](ChatCompletionDeps.GetRequestOptions),
		)),
	)
}
