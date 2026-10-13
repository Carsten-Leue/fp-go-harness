package tools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeFakeApplyDiffDeps serves the files of a map keyed by path relative to
// root and records what apply_diff writes.
func makeFakeApplyDiffDeps(root string, files map[string]string) (*fakeFileSystem, ApplyDiffDeps) {
	absolute := map[string]string{}
	for name, content := range files {
		absolute[filepath.Join(root, name)] = content
	}
	fs := &fakeFileSystem{files: map[string]string{}}
	return fs, MakeApplyDiffDeps(MakeWorkspaceDeps(root), makeFakeFileDeps(absolute), MakeFileWriteDeps(fs.makeDirs, fs.writeFile))
}

func runApplyDiff(t *testing.T, deps ApplyDiffDeps, arguments string) result.Result[string] {
	t.Helper()

	return ApplyDiff()(arguments)(deps)(t.Context())()
}

// diffArgs encodes the arguments of an apply_diff call.
func diffArgs(t *testing.T, path, diff string) string {
	t.Helper()

	data, err := json.Marshal(map[string]any{"path": path, "diff": diff})
	require.NoError(t, err)
	return string(data)
}

func TestParseBlocks(t *testing.T) {
	for name, tc := range map[string]struct {
		diff string
		want []DiffBlock
	}{
		"plain": {
			"<<<<<<< SEARCH\nfoo\n=======\nbar\n>>>>>>> REPLACE",
			[]DiffBlock{{Search: "foo", Replace: "bar"}},
		},
		"start line and separator": {
			"<<<<<<< SEARCH\n:start_line:12\n-------\nfoo\n=======\nbar\n>>>>>>> REPLACE\n",
			[]DiffBlock{{StartLine: 12, Search: "foo", Replace: "bar"}},
		},
		"separator only": {
			"<<<<<<< SEARCH\n-------\nfoo\n=======\nbar\n>>>>>>> REPLACE",
			[]DiffBlock{{Search: "foo", Replace: "bar"}},
		},
		"several lines and empty replace": {
			"<<<<<<< SEARCH\na\n  b\n=======\n>>>>>>> REPLACE",
			[]DiffBlock{{Search: "a\n  b", Replace: ""}},
		},
		"crlf": {
			"<<<<<<< SEARCH\r\nfoo\r\n=======\r\nbar\r\n>>>>>>> REPLACE\r\n",
			[]DiffBlock{{Search: "foo", Replace: "bar"}},
		},
		"several blocks with text between": {
			"<<<<<<< SEARCH\n:start_line:1\n-------\na\n=======\nA\n>>>>>>> REPLACE\n\nsome text\n<<<<<<< SEARCH\n:start_line:3\n-------\nc\n=======\nC\n>>>>>>> REPLACE",
			[]DiffBlock{{StartLine: 1, Search: "a", Replace: "A"}, {StartLine: 3, Search: "c", Replace: "C"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, result.Of(tc.want), parseBlocks()(tc.diff))
		})
	}
}

func TestParseBlocks_Failures(t *testing.T) {
	assert.Equal(t, result.Left[[]DiffBlock](errNoBlocks), parseBlocks()("foo"))
	assert.Equal(t, result.Left[[]DiffBlock](errNoBlocks), parseBlocks()("<<<<<<< SEARCH\nfoo\n=======\nbar"))
	assert.Equal(t, result.Left[[]DiffBlock](errEmptyBlock), parseBlocks()("<<<<<<< SEARCH\n=======\nbar\n>>>>>>> REPLACE"))
}

func TestApplyBlocks(t *testing.T) {
	content := "x\ny\nx\ny\nx\n"

	for name, tc := range map[string]struct {
		blocks []DiffBlock
		want   string
	}{
		"no hint replaces the first": {
			[]DiffBlock{{Search: "x", Replace: "X"}},
			"X\ny\nx\ny\nx\n",
		},
		"hint picks the nearest": {
			[]DiffBlock{{StartLine: 4, Search: "x", Replace: "X"}},
			"x\ny\nX\ny\nx\n",
		},
		"hint on the line itself": {
			[]DiffBlock{{StartLine: 5, Search: "x", Replace: "X"}},
			"x\ny\nx\ny\nX\n",
		},
		"several lines": {
			[]DiffBlock{{Search: "y\nx", Replace: "z"}},
			"x\nz\ny\nx\n",
		},
		"blocks apply in order": {
			[]DiffBlock{{Search: "x", Replace: "a"}, {Search: "a\ny", Replace: "b"}},
			"b\nx\ny\nx\n",
		},
		"literal special characters": {
			[]DiffBlock{{Search: "x\n", Replace: "$1(.)\n"}},
			"$1(.)\ny\nx\ny\nx\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, result.Of(tc.want), applyBlocks()(tc.blocks)(content))
		})
	}
}

