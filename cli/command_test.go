package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/mode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests below fail before any request is sent, so they don't need an API
// key.

func TestPromptCommand_MissingPrompt(t *testing.T) {
	err := MakePromptCommand().Run(t.Context(), []string{"fp-go-harness", "--workspace", t.TempDir()})

	assert.ErrorContains(t, err, "missing prompt")
}

// withHome makes a temporary directory the home directory of the test, so
// the custom modes of the real one don't change the result.
func withHome(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// writeMode writes a mode file into the modes directory of a root.
func writeMode(t *testing.T, root, name, text string) {
	t.Helper()

	dir := filepath.Join(root, filepath.FromSlash(mode.ModesDir))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644))
}

func TestPromptCommand_UnknownMode(t *testing.T) {
	withHome(t)

	err := MakePromptCommand().Run(t.Context(), []string{"fp-go-harness", "--workspace", t.TempDir(), "--mode", "review", "hello"})

	assert.EqualError(t, err, `unknown mode "review", use one of agent, plan, ask`)
}

func TestPromptCommand_CustomModes(t *testing.T) {
	home := withHome(t)
	workspace := t.TempDir()
	writeMode(t, home, "review.md", "---\nid: review\norder: 5\ntools: [grep]\n---\nYou review.\n")
	writeMode(t, workspace, "docs.md", "---\nid: docs\norder: 4\ntools: [read_file]\n---\nYou write docs.\n")

	err := MakePromptCommand().Run(t.Context(), []string{"fp-go-harness", "--workspace", workspace, "--mode", "code", "hello"})

	assert.EqualError(t, err, `unknown mode "code", use one of agent, plan, ask, docs, review`)
}

func TestPromptCommand_DuplicateCustomMode(t *testing.T) {
	withHome(t)
	workspace := t.TempDir()
	writeMode(t, workspace, "a.md", "---\nid: review\n---\nYou review.\n")
	writeMode(t, workspace, "b.md", "---\nid: review\n---\nYou review too.\n")

	err := MakePromptCommand().Run(t.Context(), []string{"fp-go-harness", "--workspace", workspace, "hello"})

	assert.ErrorContains(t, err, `mode "review" is defined twice`)
}

func TestPromptCommand_InvalidMaxIterations(t *testing.T) {
	err := MakePromptCommand().Run(t.Context(), []string{"fp-go-harness", "--workspace", t.TempDir(), "--max-iterations", "0", "hello"})

	assert.Error(t, err)
}
