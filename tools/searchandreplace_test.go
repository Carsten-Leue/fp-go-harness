package tools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeFakeSearchAndReplaceDeps serves the files of a map keyed by path
// relative to root and records what search_and_replace writes.
func makeFakeSearchAndReplaceDeps(root string, files map[string]string) (*fakeFileSystem, SearchAndReplaceDeps) {
	absolute := map[string]string{}
	for name, content := range files {
		absolute[filepath.Join(root, name)] = content
	}
	fs := &fakeFileSystem{files: map[string]string{}}
	return fs, MakeSearchAndReplaceDeps(MakeWorkspaceDeps(root), makeFakeFileDeps(absolute), MakeFileWriteDeps(fs.makeDirs, fs.writeFile))
}

func runSearchAndReplace(t *testing.T, deps SearchAndReplaceDeps, arguments string) result.Result[string] {
	t.Helper()

	return SearchAndReplace()(arguments)(deps)(t.Context())()
}

// replaceArgs encodes the arguments of a search_and_replace call; extra holds
// the optional ones.
func replaceArgs(t *testing.T, path, search, replace string, extra map[string]any) string {
	t.Helper()

	args := map[string]any{"path": path, "search": search, "replace": replace}
	for key, value := range extra {
		args[key] = value
	}
	data, err := json.Marshal(args)
	require.NoError(t, err)
	return string(data)
}

// compiled fills in the pattern of a call, as SearchAndReplace does.
func compiled(t *testing.T, a SearchAndReplaceArgs) SearchAndReplaceArgs {
	t.Helper()

	pattern, err := result.Unwrap(compileSearch()(a))
	require.NoError(t, err)
	a.Pattern = pattern
	return a
}

func TestPatternSource(t *testing.T) {
	for name, tc := range map[string]struct {
		args SearchAndReplaceArgs
		want string
	}{
		"literal":             {SearchAndReplaceArgs{Search: "a.b(c)"}, `a\.b\(c\)`},
		"regex":               {SearchAndReplaceArgs{Search: `a.b(c)`, UseRegex: true}, `a.b(c)`},
		"literal ignore case": {SearchAndReplaceArgs{Search: "a.b", IgnoreCase: true}, `(?i)a\.b`},
		"regex ignore case":   {SearchAndReplaceArgs{Search: "a.b", UseRegex: true, IgnoreCase: true}, `(?i)a.b`},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, patternSource()(tc.args))
		})
	}
}

func TestCompileSearch_Invalid(t *testing.T) {
	_, err := result.Unwrap(compileSearch()(SearchAndReplaceArgs{Search: "(", UseRegex: true}))
	assert.ErrorContains(t, err, "search is not a valid regular expression")
}

func TestCheckArgs(t *testing.T) {
	valid := SearchAndReplaceArgs{Search: "a", StartLine: 2, EndLine: 2}
	assert.Equal(t, result.Of(valid), checkArgs()(valid))

	open := SearchAndReplaceArgs{Search: "a", StartLine: 5}
	assert.Equal(t, result.Of(open), checkArgs()(open))

	for name, tc := range map[string]struct {
		args SearchAndReplaceArgs
		want error
	}{
		"empty search":   {SearchAndReplaceArgs{}, errEmptySearch},
		"negative start": {SearchAndReplaceArgs{Search: "a", StartLine: -1}, errNegativeLine},
		"negative end":   {SearchAndReplaceArgs{Search: "a", EndLine: -1}, errNegativeLine},
		"unordered":      {SearchAndReplaceArgs{Search: "a", StartLine: 3, EndLine: 2}, errLineOrder},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, result.Left[SearchAndReplaceArgs](tc.want), checkArgs()(tc.args))
		})
	}
}

func TestReplaceLines(t *testing.T) {
	lines := splitAfterLines()("a\nb\nc\nd\n")
	upper := regexp.MustCompile(`\w`).ReplaceAllString

	assert.Equal(t, "a\nX\nX\nd\n", replaceLines(1, 3, func(s string) string { return upper(s, "X") })(lines))
	assert.Equal(t, "X\nX\nX\nX\n", replaceLines(0, 100, func(s string) string { return upper(s, "X") })(lines))
	// a range beyond the end of the text changes nothing
	assert.Equal(t, "a\nb\nc\nd\n", replaceLines(10, 100, func(s string) string { return upper(s, "X") })(lines))
}

