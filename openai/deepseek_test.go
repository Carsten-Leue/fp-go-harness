package openai

import (
	"context"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/env"
	"github.com/Carsten-Leue/fp-go-harness/http"
	"github.com/IBM/fp-go/v2/result"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"
)

func makeDeepSeekChatCompletionDeps(h http.HttpDeps, e env.EnvironmentDeps) DeepSeekDeps {
	type combined struct {
		http.HttpDeps
		env.EnvironmentDeps
	}

	return &combined{h, e}
}

func TestMakeDeepSeekChatCompletionDeps(t *testing.T) {
	require.NoError(t, godotenv.Load("../.env"))

	deps, err := result.Unwrap(MakeDeepSeekChatCompletionDeps()(
		makeDeepSeekChatCompletionDeps(http.MakeDefaultHttpDeps(), env.MakeEnvironmentDeps()),
	)(context.Background())())

	require.NoError(t, err)
	require.NotNil(t, deps)
}