func TestApplyBlocks_NoMatch(t *testing.T) {
	_, err := result.Unwrap(applyBlocks()([]DiffBlock{{Search: "x", Replace: "y"}, {Search: "q", Replace: "r"}})("x\n"))
	assert.ErrorContains(t, err, "block 2 of the diff does not match")
}

func TestApplyDiff(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeApplyDiffDeps(root, map[string]string{
		filepath.Join("dir", "x.go"): "package x\n\nfunc F() {}\n\nfunc G() {}\n",
	})

	diff := "<<<<<<< SEARCH\n:start_line:3\n-------\nfunc F() {}\n=======\nfunc F() int { return 1 }\n>>>>>>> REPLACE\n\n" +
		"<<<<<<< SEARCH\n:start_line:5\n-------\nfunc G() {}\n=======\n>>>>>>> REPLACE"

	assert.Equal(t,
		result.Of("Applied 2 block(s) to "+filepath.Join("dir", "x.go")+"."),
		runApplyDiff(t, deps, diffArgs(t, "dir/x.go", diff)))

	assert.Equal(t, map[string]string{filepath.Join(root, "dir", "x.go"): "package x\n\nfunc F() int { return 1 }\n\n\n"}, fs.files)
}

func TestApplyDiff_CRLF(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeApplyDiffDeps(root, map[string]string{"x.txt": "a\r\nb\r\nc\r\n"})

	// a search over several lines matches, and the file keeps its line endings
	assert.Equal(t,
		result.Of("Applied 1 block(s) to x.txt."),
		runApplyDiff(t, deps, diffArgs(t, "x.txt", "<<<<<<< SEARCH\na\nb\n=======\na\nB\nb\n>>>>>>> REPLACE")))

	assert.Equal(t, map[string]string{filepath.Join(root, "x.txt"): "a\r\nB\r\nb\r\nc\r\n"}, fs.files)
}

func TestApplyDiff_Failures(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeApplyDiffDeps(root, map[string]string{"x.txt": "hello\n"})
	block := "<<<<<<< SEARCH\nhello\n=======\nbye\n>>>>>>> REPLACE"

	for name, tc := range map[string]struct {
		arguments string
		want      string
	}{
		"invalid json": {`{`, "unexpected end of JSON input"},
		"no blocks":    {diffArgs(t, "x.txt", "hello"), "the diff has no SEARCH/REPLACE block"},
		"empty path":   {diffArgs(t, "", block), "path must not be empty"},
		"outside":      {diffArgs(t, "../y.txt", block), "is outside the workspace"},
		"missing file": {diffArgs(t, "y.txt", block), "y.txt"},
		// the first block matches, the second doesn't: nothing is written
		"no match": {diffArgs(t, "x.txt", block+"\n<<<<<<< SEARCH\nnope\n=======\nyes\n>>>>>>> REPLACE"), "block 2 of the diff does not match"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := result.Unwrap(runApplyDiff(t, deps, tc.arguments))
			assert.ErrorContains(t, err, tc.want)
		})
	}

	assert.Empty(t, fs.files)
}

func TestApplyDiff_WriteError(t *testing.T) {
	root := t.TempDir()
	fs := &fakeFileSystem{files: map[string]string{}}
	failing := func(string) ioresult.Kleisli[[]byte, []byte] {
		return func([]byte) ioresult.IOResult[[]byte] {
			return ioresult.Left[[]byte](errors.New("disk full"))
		}
	}
	deps := MakeApplyDiffDeps(
		MakeWorkspaceDeps(root),
		makeFakeFileDeps(map[string]string{filepath.Join(root, "x.txt"): "a"}),
		MakeFileWriteDeps(fs.makeDirs, failing),
	)

	_, err := result.Unwrap(runApplyDiff(t, deps, diffArgs(t, "x.txt", "<<<<<<< SEARCH\na\n=======\nb\n>>>>>>> REPLACE")))
	assert.EqualError(t, err, "disk full")
}

