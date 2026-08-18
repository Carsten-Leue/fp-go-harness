package openai

import (
	"context"

	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	F "github.com/IBM/fp-go/v2/function"
	R "github.com/IBM/fp-go/v2/reader"
	"github.com/openai/openai-go/v3"
)

type (
	ChatCompletionDeps interface {
		GetChatCompletionService() *openai.ChatCompletionService
	}
	chatCompletionDeps struct {
		client *openai.Client
	}
)

func (c *chatCompletionDeps) GetChatCompletionService() *openai.ChatCompletionService {
	return &c.client.Chat.Completions
}

func MakeChatCompletionDeps(client *openai.Client) ChatCompletionDeps {
	return &chatCompletionDeps{client}
}

func getNew(svc *openai.ChatCompletionService) func(context.Context, openai.ChatCompletionNewParams) (res *openai.ChatCompletion, err error) {
	return func(ctx context.Context, ccnp openai.ChatCompletionNewParams) (res *openai.ChatCompletion, err error) {
		return svc.New(ctx, ccnp)
	}
}

func ChatCompletion(req openai.ChatCompletionNewParams) Effect[ChatCompletionDeps, *openai.ChatCompletion] {
	return F.Flow4(
		ChatCompletionDeps.GetChatCompletionService,
		getNew,
		thunk.Eitherize1,
		R.Read[Thunk[*openai.ChatCompletion]](req),
	)
}
