package tools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeFakeInsertContentDeps serves the files of a map keyed by path relative
// to root and records what insert_content writes.
func makeFakeInsertContentDeps(root string, files map[string]string) (*fakeFileSystem, InsertContentDeps) {
	absolute := map[string]string{}
	for name, content := range files {
		absolute[filepath.Join(root, name)] = content
	}
	fs := &fakeFileSystem{files: map[string]string{}}
	return fs, MakeInsertContentDeps(MakeWorkspaceDeps(root), makeFakeFileDeps(absolute), MakeFileWriteDeps(fs.makeDirs, fs.writeFile))
}

func runInsertContent(t *testing.T, deps InsertContentDeps, arguments string) result.Result[string] {
	t.Helper()

	return InsertContent()(arguments)(deps)(t.Context())()
}

// insertArgs encodes the arguments of an insert_content call.
func insertArgs(t *testing.T, path string, line int, content string) string {
	t.Helper()

	data, err := json.Marshal(map[string]any{"path": path, "line": line, "content": content})
	require.NoError(t, err)
	return string(data)
}

func TestCheckInsertArgs(t *testing.T) {
	valid := InsertContentArgs{Content: "a", Line: 3}
	assert.Equal(t, result.Of(valid), checkInsertArgs()(valid))

	assert.Equal(t, result.Left[InsertContentArgs](errEmptyContent), checkInsertArgs()(InsertContentArgs{}))
	assert.Equal(t, result.Left[InsertContentArgs](errNegativeInsLine), checkInsertArgs()(InsertContentArgs{Content: "a", Line: -1}))
}

func TestInsertedText(t *testing.T) {
	for name, tc := range map[string]struct {
		text    string
		line    int
		content string
		want    string
	}{
		"before first line":    {"a\nb\n", 1, "x", "x\na\nb\n"},
		"before middle line":   {"a\nb\nc\n", 2, "x\ny\n", "a\nx\ny\nb\nc\n"},
		"before last line":     {"a\nb\n", 2, "x", "a\nx\nb\n"},
		"append":               {"a\nb\n", 0, "x", "a\nb\nx\n"},
		"append without break": {"a\nb", 0, "x", "a\nb\nx\n"},
		"beyond the end":       {"a\n", 10, "x", "a\nx\n"},
		"empty file":           {"", 0, "x", "x\n"},
		"empty file at line 1": {"", 1, "x", "x\n"},
		"content with CRLF":    {"a\n", 1, "x\r\ny", "x\ny\na\n"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, insertedText()(InsertContentArgs{Text: tc.text, Line: tc.line, Content: tc.content}))
		})
	}
}

func TestInsertContent(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeInsertContentDeps(root, map[string]string{
		filepath.Join("dir", "x.go"): "package x\n\nfunc F() {}\n",
	})

	assert.Equal(t,
		result.Of("Inserted 2 line(s) into "+filepath.Join("dir", "x.go")+" before line 2."),
		runInsertContent(t, deps, insertArgs(t, "dir/x.go", 2, "\nimport \"fmt\"")))

	assert.Equal(t, map[string]string{filepath.Join(root, "dir", "x.go"): "package x\n\nimport \"fmt\"\n\nfunc F() {}\n"}, fs.files)
}

func TestInsertContent_Append(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeInsertContentDeps(root, map[string]string{"x.txt": "a\n"})

	assert.Equal(t,
		result.Of("Appended 1 line(s) to x.txt."),
		runInsertContent(t, deps, insertArgs(t, "x.txt", 0, "b\n")))

	assert.Equal(t, map[string]string{filepath.Join(root, "x.txt"): "a\nb\n"}, fs.files)
}

func TestInsertContent_CRLF(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeInsertContentDeps(root, map[string]string{"x.txt": "a\r\nb\r\n"})

	// the file keeps its line endings
	assert.Equal(t,
		result.Of("Inserted 2 line(s) into x.txt before line 2."),
		runInsertContent(t, deps, insertArgs(t, "x.txt", 2, "x\ny")))

	assert.Equal(t, map[string]string{filepath.Join(root, "x.txt"): "a\r\nx\r\ny\r\nb\r\n"}, fs.files)
}

