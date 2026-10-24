package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/mode"
	"github.com/Carsten-Leue/fp-go-harness/session"
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

// TestPromptCommand_InvalidMaxIterations asserts that a failed decoding is
// returned and printed once on the standard error, with the variable and its
// value.
func TestPromptCommand_InvalidMaxIterations(t *testing.T) {
	var errOut bytes.Buffer
	cmd := MakePromptCommand()
	cmd.ErrWriter = &errOut

	err := cmd.Run(t.Context(), []string{"fp-go-harness", "--workspace", t.TempDir(), "--max-iterations", "0", "hello"})

	require.Error(t, err)
	// the %+v of the decoding lists the failed refinement and the value; its
	// context label is fp-go's name of the composed codec, which ends with
	// the last field, so it does not name MAX_ITERATIONS
	assert.Equal(t, fmt.Sprintf("%+v\n", err), errOut.String())
	assert.Contains(t, errOut.String(), "type cannot be refined: positive")
	assert.Contains(t, errOut.String(), "value: 0")
}

// TestPromptCommand_Cancelled asserts that a run whose context is cancelled
// fails with the cancellation, which main maps to exit code 130, and prints
// it once. The turn has not started, so there is no session to report and no
// request is sent.
func TestPromptCommand_Cancelled(t *testing.T) {
	withHome(t)
	t.Setenv("DEEPSEEK_API_KEY", "test-key")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var out, errOut bytes.Buffer
	cmd := MakePromptCommand()
	cmd.Writer = &out
	cmd.ErrWriter = &errOut

	err := cmd.Run(ctx, []string{"fp-go-harness", "--workspace", t.TempDir(), "hello"})

	assert.True(t, session.IsCancelled()(err), "%v", err)
	assert.Empty(t, out.String())
	assert.Equal(t, context.Canceled.Error()+"\n", errOut.String())
}
