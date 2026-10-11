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

// fakeFileSystem records the directories and files that write_file creates.
type fakeFileSystem struct {
	dirs  []string
	files map[string]string
}

func (fs *fakeFileSystem) makeDirs(dir string) ioresult.IOResult[string] {
	return func() result.Result[string] {
		fs.dirs = append(fs.dirs, dir)
		return result.Of(dir)
	}
}

func (fs *fakeFileSystem) writeFile(path string) ioresult.Kleisli[[]byte, []byte] {
	return func(data []byte) ioresult.IOResult[[]byte] {
		return func() result.Result[[]byte] {
			fs.files[path] = string(data)
			return result.Of(data)
		}
	}
}

func makeFakeWriteFileDeps(root string) (*fakeFileSystem, WriteFileDeps) {
	fs := &fakeFileSystem{files: map[string]string{}}
	return fs, MakeWriteFileDeps(MakeWorkspaceDeps(root), MakeFileWriteDeps(fs.makeDirs, fs.writeFile))
}

func runWriteFile(t *testing.T, deps WriteFileDeps, arguments string) result.Result[string] {
	t.Helper()

	return WriteFile()(arguments)(deps)(t.Context())()
}

// writeArgs encodes the arguments of a write_file call.
func writeArgs(t *testing.T, path, content string, lineCount int) string {
	t.Helper()

	data, err := json.Marshal(map[string]any{"path": path, "content": content, "line_count": lineCount})
	require.NoError(t, err)
	return string(data)
}

func TestCountLines(t *testing.T) {
	for content, want := range map[string]int{
		"":           0,
		"\n":         1,
		"a":          1,
		"a\n":        1,
		"a\nb":       2,
		"a\r\nb\r\n": 2,
		"a\n\nb\n":   3,
	} {
		assert.Equal(t, want, countLines()(content), "%q", content)
	}
}

func TestCheckComplete(t *testing.T) {
	args := WriteFileArgs{Content: "a\nb\n", LineCount: 2}
	assert.Equal(t, result.Of(args), checkComplete()(args))

	// more lines than announced pass
	more := WriteFileArgs{Content: "a\nb\nc", LineCount: 2}
	assert.Equal(t, result.Of(more), checkComplete()(more))

	_, err := result.Unwrap(checkComplete()(WriteFileArgs{Content: "a\n", LineCount: 40}))
	assert.EqualError(t, err, "the content has a line count of 1, but line_count is 40. It looks truncated: send the complete content of the file, not only a part of it")
}

func TestWriteFile(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeWriteFileDeps(root)
	path := filepath.Join(root, "dir", "sub", "x.go")

	assert.Equal(t,
		result.Of("Wrote "+filepath.Join("dir", "sub", "x.go")+" (line count: 3)."),
		runWriteFile(t, deps, writeArgs(t, "dir/sub/x.go", "package x\n\nfunc F() {}\n", 3)))

	assert.Equal(t, []string{filepath.Join(root, "dir", "sub")}, fs.dirs)
	assert.Equal(t, map[string]string{path: "package x\n\nfunc F() {}\n"}, fs.files)

	// an absolute path names the same file
	assert.Equal(t,
		result.Of("Wrote "+filepath.Join("dir", "sub", "x.go")+" (line count: 1)."),
		runWriteFile(t, deps, writeArgs(t, path, "package y\n", 1)))
	assert.Equal(t, map[string]string{path: "package y\n"}, fs.files)
}

func TestWriteFile_Failures(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeWriteFileDeps(root)

	for name, tc := range map[string]struct {
		arguments string
		want      string
	}{
		"invalid json": {`{`, "unexpected end of JSON input"},
		"empty path":   {writeArgs(t, "", "a", 1), "path must not be empty"},
		"outside":      {writeArgs(t, "../y.txt", "a", 1), "is outside the workspace"},
		"truncated":    {writeArgs(t, "x.txt", "a\n", 2), "line count of 1, but line_count is 2"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := result.Unwrap(runWriteFile(t, deps, tc.arguments))
			assert.ErrorContains(t, err, tc.want)
		})
	}

	// nothing was written
	assert.Empty(t, fs.dirs)
	assert.Empty(t, fs.files)
}

func TestWriteFile_WriteError(t *testing.T) {
	root := t.TempDir()
	fs := &fakeFileSystem{files: map[string]string{}}
	failing := func(string) ioresult.Kleisli[[]byte, []byte] {
		return func([]byte) ioresult.IOResult[[]byte] {
			return ioresult.Left[[]byte](errors.New("disk full"))
		}
	}
	deps := MakeWriteFileDeps(MakeWorkspaceDeps(root), MakeFileWriteDeps(fs.makeDirs, failing))

	_, err := result.Unwrap(runWriteFile(t, deps, writeArgs(t, "x.txt", "a", 1)))
	assert.EqualError(t, err, "disk full")
}

// TestWriteFile_OperatingSystem writes real files through the default deps.
func TestWriteFile_OperatingSystem(t *testing.T) {
	root := t.TempDir()
	deps := MakeWriteFileDeps(MakeWorkspaceDeps(root), MakeDefaultFileWriteDeps())

	assert.Equal(t,
		result.Of("Wrote "+filepath.Join("a", "b", "x.txt")+" (line count: 2)."),
		runWriteFile(t, deps, writeArgs(t, "a/b/x.txt", "hello\nworld\n", 2)))

	content, err := os.ReadFile(filepath.Join(root, "a", "b", "x.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello\nworld\n", string(content))

	// an existing file is replaced
	assert.Equal(t,
		result.Of("Wrote "+filepath.Join("a", "b", "x.txt")+" (line count: 1)."),
		runWriteFile(t, deps, writeArgs(t, "a/b/x.txt", "bye", 1)))

	content, err = os.ReadFile(filepath.Join(root, "a", "b", "x.txt"))
	require.NoError(t, err)
	assert.Equal(t, "bye", string(content))
}

func TestMakeWriteFileTool(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeWriteFileDeps(root)

	registry := MakeToolRegistry(MakeWriteFileTool()(deps))

	call, ok := option.Unwrap(ToToolCaller()(registry)(WriteFileName))
	require.True(t, ok)
	assert.Equal(t, "Wrote x.txt (line count: 1).", runCall(t, call, writeArgs(t, "x.txt", "a", 1)))
	assert.Equal(t, map[string]string{filepath.Join(root, "x.txt"): "a"}, fs.files)

	definition := toolDefinition(registry[WriteFileName])
	assert.Equal(t, WriteFileName, definition.Name)
	assert.Equal(t, []string{"path", "content", "line_count"}, definition.Parameters["required"])

	properties := definition.Parameters["properties"].(map[string]any)
	require.Len(t, properties, 3)
	assert.Contains(t, properties["path"].(map[string]any)["description"], root)
	assert.Equal(t, "string", properties["content"].(map[string]any)["type"])
	assert.Equal(t, "integer", properties["line_count"].(map[string]any)["type"])
}

func TestAsWriteFileDeps(t *testing.T) {
	deps := MakeWriteFileDeps(MakeWorkspaceDeps(t.TempDir()), MakeDefaultFileWriteDeps())

	assert.Equal(t, deps, AsWriteFileDeps(deps))
	assert.Equal(t, FileWriteDeps(deps), AsFileWriteDeps(deps))
}
