package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The tests below fail before any request is sent, so they don't need an API
// key.

func TestPromptCommand_MissingPrompt(t *testing.T) {
	err := MakePromptCommand().Run(t.Context(), []string{"fp-go-harness", "--workspace", t.TempDir()})

	assert.ErrorContains(t, err, "missing prompt")
}

func TestPromptCommand_UnknownMode(t *testing.T) {
	err := MakePromptCommand().Run(t.Context(), []string{"fp-go-harness", "--workspace", t.TempDir(), "--mode", "plan", "hello"})

	assert.EqualError(t, err, `unknown mode "plan", use "ask" or "agent"`)
}

func TestPromptCommand_InvalidMaxIterations(t *testing.T) {
	err := MakePromptCommand().Run(t.Context(), []string{"fp-go-harness", "--workspace", t.TempDir(), "--max-iterations", "0", "hello"})

	assert.Error(t, err)
}
