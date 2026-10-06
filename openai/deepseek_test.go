package openai

import (
	"fmt"
	"testing"

	thunk "github.com/IBM/fp-go/v2/context/readerioresult"

	"github.com/Carsten-Leue/fp-go-harness/env"
	"github.com/Carsten-Leue/fp-go-harness/http"
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/require"
)

// dotEnvPath is the repository's .env file, seen from this package.
const dotEnvPath = "../" + env.DotEnvFile

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
	environment, err := result.Unwrap(env.MakeDotEnvEnvironmentDeps()(A.Of(dotEnvPath))())
	require.NoError(t, err)
	if result.IsLeft(environment.GetLookupEnv()(deepSeekAPIKeyEnvVar)()) {
		t.Skipf("%s not set in the environment or in %s", deepSeekAPIKeyEnvVar, dotEnvPath)
	}

	resp := makeSampleResponse()

	seekDeps := F.Pipe1(
		makeDeepSeekChatCompletionDeps(http.MakeDefaultHttpDeps(), environment),
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
