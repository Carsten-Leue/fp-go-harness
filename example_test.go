package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/env"
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/session"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The examples run the use cases of the CLI end to end against the real
// DeepSeek API, with the deps of main (the process environment and the .env
// file), all tools in a temporary workspace, the agent loop and the rendering
// of the result. They are skipped when DEEPSEEK_API_KEY is set neither in the
// environment nor in .env.
//
// The model words its answers differently on every run, so each example
// checks only what does not vary: a fact the model can learn only through a
// tool, or a file that a tool wrote.
//
//	go test -v -run TestExample .
//
// prints what the CLI prints for each prompt.

// deepSeekAPIKeyEnvVar is the variable the DeepSeek deps read the API key from.
const deepSeekAPIKeyEnvVar = "DEEPSEEK_API_KEY"

// requireDeepSeekDeps builds the deps of the CLI, or skips the test when the
// API key is not set.
func requireDeepSeekDeps(t *testing.T) oai.DeepSeekDeps {
	t.Helper()

	deps, err := result.Unwrap(makeDeepSeekDeps()())
	require.NoError(t, err)
	if result.IsLeft(deps.GetLookupEnv()(deepSeekAPIKeyEnvVar)()) {
		t.Skipf("%s not set in the environment or in %s", deepSeekAPIKeyEnvVar, env.DotEnvFile)
	}
	return deps
}

// makeWorkspace creates a temporary workspace with files, relative path to
// content.
func makeWorkspace(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for path, content := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	return root
}

// runCLI answers prompt as `go run . <prompt>` does in the workspace at root.
// It logs what the CLI prints and returns it.
func runCLI(t *testing.T, root, prompt string) string {
	t.Helper()

	deps := requireDeepSeekDeps(t)

	final, err := effect.RunSync(F.Pipe1(
		answer(makeRegistry(root)())(prompt),
		effect.Provide[session.FinalResult](deps),
	))(t.Context())
	require.NoError(t, err)

	out := formatResult()(final)
	t.Logf("$ fp-go-harness %q\n%s", prompt, out)
	require.Contains(t, out, "[iterations: ")
	return out
}

// A question that the model can answer without a tool.
func TestExampleAsk(t *testing.T) {
	t.Parallel()

	out := runCLI(t, makeWorkspace(t, nil), "What is a Kleisli arrow? Answer in one sentence.")

	assert.Contains(t, out, "Kleisli")
}

// The model reads a file of the workspace to answer.
func TestExampleReadFile(t *testing.T) {
	t.Parallel()

	root := makeWorkspace(t, map[string]string{
		"notes.txt": "Shopping list\nThe code word is PERIWINKLE-42.\n",
	})

	out := runCLI(t, root, "What is the code word in notes.txt?")

	assert.Contains(t, out, "PERIWINKLE-42")
}

// The model finds files by name.
func TestExampleGlob(t *testing.T) {
	t.Parallel()

	root := makeWorkspace(t, map[string]string{
		"cmd/server/server.go": "package server\n",
		"internal/zebra.go":    "package internal\n",
		"docs/zebra.md":        "# Zebra\n",
	})

	out := runCLI(t, root, "List the paths of all Go files in this project.")

	assert.Contains(t, out, "server.go")
	assert.Contains(t, out, "zebra.go")
}

// The model lists the contents of a directory.
func TestExampleListFiles(t *testing.T) {
	t.Parallel()

	root := makeWorkspace(t, map[string]string{
		"assets/aubergine.png": "",
		"assets/quokka.svg":    "",
	})

	out := runCLI(t, root, "Which files are in the assets directory?")

	assert.Contains(t, out, "aubergine.png")
	assert.Contains(t, out, "quokka.svg")
}

// The model searches the contents of the workspace.
func TestExampleGrep(t *testing.T) {
	t.Parallel()

	root := makeWorkspace(t, map[string]string{
		"billing/invoice.go": "package billing\n\n// TODO(walrus): round the totals\nfunc Total() {}\n",
		"billing/tax.go":     "package billing\n\nfunc Tax() {}\n",
		"README.md":          "# Shop\n",
	})

	out := runCLI(t, root, "Which file has a TODO about rounding? Answer with its path.")

	assert.Contains(t, out, "invoice.go")
}

// The model runs a shell command in the workspace and reports its output.
func TestExampleExecuteCommand(t *testing.T) {
	t.Parallel()

	out := runCLI(t, makeWorkspace(t, nil), "Run the shell command `echo harness-ok-7319` and tell me exactly what it printed.")

	assert.Contains(t, out, "harness-ok-7319")
}

// A failing tool call doesn't stop the CLI: the error goes back to the model,
// which reports it.
func TestExampleToolError(t *testing.T) {
	t.Parallel()

	out := runCLI(t, makeWorkspace(t, nil), "Run the shell command `exit 42` and tell me its exit code.")

	assert.Contains(t, out, "42")
}
