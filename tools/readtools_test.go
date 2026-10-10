package tools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/IBM/fp-go/v2/option"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMakeReadTools(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "x.txt"), []byte("hello\n"), 0o600))

	registry := MakeReadTools()(MakeDefaultReadToolsDeps(root))

	assert.ElementsMatch(t, []string{ReadFileName, ListFilesName, GlobName, GrepName}, R.Keys(registry))

	caller := ToToolCaller()(registry)
	run := func(name, arguments string) string {
		call, ok := option.Unwrap(caller(name))
		require.True(t, ok, name)
		return runCall(t, call, arguments)
	}

	assert.Equal(t, "Contents of file x.txt:\n\n1 | hello\n2 |", run(ReadFileName, `{"path":"x.txt"}`))
	assert.Equal(t, "Directory listing for .:\n\nx.txt", run(ListFilesName, `{"path":"."}`))
	assert.Contains(t, run(GlobName, `{"pattern":"*.txt"}`), "x.txt")
	assert.Contains(t, run(GrepName, `{"pattern":"hello"}`), "x.txt")
}

func TestMakeReadTools_NoExecuteCommand(t *testing.T) {
	registry := MakeReadTools()(MakeDefaultReadToolsDeps(t.TempDir()))

	assert.NotContains(t, registry, ExecuteCommandName)
}