// TestApplyDiff_OperatingSystem edits a real file through the default deps.
func TestApplyDiff_OperatingSystem(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "x.txt")
	require.NoError(t, os.WriteFile(path, []byte("one\ntwo\nthree\n"), 0o644))
	deps := MakeApplyDiffDeps(MakeWorkspaceDeps(root), MakeDefaultFileDeps(), MakeDefaultFileWriteDeps())

	assert.Equal(t,
		result.Of("Applied 1 block(s) to x.txt."),
		runApplyDiff(t, deps, diffArgs(t, path, "<<<<<<< SEARCH\n:start_line:2\n-------\ntwo\n=======\n2\n>>>>>>> REPLACE")))

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "one\n2\nthree\n", string(content))
}

func TestMakeApplyDiffTool(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeApplyDiffDeps(root, map[string]string{"x.txt": "a"})

	registry := MakeToolRegistry(MakeApplyDiffTool()(deps))

	call, ok := option.Unwrap(ToToolCaller()(registry)(ApplyDiffName))
	require.True(t, ok)
	assert.Equal(t, "Applied 1 block(s) to x.txt.", runCall(t, call, diffArgs(t, "x.txt", "<<<<<<< SEARCH\na\n=======\nb\n>>>>>>> REPLACE")))
	assert.Equal(t, map[string]string{filepath.Join(root, "x.txt"): "b"}, fs.files)

	definition := toolDefinition(registry[ApplyDiffName])
	assert.Equal(t, ApplyDiffName, definition.Name)
	assert.Equal(t, []string{"path", "diff"}, definition.Parameters["required"])

	properties := definition.Parameters["properties"].(map[string]any)
	require.Len(t, properties, 2)
	assert.Contains(t, properties["path"].(map[string]any)["description"], root)
}

func TestAsApplyDiffDeps(t *testing.T) {
	deps := MakeApplyDiffDeps(MakeWorkspaceDeps(t.TempDir()), MakeDefaultFileDeps(), MakeDefaultFileWriteDeps())

	assert.Equal(t, deps, AsApplyDiffDeps(deps))
}

func TestMakeApplyDiffDeps(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeApplyDiffDeps(root, nil)

	assert.Equal(t, root, deps.GetWorkspaceRoot())

	// the writer of the deps is the one passed in
	assert.Equal(t, result.Of([]byte("b")), deps.GetWriteFile()("p")([]byte("b"))())
	assert.Equal(t, map[string]string{"p": "b"}, fs.files)
}

func TestParseStartLine(t *testing.T) {
	assert.Equal(t, result.Of(0), parseStartLine()(""))
	assert.Equal(t, result.Of(0), parseStartLine()("0"))
	assert.Equal(t, result.Of(42), parseStartLine()("42"))

	_, err := result.Unwrap(parseStartLine()("x"))
	assert.ErrorIs(t, err, strconv.ErrSyntax)

	_, err = result.Unwrap(parseStartLine()("99999999999999999999"))
	assert.ErrorIs(t, err, strconv.ErrRange)
}

func TestAndThenSet(t *testing.T) {
	lenses := MakeDiffBlockLenses()

	set := andThenSet(lenses.Replace.Set)(lenses.Search.Set("s"))("r")

	assert.Equal(t, DiffBlock{Search: "s", Replace: "r"}, set(DiffBlock{}))
	// a field that neither setter touches is kept
	assert.Equal(t, DiffBlock{StartLine: 7, Search: "s", Replace: "r"}, set(DiffBlock{StartLine: 7, Search: "old", Replace: "old"}))
}

func TestMakeDiffBlock(t *testing.T) {
	assert.Equal(t, DiffBlock{StartLine: 5, Search: "s", Replace: "r"}, makeDiffBlock()("s")("r")(5))
	assert.Equal(t, DiffBlock{}, makeDiffBlock()("")("")(0))
}