func TestEdit(t *testing.T) {
	content := "foo bar\nfoo baz\nqux foo\n"

	for name, tc := range map[string]struct {
		args  SearchAndReplaceArgs
		want  string
		count int
	}{
		"literal": {
			SearchAndReplaceArgs{Search: "foo", Replace: "x"},
			"x bar\nx baz\nqux x\n", 3,
		},
		"literal keeps $": {
			SearchAndReplaceArgs{Search: "bar", Replace: "$1"},
			"foo $1\nfoo baz\nqux foo\n", 1,
		},
		"regex groups": {
			SearchAndReplaceArgs{Search: `foo (\w+)`, Replace: "$1-foo", UseRegex: true},
			"bar-foo\nbaz-foo\nqux foo\n", 2,
		},
		"regex line anchors": {
			SearchAndReplaceArgs{Search: `(?m)^foo`, Replace: "x", UseRegex: true},
			"x bar\nx baz\nqux foo\n", 2,
		},
		"ignore case": {
			SearchAndReplaceArgs{Search: "FOO", Replace: "x", IgnoreCase: true},
			"x bar\nx baz\nqux x\n", 3,
		},
		"several lines": {
			SearchAndReplaceArgs{Search: "bar\nfoo", Replace: "-"},
			"foo - baz\nqux foo\n", 1,
		},
		"line range": {
			SearchAndReplaceArgs{Search: "foo", Replace: "x", StartLine: 2, EndLine: 3},
			"foo bar\nx baz\nqux x\n", 2,
		},
		"open start": {
			SearchAndReplaceArgs{Search: "foo", Replace: "x", EndLine: 1},
			"x bar\nfoo baz\nqux foo\n", 1,
		},
		"open end": {
			SearchAndReplaceArgs{Search: "foo", Replace: "x", StartLine: 3},
			"foo bar\nfoo baz\nqux x\n", 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			args := compiled(t, tc.args)
			args.Content = content

			edited, err := result.Unwrap(edit()(args))
			require.NoError(t, err)
			assert.Equal(t, tc.want, edited.Content)
			assert.Equal(t, tc.count, edited.Count)
		})
	}
}

func TestEdit_NoMatch(t *testing.T) {
	args := compiled(t, SearchAndReplaceArgs{Search: "foo", StartLine: 2, Name: "x.txt"})
	args.Content = "foo\nbar\n"

	_, err := result.Unwrap(edit()(args))
	assert.EqualError(t, err, `no match for "foo" in x.txt. Read the file again: the search must match its current content exactly`)
}

func TestSearchAndReplace(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeSearchAndReplaceDeps(root, map[string]string{
		filepath.Join("dir", "x.go"): "package x\n\nfunc F() {}\n",
	})

	assert.Equal(t,
		result.Of("Replaced 1 occurrence(s) in "+filepath.Join("dir", "x.go")+"."),
		runSearchAndReplace(t, deps, replaceArgs(t, "dir/x.go", "func F()", "func G()", nil)))

	assert.Equal(t, map[string]string{filepath.Join(root, "dir", "x.go"): "package x\n\nfunc G() {}\n"}, fs.files)
	assert.Empty(t, fs.dirs)
}

func TestSearchAndReplace_CRLF(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeSearchAndReplaceDeps(root, map[string]string{"x.txt": "a\r\nb\r\nc\r\n"})

	// a search over several lines matches, and the file keeps its line endings
	assert.Equal(t,
		result.Of("Replaced 1 occurrence(s) in x.txt."),
		runSearchAndReplace(t, deps, replaceArgs(t, "x.txt", "a\nb", "a\nB\nb", nil)))

	assert.Equal(t, map[string]string{filepath.Join(root, "x.txt"): "a\r\nB\r\nb\r\nc\r\n"}, fs.files)
}

