package openai

import (
	"fmt"
	"testing"

	thunk "github.com/IBM/fp-go/v2/context/readerioresult"

	"github.com/Carsten-Leue/fp-go-harness/env"
	"github.com/Carsten-Leue/fp-go-harness/http"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/result"
	"github.com/joho/godotenv"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/require"
)

func makeDeepSeekChatCompletionDeps(h http.HttpDeps, e env.EnvironmentDeps) DeepSeekDeps {
	type combined struct {
		http.HttpDeps
		env.EnvironmentDeps
	}

	return &combined{h, e}
}

func makeSampleMessage() openai.ChatCompletionNewParams {
	modelLens := MakeChatCompletionNewParamsModelLens()

	return F.Pipe1(
		ForAskMode(),
		modelLens.Set(DeepSeekModelFlash),
	)
}

func makeSampleResponse() Effect[ChatCompletionDeps, *openai.ChatCompletion] {
	return F.Pipe1(
		makeSampleMessage(),
		ChatCompletion(),
	)
}

func TestMakeDeepSeekChatCompletionDeps(t *testing.T) {
	require.NoError(t, godotenv.Load("../.env"))

	resp := makeSampleResponse()

	seekDeps := F.Pipe1(
		makeDeepSeekChatCompletionDeps(http.MakeDefaultHttpDeps(), env.MakeEnvironmentDeps()),
		MakeDeepSeekChatCompletionDeps(),
	)

	final, err := result.Unwrap(F.Pipe1(
		seekDeps,
		thunk.Chain(resp),
	)(t.Context())())

	require.NoError(t, err)
	require.NotNil(t, final)

	fmt.Println(final.ID)
}