func TestBlockOf(t *testing.T) {
	assert.Equal(t, result.Of(DiffBlock{StartLine: 12, Search: "s", Replace: "r"}), blockOf([]string{"match", "12", "s", "r"}))
	assert.Equal(t, result.Of(DiffBlock{Search: "s"}), blockOf([]string{"match", "", "s", ""}))

	_, err := result.Unwrap(blockOf([]string{"match", "99999999999999999999", "s", "r"}))
	assert.ErrorIs(t, err, strconv.ErrRange)
}

func TestParseBlocks_EdgeCases(t *testing.T) {
	for name, tc := range map[string]struct {
		diff string
		want []DiffBlock
	}{
		"trailing whitespace after the markers": {
			"<<<<<<< SEARCH  \n:start_line: 3 \n------- \nfoo\n=======\t\nbar\n>>>>>>> REPLACE  ",
			[]DiffBlock{{StartLine: 3, Search: "foo", Replace: "bar"}},
		},
		"text before the first block": {
			"Here is the diff:\n<<<<<<< SEARCH\nfoo\n=======\nbar\n>>>>>>> REPLACE",
			[]DiffBlock{{Search: "foo", Replace: "bar"}},
		},
		"indentation and inner empty lines are kept": {
			"<<<<<<< SEARCH\n\tfoo\n\n\tbar\n=======\n\tbaz\n>>>>>>> REPLACE",
			[]DiffBlock{{Search: "\tfoo\n\n\tbar", Replace: "\tbaz"}},
		},
		"replace over several lines": {
			"<<<<<<< SEARCH\nfoo\n=======\na\nb\n>>>>>>> REPLACE",
			[]DiffBlock{{Search: "foo", Replace: "a\nb"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, result.Of(tc.want), parseBlocks()(tc.diff))
		})
	}
}

func TestParseBlocks_MoreFailures(t *testing.T) {
	assert.Equal(t, result.Left[[]DiffBlock](errNoBlocks), parseBlocks()(""))
	// a marker that is not on a line of its own doesn't start a block
	assert.Equal(t, result.Left[[]DiffBlock](errNoBlocks), parseBlocks()("x <<<<<<< SEARCH\nfoo\n=======\nbar\n>>>>>>> REPLACE"))
	// one empty block fails the whole diff
	assert.Equal(t, result.Left[[]DiffBlock](errEmptyBlock), parseBlocks()(
		"<<<<<<< SEARCH\nfoo\n=======\nbar\n>>>>>>> REPLACE\n<<<<<<< SEARCH\n-------\n=======\nbar\n>>>>>>> REPLACE"))

	_, err := result.Unwrap(parseBlocks()("<<<<<<< SEARCH\n:start_line:99999999999999999999\n-------\nfoo\n=======\nbar\n>>>>>>> REPLACE"))
	assert.ErrorIs(t, err, strconv.ErrRange)
}

func TestLineDistance(t *testing.T) {
	content := "a\nb\nc\n"

	assert.Equal(t, 0, lineDistance(1, content, []int{0, 1}))
	assert.Equal(t, 0, lineDistance(3, content, []int{4, 5}))
	assert.Equal(t, 2, lineDistance(1, content, []int{4, 5}))
	assert.Equal(t, 2, lineDistance(3, content, []int{0, 1}))
	// without a hint the distance is the line number itself
	assert.Equal(t, 2, lineDistance(0, content, []int{2, 3}))
}

func TestMatchesOf(t *testing.T) {
	assert.Equal(t, [][]int{{0, 2}, {3, 5}}, matchesOf()("a.")("a.ba."))
	// the search is literal: the dot matches only a dot
	assert.Empty(t, matchesOf()("a.")("ab"))
	// occurrences don't overlap
	assert.Equal(t, [][]int{{0, 2}}, matchesOf()("aa")("aaa"))
}

func TestNearestMatch(t *testing.T) {
	content := "x\ny\nx\ny\nx\n"

	for name, tc := range map[string]struct {
		block DiffBlock
		want  Option[[]int]
	}{
		"no hint is the first":            {DiffBlock{Search: "x"}, option.Some([]int{0, 1})},
		"the nearest":                     {DiffBlock{StartLine: 3, Search: "x"}, option.Some([]int{4, 5})},
		"a hint past the end is the last": {DiffBlock{StartLine: 100, Search: "x"}, option.Some([]int{8, 9})},
		"a tie goes to the first":         {DiffBlock{StartLine: 2, Search: "x"}, option.Some([]int{0, 1})},
		"none":                            {DiffBlock{Search: "z"}, option.None[[]int]()},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, nearestMatch()(tc.block)(content))
		})
	}
}

