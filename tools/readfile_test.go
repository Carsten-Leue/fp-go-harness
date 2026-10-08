package tools

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeFakeFileDeps serves the files of a map keyed by absolute path.
func makeFakeFileDeps(files map[string]string) FileDeps {
	return MakeFileDeps(func(path string) ioresult.IOResult[[]byte] {
		return func() result.Result[[]byte] {
			content, ok := files[path]
			if !ok {
				return result.Left[[]byte](&fs.PathError{Op: "open", Path: path, Err: fs.ErrNotExist})
			}
			return result.Of([]byte(content))
		}
	})
}

func runReadFile(t *testing.T, deps ReadFileDeps, arguments string) result.Result[string] {
	t.Helper()

	return ReadFile()(arguments)(deps)(t.Context())()
}

func TestParseRange(t *testing.T) {
	assert.Equal(t, result.Of(pair.MakePair(3, 7)), parseRange()("3-7"))
	assert.Equal(t, result.Of(pair.MakePair(5, 5)), parseRange()(" 5 - 5 "))

	for name, tc := range map[string]struct {
		rng  string
		want string
	}{
		"no dash":    {"12", "range must have the format start-end, e.g. 10-20"},
		"not an int": {"a-3", `strconv.Atoi: parsing "a": invalid syntax`},
		"zero":       {"0-3", "line number 0 must be at least 1"},
		"zero end":   {"1-0", "line number 0 must be at least 1"},
		"unordered":  {"7-3", "the start of the range must not be after its end"},
		"open end":   {"3-", `strconv.Atoi: parsing "": invalid syntax`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := result.Unwrap(parseRange()(tc.rng))
			assert.EqualError(t, err, tc.want)
		})
	}
}

func TestNumberRange(t *testing.T) {
	lines := []string{"a", "b", "c"}

	assert.Equal(t, result.Of("2 | b\n3 | c"), numberRange(2, 3)(lines))
	// the end is cut off at the last line
	assert.Equal(t, result.Of("3 | c"), numberRange(3, 10)(lines))
	// a start beyond the last line fails
	_, err := result.Unwrap(numberRange(4, 10)(lines))
	assert.EqualError(t, err, "range starts at line 4, but the file has 3 lines")
}

func TestCutRange(t *testing.T) {
	assert.Equal(t, option.Some(pair.MakePair("1", "2")), cutRange("1-2"))
	assert.True(t, option.IsNone(cutRange("12")))
}

func TestNumberLines(t *testing.T) {
	assert.Equal(t, "1 | a\n2 | b\n3 |", numberLines(1)([]string{"a", "b", ""}))
	assert.Equal(t, "10 | x", numberLines(10)([]string{"x"}))
}

func TestSplitLines_DropsCarriageReturns(t *testing.T) {
	assert.Equal(t, []string{"a", "b", ""}, splitLines()([]byte("a\r\nb\r\n")))
}

func TestReadFile_WholeFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "dir", "x.go")
	deps := MakeReadFileDeps(MakeWorkspaceDeps(root), makeFakeFileDeps(map[string]string{
		path: "package x\n\nfunc F() {}\n",
	}))

	want := "Contents of file " + filepath.Join("dir", "x.go") + ":\n\n" +
		"1 | package x\n2 | \n3 | func F() {}\n4 |"

	// a relative and an absolute path name the file the same way
	assert.Equal(t, result.Of(want), runReadFile(t, deps, `{"path":"dir/x.go"}`))

	args, err := json.Marshal(map[string]string{"path": path})
	require.NoError(t, err)
	assert.Equal(t, result.Of(want), runReadFile(t, deps, string(args)))
}

