package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/env"
	"github.com/Carsten-Leue/fp-go-harness/mode"
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/session"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/pair"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The examples run the use cases of the CLI end to end against the real
// DeepSeek API, with the deps of the CLI (the process environment and the .env
// file), all tools in a temporary workspace, the agent loop and the rendering
// of the result. They are skipped when DEEPSEEK_API_KEY is set neither in the
// environment nor in .env.
//
// The model words its answers differently on every run, so each example
// checks only what does not vary: a fact the model can learn only through a
// tool, or a file that a tool wrote.
//
//	go test -v -run TestExample ./cli
//
// prints what the CLI prints for each prompt.

// deepSeekAPIKeyEnvVar is the variable the DeepSeek deps read the API key from.
const deepSeekAPIKeyEnvVar = "DEEPSEEK_API_KEY"

// dotEnvPath is the .env file in the repository root.
const dotEnvPath = "../" + env.DotEnvFile

// requireDeepSeekDeps builds the deps of the CLI, or skips the test when the
// API key is not set.
func requireDeepSeekDeps(t *testing.T) oai.DeepSeekDeps {
	t.Helper()

	deps, err := result.Unwrap(F.Pipe2(
		A.Of(dotEnvPath),
		env.MakeDotEnvEnvironmentDeps(),
		ioresult.Map(newDeepSeekDeps),
	)())
	require.NoError(t, err)
	if result.IsLeft(deps.GetLookupEnv()(deepSeekAPIKeyEnvVar)()) {
		t.Skipf("%s not set in the environment or in %s", deepSeekAPIKeyEnvVar, env.DotEnvFile)
	}
	return deps
}

// makeWorkspace creates a temporary workspace with files, relative path to
// content.
func makeWorkspace(t *testing.T, files Record[string, string]) string {
	t.Helper()

	root := t.TempDir()
	for path, content := range files {
		full := filepath.Join(root, filepath.FromSlash(path))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	}
	return root
}

// runCLI answers prompt as `go run . --mode agent --workspace <root> <prompt>`
// does: the examples run commands, so they need every tool.
// It logs what the CLI prints and returns it.
func runCLI(t *testing.T, root, prompt string) string {
	t.Helper()

	deps := requireDeepSeekDeps(t)
	cfg, err := result.Unwrap(decodeConfig()(R.FromEntries(A.From(
		pair.MakePair(KeyMode, mode.AgentID),
		pair.MakePair(KeyWorkspace, root),
	))))
	require.NoError(t, err)
	run, err := result.Unwrap(makeAnswer(cfg)())
	require.NoError(t, err)

	turn, err := effect.RunSync(F.Pipe1(
		run(prompt),
		effect.Provide[session.TurnResult](deps),
	))(t.Context())
	require.NoError(t, err)
	completion, err := result.Unwrap(pair.Tail(turn))
	require.NoError(t, err)

	out := formatResult()(pair.MakePair(pair.Head(turn), completion))
	t.Logf("$ fp-go-harness %q\n%s", prompt, out)
	require.Contains(t, out, "[iterations: ")
	return out
}

// A question that the model can answer without a tool.
func TestExampleAsk(t *testing.T) {
	t.Parallel()

	out := runCLI(t, makeWorkspace(t, R.Empty[string, string]()), "What is a Kleisli arrow? Answer in one sentence.")

	assert.Contains(t, out, "Kleisli")
}

// The model reads a file of the workspace to answer.
func TestExampleReadFile(t *testing.T) {
	t.Parallel()

	root := makeWorkspace(t, R.Singleton(
		"notes.txt", "Shopping list\nThe code word is PERIWINKLE-42.\n",
	))

	out := runCLI(t, root, "What is the code word in notes.txt?")

	assert.Contains(t, out, "PERIWINKLE-42")
}

// The model finds files by name.
func TestExampleGlob(t *testing.T) {
	t.Parallel()

	root := makeWorkspace(t, R.FromEntries(A.From(
		pair.MakePair("cmd/server/server.go", "package server\n"),
		pair.MakePair("internal/zebra.go", "package internal\n"),
		pair.MakePair("docs/zebra.md", "# Zebra\n"),
	)))

	out := runCLI(t, root, "List the paths of all Go files in this project.")

	assert.Contains(t, out, "server.go")
	assert.Contains(t, out, "zebra.go")
}

// The model lists the contents of a directory.
func TestExampleListFiles(t *testing.T) {
	t.Parallel()

	root := makeWorkspace(t, R.FromEntries(A.From(
		pair.MakePair("assets/aubergine.png", ""),
		pair.MakePair("assets/quokka.svg", ""),
	)))

	out := runCLI(t, root, "Which files are in the assets directory?")

	assert.Contains(t, out, "aubergine.png")
	assert.Contains(t, out, "quokka.svg")
}

// The model searches the contents of the workspace.
func TestExampleGrep(t *testing.T) {
	t.Parallel()

	root := makeWorkspace(t, R.FromEntries(A.From(
		pair.MakePair("billing/invoice.go", "package billing\n\n// TODO(walrus): round the totals\nfunc Total() {}\n"),
		pair.MakePair("billing/tax.go", "package billing\n\nfunc Tax() {}\n"),
		pair.MakePair("README.md", "# Shop\n"),
	)))

	out := runCLI(t, root, "Which file has a TODO about rounding? Answer with its path.")

	assert.Contains(t, out, "invoice.go")
}

// The model runs a shell command in the workspace and reports its output.
func TestExampleExecuteCommand(t *testing.T) {
	t.Parallel()

	out := runCLI(t, makeWorkspace(t, R.Empty[string, string]()), "Run the shell command `echo harness-ok-7319` and tell me exactly what it printed.")

	assert.Contains(t, out, "harness-ok-7319")
}

// A failing tool call doesn't stop the CLI: the error goes back to the model,
// which reports it.
func TestExampleToolError(t *testing.T) {
	t.Parallel()

	out := runCLI(t, makeWorkspace(t, R.Empty[string, string]()), "Run the shell command `exit 42` and tell me its exit code.")

	assert.Contains(t, out, "42")
}