func TestSplice(t *testing.T) {
	assert.Equal(t, "aXc", splice("X", "abc", []int{1, 2}))
	assert.Equal(t, "ac", splice("", "abc", []int{1, 2}))
	assert.Equal(t, "Xabc", splice("X", "abc", []int{0, 0}))
	assert.Equal(t, "X", splice("X", "abc", []int{0, 3}))
}

func TestNoMatch(t *testing.T) {
	assert.ErrorContains(t, noMatch(0)(), "block 1 of the diff does not match")
	assert.ErrorContains(t, noMatch(2)(), "block 3 of the diff does not match")
}

func TestApplyBlock(t *testing.T) {
	assert.Equal(t, result.Of("a\nB\nb\n"), applyBlock(0, DiffBlock{StartLine: 2, Search: "b", Replace: "B"})("a\nb\nb\n"))
	assert.Equal(t, result.Of("a\nb\nB\n"), applyBlock(0, DiffBlock{StartLine: 3, Search: "b", Replace: "B"})("a\nb\nb\n"))
	// only one occurrence is replaced
	assert.Equal(t, result.Of("A\na\n"), applyBlock(0, DiffBlock{Search: "a", Replace: "A"})("a\na\n"))

	_, err := result.Unwrap(applyBlock(4, DiffBlock{Search: "z", Replace: "Z"})("a\n"))
	assert.ErrorContains(t, err, "block 5 of the diff does not match")
}

func TestThenBlock(t *testing.T) {
	b := DiffBlock{Search: "b", Replace: "B"}

	assert.Equal(t, result.Of("aB"), thenBlock(1, result.Of[string], b)("ab"))
	// the block runs on the result of the ones before it
	assert.Equal(t, result.Of("XB"), thenBlock(1, applyBlock(0, DiffBlock{Search: "a", Replace: "X"}), b)("ab"))

	// a failure before the block is kept
	failed := errors.New("earlier")
	assert.Equal(t, result.Left[string](failed), thenBlock(1, F.Constant1[string](result.Left[string](failed)), b)("ab"))

	_, err := result.Unwrap(thenBlock(1, result.Of[string], DiffBlock{Search: "z"})("ab"))
	assert.ErrorContains(t, err, "block 2 of the diff does not match")
}

func TestApplyBlocks_NoBlocks(t *testing.T) {
	assert.Equal(t, result.Of("abc"), applyBlocks()(nil)("abc"))
}

func TestApplyBlocks_FirstFailureWins(t *testing.T) {
	_, err := result.Unwrap(applyBlocks()([]DiffBlock{{Search: "q"}, {Search: "r"}})("x"))
	assert.ErrorContains(t, err, "block 1 of the diff does not match")
}

func TestPatchedContent(t *testing.T) {
	assert.Equal(t,
		result.Of("A\nb\n"),
		patchedContent()(ApplyDiffArgs{Blocks: []DiffBlock{{Search: "a", Replace: "A"}}, Content: "a\nb\n"}))

	_, err := result.Unwrap(patchedContent()(ApplyDiffArgs{Blocks: []DiffBlock{{Search: "z"}}, Content: "a\n"}))
	assert.ErrorContains(t, err, "block 1 of the diff does not match")
}

func TestPatchedText(t *testing.T) {
	assert.Equal(t, "a\nb\n", patchedText()(ApplyDiffArgs{Content: "a\nb\n"}))
	assert.Equal(t, "a\r\nb\r\n", patchedText()(ApplyDiffArgs{Content: "a\nb\n", CRLF: true}))
}

func TestWritePatchedWith(t *testing.T) {
	fs := &fakeFileSystem{files: map[string]string{}}

	assert.Equal(t,
		result.Of([]byte("a\r\n")),
		writePatchedWith()(fs.writeFile)(ApplyDiffArgs{Path: "p", Content: "a\n", CRLF: true})())
	assert.Equal(t, map[string]string{"p": "a\r\n"}, fs.files)
}