func TestReadFile_Range(t *testing.T) {
	root := t.TempDir()
	deps := MakeReadFileDeps(MakeWorkspaceDeps(root), makeFakeFileDeps(map[string]string{
		filepath.Join(root, "x.txt"): "a\nb\nc\nd\n",
	}))

	assert.Equal(t,
		result.Of("Contents of file x.txt:\n\n2 | b\n3 | c"),
		runReadFile(t, deps, `{"path":"x.txt","range":"2-3"}`))

	// a range beyond the end of the file is cut off
	assert.Equal(t,
		result.Of("Contents of file x.txt:\n\n4 | d\n5 |"),
		runReadFile(t, deps, `{"path":"x.txt","range":"4-100"}`))

	// a range that starts after the last line is an error for the model
	_, err := result.Unwrap(runReadFile(t, deps, `{"path":"x.txt","range":"50-60"}`))
	assert.EqualError(t, err, "range starts at line 50, but the file has 5 lines")
}

func TestReadFile_EmptyFile(t *testing.T) {
	root := t.TempDir()
	deps := MakeReadFileDeps(MakeWorkspaceDeps(root), makeFakeFileDeps(map[string]string{
		filepath.Join(root, "empty.txt"): "",
	}))

	assert.Equal(t,
		result.Of("Contents of file empty.txt:\n\n1 |"),
		runReadFile(t, deps, `{"path":"empty.txt"}`))
}

func TestReadFile_Failures(t *testing.T) {
	root := t.TempDir()
	deps := MakeReadFileDeps(MakeWorkspaceDeps(root), makeFakeFileDeps(map[string]string{
		filepath.Join(root, "x.txt"): "a\n",
	}))

	for name, tc := range map[string]struct {
		arguments string
		want      string
	}{
		"invalid json": {`{`, "unexpected end of JSON input"},
		"empty path":   {`{"path":""}`, "path must not be empty"},
		"outside":      {`{"path":"../y.txt"}`, "is outside the workspace"},
		"missing file": {`{"path":"missing.txt"}`, "file does not exist"},
		"bad range":    {`{"path":"x.txt","range":"x"}`, "range must have the format start-end"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := result.Unwrap(runReadFile(t, deps, tc.arguments))
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

// TestReadFile_OperatingSystem reads a real file through the default deps.
func TestReadFile_OperatingSystem(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "x.txt"), []byte("hello\r\nworld"), 0o600))
	deps := MakeReadFileDeps(MakeWorkspaceDeps(root), MakeDefaultFileDeps())

	assert.Equal(t,
		result.Of("Contents of file x.txt:\n\n1 | hello\n2 | world"),
		runReadFile(t, deps, `{"path":"x.txt"}`))
}

func TestMakeReadFileTool(t *testing.T) {
	root := t.TempDir()
	deps := MakeReadFileDeps(MakeWorkspaceDeps(root), makeFakeFileDeps(map[string]string{
		filepath.Join(root, "x.txt"): "a",
	}))

	registry := MakeToolRegistry(MakeReadFileTool()(deps))

	call, ok := option.Unwrap(ToToolCaller()(registry)(ReadFileName))
	require.True(t, ok)
	assert.Equal(t, "Contents of file x.txt:\n\n1 | a", runCall(t, call, `{"path":"x.txt"}`))

	definition := toolDefinition(registry[ReadFileName])
	assert.Equal(t, ReadFileName, definition.Name)
	assert.Equal(t, "object", definition.Parameters["type"])
	assert.Equal(t, []string{"path"}, definition.Parameters["required"])

	properties := definition.Parameters["properties"].(map[string]any)
	require.Len(t, properties, 2)

	path := properties["path"].(map[string]any)
	assert.Equal(t, "string", path["type"])
	assert.Contains(t, path["description"], root)

	rng := properties["range"].(map[string]any)
	assert.Equal(t, "string", rng["type"])
	assert.Contains(t, rng["description"], "start-end")
}

func TestAsReadFileDeps(t *testing.T) {
	deps := MakeReadFileDeps(MakeWorkspaceDeps(t.TempDir()), MakeDefaultFileDeps())

	assert.Equal(t, deps, AsReadFileDeps(deps))
	assert.Equal(t, FileDeps(deps), AsFileDeps(deps))
}
