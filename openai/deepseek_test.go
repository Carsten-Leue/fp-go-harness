package openai

import (
	"context"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/env"
	"github.com/IBM/fp-go/v2/result"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/require"
)

func TestMakeDeepSeekChatCompletionDeps(t *testing.T) {
	require.NoError(t, godotenv.Load("../.env"))

	deps, err := result.Unwrap(MakeDeepSeekChatCompletionDeps()(env.MakeEnvironmentDeps())(context.Background())())

	require.NoError(t, err)
	require.NotNil(t, deps)
}