func TestSearchAndReplace_Failures(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeSearchAndReplaceDeps(root, map[string]string{"x.txt": "hello\n"})

	for name, tc := range map[string]struct {
		arguments string
		want      string
	}{
		"invalid json":  {`{`, "unexpected end of JSON input"},
		"empty search":  {replaceArgs(t, "x.txt", "", "a", nil), "search must not be empty"},
		"invalid regex": {replaceArgs(t, "x.txt", "(", "a", map[string]any{"use_regex": true}), "not a valid regular expression"},
		"bad range":     {replaceArgs(t, "x.txt", "a", "b", map[string]any{"start_line": 3, "end_line": 1}), "end_line must not be before start_line"},
		"empty path":    {replaceArgs(t, "", "a", "b", nil), "path must not be empty"},
		"outside":       {replaceArgs(t, "../y.txt", "a", "b", nil), "is outside the workspace"},
		"missing file":  {replaceArgs(t, "y.txt", "a", "b", nil), "y.txt"},
		"no match":      {replaceArgs(t, "x.txt", "bye", "b", nil), `no match for "bye" in x.txt`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := result.Unwrap(runSearchAndReplace(t, deps, tc.arguments))
			assert.ErrorContains(t, err, tc.want)
		})
	}

	// nothing was written
	assert.Empty(t, fs.files)
}

func TestSearchAndReplace_WriteError(t *testing.T) {
	root := t.TempDir()
	fs := &fakeFileSystem{files: map[string]string{}}
	failing := func(string) ioresult.Kleisli[[]byte, []byte] {
		return func([]byte) ioresult.IOResult[[]byte] {
			return ioresult.Left[[]byte](errors.New("disk full"))
		}
	}
	deps := MakeSearchAndReplaceDeps(
		MakeWorkspaceDeps(root),
		makeFakeFileDeps(map[string]string{filepath.Join(root, "x.txt"): "a"}),
		MakeFileWriteDeps(fs.makeDirs, failing),
	)

	_, err := result.Unwrap(runSearchAndReplace(t, deps, replaceArgs(t, "x.txt", "a", "b", nil)))
	assert.EqualError(t, err, "disk full")
}

// TestSearchAndReplace_OperatingSystem edits a real file through the default
// deps.
func TestSearchAndReplace_OperatingSystem(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "x.txt")
	require.NoError(t, os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644))
	deps := MakeSearchAndReplaceDeps(MakeWorkspaceDeps(root), MakeDefaultFileDeps(), MakeDefaultFileWriteDeps())

	assert.Equal(t,
		result.Of("Replaced 2 occurrence(s) in x.txt."),
		runSearchAndReplace(t, deps, replaceArgs(t, "x.txt", `(?m)^t(\w+)$`, "T$1", map[string]any{"use_regex": true})))

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "one\nTwo\nThree\n", string(content))
}

func TestMakeSearchAndReplaceTool(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeSearchAndReplaceDeps(root, map[string]string{"x.txt": "a"})

	registry := MakeToolRegistry(MakeSearchAndReplaceTool()(deps))

	call, ok := option.Unwrap(ToToolCaller()(registry)(SearchAndReplaceName))
	require.True(t, ok)
	assert.Equal(t, "Replaced 1 occurrence(s) in x.txt.", runCall(t, call, replaceArgs(t, "x.txt", "a", "b", nil)))
	assert.Equal(t, map[string]string{filepath.Join(root, "x.txt"): "b"}, fs.files)

	definition := toolDefinition(registry[SearchAndReplaceName])
	assert.Equal(t, SearchAndReplaceName, definition.Name)
	assert.Equal(t, []string{"path", "search", "replace"}, definition.Parameters["required"])

	properties := definition.Parameters["properties"].(map[string]any)
	require.Len(t, properties, 7)
	assert.Contains(t, properties["path"].(map[string]any)["description"], root)
	assert.Equal(t, "boolean", properties["use_regex"].(map[string]any)["type"])
	assert.Equal(t, "integer", properties["start_line"].(map[string]any)["type"])
}

func TestAsSearchAndReplaceDeps(t *testing.T) {
	deps := MakeSearchAndReplaceDeps(MakeWorkspaceDeps(t.TempDir()), MakeDefaultFileDeps(), MakeDefaultFileWriteDeps())

	assert.Equal(t, deps, AsSearchAndReplaceDeps(deps))
}