func TestWritePatched(t *testing.T) {
	fs, deps := makeFakeApplyDiffDeps(t.TempDir(), nil)

	assert.Equal(t,
		result.Of([]byte("b\n")),
		writePatched()(ApplyDiffArgs{Path: "p", Content: "b\n"})(deps)(t.Context())())
	assert.Equal(t, map[string]string{"p": "b\n"}, fs.files)
}

func TestAppliedText(t *testing.T) {
	assert.Equal(t, "Applied 0 block(s) to x.txt.", appliedText(ApplyDiffArgs{Name: "x.txt"}))
	assert.Equal(t, "Applied 2 block(s) to a/b.", appliedText(ApplyDiffArgs{Blocks: make([]DiffBlock, 2), Name: "a/b"}))
}

func TestApplyDiffDefinition(t *testing.T) {
	definition := applyDiffDefinition("/the/root")

	assert.Equal(t, ApplyDiffName, definition.Name)
	assert.Contains(t, definition.Description.Value, "<<<<<<< SEARCH")
	assert.Equal(t, "object", definition.Parameters["type"])
	assert.Equal(t, []string{"path", "diff"}, definition.Parameters["required"])

	properties := definition.Parameters["properties"].(map[string]any)
	assert.Contains(t, properties["path"].(map[string]any)["description"], "/the/root")
	assert.Equal(t, "string", properties["diff"].(map[string]any)["type"])
}

func TestApplyDiff_AbsolutePath(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeApplyDiffDeps(root, map[string]string{"x.txt": "a"})

	assert.Equal(t,
		result.Of("Applied 1 block(s) to x.txt."),
		runApplyDiff(t, deps, diffArgs(t, filepath.Join(root, "x.txt"), "<<<<<<< SEARCH\na\n=======\nb\n>>>>>>> REPLACE")))
	assert.Equal(t, map[string]string{filepath.Join(root, "x.txt"): "b"}, fs.files)
}

func TestApplyDiff_CRLFDiff(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeApplyDiffDeps(root, map[string]string{"x.txt": "a\nb\n"})

	// a diff with CRLF line endings edits an LF file, which stays LF
	assert.Equal(t,
		result.Of("Applied 1 block(s) to x.txt."),
		runApplyDiff(t, deps, diffArgs(t, "x.txt", "<<<<<<< SEARCH\r\na\r\nb\r\n=======\r\nc\r\n>>>>>>> REPLACE\r\n")))
	assert.Equal(t, map[string]string{filepath.Join(root, "x.txt"): "c\n"}, fs.files)
}

func TestApplyDiff_IgnoresInternalFields(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeApplyDiffDeps(root, map[string]string{"x.txt": "a"})

	// the fields without a JSON name can't be set by the call
	arguments := `{"path":"x.txt","diff":"<<<<<<< SEARCH\na\n=======\nb\n>>>>>>> REPLACE","Content":"evil","Name":"evil","CRLF":true}`

	assert.Equal(t, result.Of("Applied 1 block(s) to x.txt."), runApplyDiff(t, deps, arguments))
	assert.Equal(t, map[string]string{filepath.Join(root, "x.txt"): "b"}, fs.files)
}

func TestApplyDiff_MoreFailures(t *testing.T) {
	root := t.TempDir()
	fs, deps := makeFakeApplyDiffDeps(root, map[string]string{"x.txt": "hello\n"})

	for name, tc := range map[string]struct {
		arguments string
		want      string
	}{
		"empty search":   {diffArgs(t, "x.txt", "<<<<<<< SEARCH\n=======\nbye\n>>>>>>> REPLACE"), errEmptyBlock.Error()},
		"bad start line": {diffArgs(t, "x.txt", "<<<<<<< SEARCH\n:start_line:99999999999999999999\n-------\nhello\n=======\nbye\n>>>>>>> REPLACE"), "value out of range"},
		"no match":       {diffArgs(t, "x.txt", "<<<<<<< SEARCH\nnope\n=======\nyes\n>>>>>>> REPLACE"), "block 1 of the diff does not match"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := result.Unwrap(runApplyDiff(t, deps, tc.arguments))
			assert.ErrorContains(t, err, tc.want)
		})
	}

	assert.Empty(t, fs.files)
}