func TestInsertContent_Failures(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeInsertContentDeps(root, map[string]string{"x.txt": "hello\n"})

	for name, tc := range map[string]struct {
		arguments string
		want      string
	}{
		"invalid json":  {`{`, "unexpected end of JSON input"},
		"empty content": {insertArgs(t, "x.txt", 1, ""), "content must not be empty"},
		"negative line": {insertArgs(t, "x.txt", -1, "a"), "line must not be negative"},
		"empty path":    {insertArgs(t, "", 1, "a"), "path must not be empty"},
		"outside":       {insertArgs(t, "../y.txt", 1, "a"), "is outside the workspace"},
		"missing file":  {insertArgs(t, "y.txt", 1, "a"), "y.txt"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := result.Unwrap(runInsertContent(t, deps, tc.arguments))
			assert.ErrorContains(t, err, tc.want)
		})
	}

	// nothing was written
	assert.Empty(t, fs.files)
}

func TestInsertContent_WriteError(t *testing.T) {
	root := t.TempDir()
	fs := &fakeFileSystem{files: map[string]string{}}
	failing := func(string) ioresult.Kleisli[[]byte, []byte] {
		return func([]byte) ioresult.IOResult[[]byte] {
			return ioresult.Left[[]byte](errors.New("disk full"))
		}
	}
	deps := MakeInsertContentDeps(
		MakeWorkspaceDeps(root),
		makeFakeFileDeps(map[string]string{filepath.Join(root, "x.txt"): "a"}),
		MakeFileWriteDeps(fs.makeDirs, failing),
	)

	_, err := result.Unwrap(runInsertContent(t, deps, insertArgs(t, "x.txt", 0, "b")))
	assert.EqualError(t, err, "disk full")
}

// TestInsertContent_OperatingSystem edits a real file through the default
// deps.
func TestInsertContent_OperatingSystem(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "x.txt")
	require.NoError(t, os.WriteFile(path, []byte("one\nthree\n"), 0o644))
	deps := MakeInsertContentDeps(MakeWorkspaceDeps(root), MakeDefaultFileDeps(), MakeDefaultFileWriteDeps())

	assert.Equal(t,
		result.Of("Inserted 1 line(s) into x.txt before line 2."),
		runInsertContent(t, deps, insertArgs(t, "x.txt", 2, "two")))

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "one\ntwo\nthree\n", string(content))
}

func TestMakeInsertContentTool(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeInsertContentDeps(root, map[string]string{"x.txt": "a\n"})

	registry := MakeToolRegistry(MakeInsertContentTool()(deps))

	call, ok := option.Unwrap(ToToolCaller()(registry)(InsertContentName))
	require.True(t, ok)
	assert.Equal(t, "Appended 1 line(s) to x.txt.", runCall(t, call, insertArgs(t, "x.txt", 0, "b")))
	assert.Equal(t, map[string]string{filepath.Join(root, "x.txt"): "a\nb\n"}, fs.files)

	definition := toolDefinition(registry[InsertContentName])
	assert.Equal(t, InsertContentName, definition.Name)
	assert.Equal(t, []string{"path", "line", "content"}, definition.Parameters["required"])

	properties := definition.Parameters["properties"].(map[string]any)
	require.Len(t, properties, 3)
	assert.Contains(t, properties["path"].(map[string]any)["description"], root)
	assert.Equal(t, "integer", properties["line"].(map[string]any)["type"])
}

func TestAsInsertContentDeps(t *testing.T) {
	deps := MakeInsertContentDeps(MakeWorkspaceDeps(t.TempDir()), MakeDefaultFileDeps(), MakeDefaultFileWriteDeps())

	assert.Equal(t, deps, AsInsertContentDeps(deps))
}
