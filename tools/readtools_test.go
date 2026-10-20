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
	assert.ElementsMatch(t, ReadToolNames(), R.Keys(registry))

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

func TestMakeTools(t *testing.T) {
	root := t.TempDir()
	deps := MakeDefaultToolsDeps(root, filepath.Join(root, "logs"))()

	registry := MakeTools()(deps)

	assert.ElementsMatch(t,
		[]string{ReadFileName, ListFilesName, GlobName, GrepName, WriteFileName, SearchAndReplaceName, ApplyDiffName, InsertContentName, ExecuteCommandName},
		R.Keys(registry))
	assert.ElementsMatch(t, ToolNames(), R.Keys(registry))

	caller := ToToolCaller()(registry)
	run := func(name, arguments string) string {
		call, ok := option.Unwrap(caller(name))
		require.True(t, ok, name)
		return runCall(t, call, arguments)
	}

	// what write_file writes, read_file reads
	assert.Equal(t, "Wrote x.txt (line count: 1).", run(WriteFileName, `{"path":"x.txt","content":"hello\n","line_count":1}`))
	assert.Equal(t, "Contents of file x.txt:\n\n1 | hello\n2 |", run(ReadFileName, `{"path":"x.txt"}`))

	// search_and_replace edits what write_file wrote
	assert.Equal(t, "Replaced 1 occurrence(s) in x.txt.", run(SearchAndReplaceName, `{"path":"x.txt","search":"hello","replace":"bye"}`))
	assert.Equal(t, "Contents of file x.txt:\n\n1 | bye\n2 |", run(ReadFileName, `{"path":"x.txt"}`))

	// apply_diff edits what search_and_replace left
	assert.Equal(t, "Applied 1 block(s) to x.txt.", run(ApplyDiffName, `{"path":"x.txt","diff":"<<<<<<< SEARCH\nbye\n=======\nhi\n>>>>>>> REPLACE"}`))
	assert.Equal(t, "Contents of file x.txt:\n\n1 | hi\n2 |", run(ReadFileName, `{"path":"x.txt"}`))

	// insert_content adds a line before what apply_diff left
	assert.Equal(t, "Inserted 1 line(s) into x.txt before line 1.", run(InsertContentName, `{"path":"x.txt","line":1,"content":"first"}`))
	assert.Equal(t, "Contents of file x.txt:\n\n1 | first\n2 | hi\n3 |", run(ReadFileName, `{"path":"x.txt"}`))

	assert.Equal(t, deps, AsToolsDeps(deps))
}
