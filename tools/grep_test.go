package tools

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/option"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	gitignore "github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTree writes the files below root, each one hour newer than the one
// before in the order of names, so that the newest-first order is known.
func writeTree(t *testing.T, root string, names []string, files map[string]string) {
	t.Helper()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, name := range names {
		p := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
		require.NoError(t, os.WriteFile(p, []byte(files[name]), 0o600))
		modTime := start.Add(time.Duration(i) * time.Hour)
		require.NoError(t, os.Chtimes(p, modTime, modTime))
	}
}

func makeOSGrepDeps(root string) GrepDeps {
	return MakeGrepDeps(MakeWorkspaceDeps(root), MakeDefaultWalkDeps(), MakeDefaultFileDeps())
}

func runGrep(t *testing.T, deps GrepDeps, arguments string) result.Result[string] {
	t.Helper()

	return Grep()(arguments)(deps)(t.Context())()
}

func TestCompilePattern(t *testing.T) {
	re, err := result.Unwrap(compilePattern()(GrepArgs{Pattern: "foo"}))
	require.NoError(t, err)
	assert.True(t, re.MatchString("a foo b"))
	assert.False(t, re.MatchString("FOO"))

	re, err = result.Unwrap(compilePattern()(GrepArgs{Pattern: "foo", IgnoreCase: true}))
	require.NoError(t, err)
	assert.True(t, re.MatchString("FOO"))

	_, err = result.Unwrap(compilePattern()(GrepArgs{Pattern: "("}))
	assert.ErrorContains(t, err, "grep pattern is not a valid regular expression")
}

func TestIncludeMatcher(t *testing.T) {
	all, err := result.Unwrap(includeMatcher()(""))
	require.NoError(t, err)
	assert.True(t, all("x/y.md"))

	goFiles, err := result.Unwrap(includeMatcher()("*.{go,mod}"))
	require.NoError(t, err)
	assert.True(t, goFiles("x/a.go"))
	assert.True(t, goFiles("go.mod"))
	assert.False(t, goFiles("a.md"))
}

func TestIgnoreDomain(t *testing.T) {
	assert.Nil(t, ignoreDomain()(FileEntry{Path: ".gitignore"}))
	assert.Equal(t, []string{"a", "b"}, ignoreDomain()(FileEntry{Path: "a/b/.gitignore"}))
}

func TestParseIgnore(t *testing.T) {
	patterns := parseIgnore(nil)([]byte("# comment\r\n\r\n*.log\r\n!keep.log\n  \n"))
	require.Len(t, patterns, 2)

	keep := notIgnored(nil, patterns)
	assert.False(t, keep(FileEntry{Path: "x/a.log"}))
	assert.True(t, keep(FileEntry{Path: "keep.log"}))
	assert.True(t, keep(FileEntry{Path: "a.go"}))
}

func TestNotIgnored(t *testing.T) {
	patterns := []gitignore.Pattern{
		gitignore.ParsePattern("build/", nil),
		gitignore.ParsePattern("*.tmp", []string{"sub"}),
	}
	keep := notIgnored(nil, patterns)

	// a directory pattern excludes the files below it, but not a file of that name
	assert.False(t, keep(FileEntry{Path: "build/x.go"}))
	assert.True(t, keep(FileEntry{Path: "build"}))
	// a nested pattern applies only below its directory
	assert.False(t, keep(FileEntry{Path: "sub/a.tmp"}))
	assert.True(t, keep(FileEntry{Path: "a.tmp"}))

	// entries of a searched subdirectory are matched by their workspace path
	inSub := notIgnored([]string{"sub"}, patterns)
	assert.False(t, inSub(FileEntry{Path: "a.tmp"}))
	assert.True(t, inSub(FileEntry{Path: "a.go"}))
}

func TestSegments(t *testing.T) {
	assert.Nil(t, segments()("."))
	assert.Equal(t, []string{"a"}, segments()("a"))
	assert.Equal(t, []string{"a", "b"}, segments()(filepath.Join("a", "b")))
}

func TestIgnoreFiles(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "ws")
	args := GrepArgs{
		Name: filepath.Join("a", "b"),
		Scope: GrepScope{
			Dir: filepath.Join(root, "a", "b"),
			Entries: []FileEntry{
				{Path: "c/d/.gitignore"},
				{Path: ".gitignore"},
				{Path: "c/.gitignore"},
				{Path: "x.go"},
				{Path: "e/.gitignore", IsDir: true},
			},
		},
	}

	// exclude first, then root down to the searched directory, then below it;
	// the .gitignore of the searched directory is listed once
	assert.Equal(t, []IgnoreFile{
		{Path: filepath.Join(root, ".git", "info", "exclude")},
		{Path: filepath.Join(root, ".gitignore"), Domain: nil},
		{Path: filepath.Join(root, "a", ".gitignore"), Domain: []string{"a"}},
		{Path: filepath.Join(root, "a", "b", ".gitignore"), Domain: []string{"a", "b"}},
		{Path: filepath.Join(root, "a", "b", "c", ".gitignore"), Domain: []string{"a", "b", "c"}},
		{Path: filepath.Join(root, "a", "b", "c", "d", ".gitignore"), Domain: []string{"a", "b", "c", "d"}},
	}, ignoreFiles(root)(args))

	// at the workspace root
	assert.Equal(t, []IgnoreFile{
		{Path: filepath.Join(root, ".git", "info", "exclude")},
		{Path: filepath.Join(root, ".gitignore"), Domain: nil},
	}, ignoreFiles(root)(GrepArgs{Name: ".", Scope: GrepScope{Dir: root}}))
}

func TestCompilePattern_WordRegexp(t *testing.T) {
	re, err := result.Unwrap(compilePattern()(GrepArgs{Pattern: "foo|bar", WordRegexp: true, IgnoreCase: true}))
	require.NoError(t, err)

	// the boundaries surround the whole alternation
	assert.True(t, re.MatchString("a FOO b"))
	assert.True(t, re.MatchString("bar"))
	assert.False(t, re.MatchString("food"))
	assert.False(t, re.MatchString("rebar"))
}

func TestLineMatcher(t *testing.T) {
	re := regexp.MustCompile("x")

	assert.True(t, lineMatcher()(GrepArgs{Regexp: re})("ax"))
	assert.False(t, lineMatcher()(GrepArgs{Regexp: re})("a"))
	assert.False(t, lineMatcher()(GrepArgs{Regexp: re, InvertMatch: true})("ax"))
	assert.True(t, lineMatcher()(GrepArgs{Regexp: re, InvertMatch: true})("a"))
}

func TestByDepth(t *testing.T) {
	entries := []FileEntry{{Path: "a/b/.gitignore"}, {Path: ".gitignore"}, {Path: "a/.gitignore"}}

	assert.Equal(t,
		[]string{".gitignore", "a/.gitignore", "a/b/.gitignore"},
		A.Map(MakeFileEntryPathLens().Get)(A.SortBy(byDepth())(entries)))
}

func TestGrepContent(t *testing.T) {
	match := regexp.MustCompile("fo+").MatchString

	assert.Equal(t,
		[]GrepLine{{File: "a.go", Number: 1, Text: "foo"}, {File: "a.go", Number: 3, Text: "a fo"}},
		grepContent(match)("a.go")([]byte("foo\r\nbar\r\na fo\r\n")))

	// binary content has no matches
	assert.Empty(t, grepContent(match)("a.bin")([]byte("foo\x00")))
}

func TestRenderMatches(t *testing.T) {
	lines := []GrepLine{
		{File: "b.go", Number: 3, Text: "x"},
		{File: "a.go", Number: 1, Text: "\ty"},
		{File: "b.go", Number: 9, Text: "z"},
	}

	assert.Equal(t,
		"Found 3 matches\nb.go:\n  Line 3: x\n  Line 9: z\n\na.go:\n  Line 1: \ty",
		renderMatches()(lines))
	assert.Equal(t, noMatches, renderMatches()(nil))
}

func TestRenderFiles(t *testing.T) {
	lines := []GrepLine{{File: "b.go", Number: 3}, {File: "a.go", Number: 1}, {File: "b.go", Number: 9}}

	assert.Equal(t, "Found 2 files with matches\nb.go\na.go", renderFiles()(lines))
	assert.Equal(t, noMatches, renderFiles()(nil))
}

func TestRenderMatches_Cap(t *testing.T) {
	lines := make([]GrepLine, maxGrepResults+1)
	for i := range lines {
		lines[i] = GrepLine{File: "a.go", Number: i + 1, Text: "x"}
	}

	out := renderMatches()(lines)
	assert.True(t, strings.HasPrefix(out, "Found 101 matches\na.go:\n"))
	assert.Contains(t, out, "  Line 100: x")
	assert.NotContains(t, out, "  Line 101: x")
	assert.True(t, strings.HasSuffix(out, matchesTruncated))

	// exactly at the cap: no note
	assert.NotContains(t, renderMatches()(lines[:maxGrepResults]), "Showing")
}

func TestRenderFiles_Cap(t *testing.T) {
	lines := make([]GrepLine, maxGrepResults+1)
	for i := range lines {
		lines[i] = GrepLine{File: fmt.Sprintf("f%03d.go", i), Number: 1}
	}

	out := renderFiles()(lines)
	assert.True(t, strings.HasPrefix(out, "Found 101 files with matches\nf000.go\n"))
	assert.Contains(t, out, "f099.go")
	assert.NotContains(t, out, "f100.go")
	assert.True(t, strings.HasSuffix(out, filesTruncated))
}

func TestGrep(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root,
		[]string{"a.go", "docs/b.md", "tools/c.go", "bin.dat"},
		map[string]string{
			"a.go":       "package main\n\nfunc Main() {}\n",
			"docs/b.md":  "# main\n",
			"tools/c.go": "package tools\n// main entry\nfunc main() {}\n",
			"bin.dat":    "main\x00",
		})
	deps := makeOSGrepDeps(root)
	c := filepath.Join("tools", "c.go")
	b := filepath.Join("docs", "b.md")

	// grouped by file, the newest file first; the binary file is skipped
	assert.Equal(t,
		result.Of("Found 4 matches\n"+c+":\n  Line 2: // main entry\n  Line 3: func main() {}\n\n"+b+":\n  Line 1: # main\n\na.go:\n  Line 1: package main"),
		runGrep(t, deps, `{"pattern":"main"}`))

	assert.Equal(t,
		result.Of("Found 3 files with matches\n"+c+"\n"+b+"\na.go"),
		runGrep(t, deps, `{"pattern":"main","files_with_matches":true}`))

	assert.Equal(t,
		result.Of("Found 2 matches\na.go:\n  Line 1: package main\n  Line 3: func Main() {}"),
		runGrep(t, deps, `{"pattern":"main","include":"a.*","ignore_case":true}`))

	// a directory narrows the search, the names stay relative to the workspace
	assert.Equal(t,
		result.Of("Found 1 files with matches\n"+b),
		runGrep(t, deps, `{"pattern":"main","path":"docs","files_with_matches":true}`))

	assert.Equal(t, result.Of(noMatches), runGrep(t, deps, `{"pattern":"nowhere"}`))
}

func TestGrep_File(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root,
		[]string{"tools/c.go", "tools/d.go"},
		map[string]string{"tools/c.go": "x := 1\n", "tools/d.go": "x := 2\n"})
	deps := makeOSGrepDeps(root)
	c := filepath.Join("tools", "c.go")

	assert.Equal(t,
		result.Of("Found 1 matches\n"+c+":\n  Line 1: x := 1"),
		runGrep(t, deps, `{"pattern":"x","path":"tools/c.go"}`))

	// an absolute path to the file
	assert.Equal(t,
		result.Of("Found 1 files with matches\n"+c),
		runGrep(t, deps, `{"pattern":"x","files_with_matches":true,"path":`+strconv.Quote(filepath.Join(root, c))+`}`))
}

func TestGrep_Gitignore(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root,
		[]string{".gitignore", "a.go", "a.log", "build/b.go", "sub/.gitignore", "sub/keep.log", "sub/c.tmp", "c.tmp", ".git/config"},
		map[string]string{
			".gitignore":     "# outputs\n*.log\nbuild/\n",
			"a.go":           "hit\n",
			"a.log":          "hit\n",
			"build/b.go":     "hit\n",
			"sub/.gitignore": "!keep.log\n*.tmp\n",
			"sub/keep.log":   "hit\n",
			"sub/c.tmp":      "hit\n",
			"c.tmp":          "hit\n",
			".git/config":    "hit\n",
		})
	deps := makeOSGrepDeps(root)

	// ignored files, ignored directories and .git are skipped; a nested
	// .gitignore re-includes below its directory and its patterns stay there
	assert.Equal(t,
		result.Of("Found 3 files with matches\nc.tmp\n"+filepath.Join("sub", "keep.log")+"\na.go"),
		runGrep(t, deps, `{"pattern":"hit","files_with_matches":true}`))

	// the .gitignore files of the searched directory and above it apply
	assert.Equal(t,
		result.Of("Found 1 files with matches\n"+filepath.Join("sub", "keep.log")),
		runGrep(t, deps, `{"pattern":"hit","path":"sub","files_with_matches":true}`))
}

func TestGrep_GitignoreAbove(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root,
		[]string{".gitignore", "a/.gitignore", "a/b/x.log", "a/b/x.tmp", "a/b/x.go", "a/b/gen/y.go"},
		map[string]string{
			".gitignore":   "*.log\n",
			"a/.gitignore": "*.tmp\n/b/gen/\n",
			"a/b/x.log":    "hit\n",
			"a/b/x.tmp":    "hit\n",
			"a/b/x.go":     "hit\n",
			"a/b/gen/y.go": "hit\n",
		})
	deps := makeOSGrepDeps(root)
	x := filepath.Join("a", "b", "x.go")

	// patterns of the workspace root and of a/ apply when searching a/b,
	// including the anchored /b/gen/ of a/.gitignore
	assert.Equal(t,
		result.Of("Found 1 files with matches\n"+x),
		runGrep(t, deps, `{"pattern":"hit","path":"a/b","files_with_matches":true}`))

	// and when searching a single file below an ignored directory's sibling
	assert.Equal(t,
		result.Of("Found 1 files with matches\n"+x),
		runGrep(t, deps, `{"pattern":"hit","path":"a/b/x.go","files_with_matches":true}`))
}

func TestGrep_GitInfoExclude(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root,
		[]string{".git/info/exclude", "a.go", "local/b.go", "sub/.gitignore", "sub/local/c.go"},
		map[string]string{
			".git/info/exclude": "# personal\nlocal/\n",
			"a.go":              "hit\n",
			"local/b.go":        "hit\n",
			"sub/.gitignore":    "!local/\n",
			"sub/local/c.go":    "hit\n",
		})
	deps := makeOSGrepDeps(root)

	// exclude applies to the whole workspace, at the lowest priority:
	// a .gitignore can re-include what it excludes
	assert.Equal(t,
		result.Of("Found 2 files with matches\n"+filepath.Join("sub", "local", "c.go")+"\na.go"),
		runGrep(t, deps, `{"pattern":"hit","files_with_matches":true}`))

	assert.Equal(t, result.Of(noMatches), runGrep(t, deps, `{"pattern":"hit","path":"local"}`))
}

func TestGrep_WordAndInvert(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, []string{"a.go"}, map[string]string{"a.go": "foo\nfood\nbar foo\n"})
	deps := makeOSGrepDeps(root)

	assert.Equal(t,
		result.Of("Found 2 matches\na.go:\n  Line 1: foo\n  Line 3: bar foo"),
		runGrep(t, deps, `{"pattern":"foo","word_regexp":true}`))

	// the final line break does not add an empty line that would match
	assert.Equal(t,
		result.Of("Found 1 matches\na.go:\n  Line 2: food"),
		runGrep(t, deps, `{"pattern":"foo","word_regexp":true,"invert_match":true}`))

	assert.Equal(t,
		result.Of("Found 1 files with matches\na.go"),
		runGrep(t, deps, `{"pattern":"zzz","invert_match":true,"files_with_matches":true}`))
}

func TestGrep_Failures(t *testing.T) {
	root := t.TempDir()
	deps := makeOSGrepDeps(root)

	for name, tc := range map[string]struct {
		arguments string
		want      string
	}{
		"invalid json": {`{`, "unexpected end of JSON input"},
		"bad pattern":  {`{"pattern":"("}`, "is not a valid regular expression"},
		"bad include":  {`{"pattern":"x","include":"[a"}`, "is malformed"},
		"outside":      {`{"pattern":"x","path":".."}`, "is outside the workspace"},
		"missing path": {`{"pattern":"x","path":"missing"}`, "cannot list directory"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := result.Unwrap(runGrep(t, deps, tc.arguments))
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestGrepDefinition(t *testing.T) {
	definition := grepDefinition("/ws")

	assert.Equal(t, GrepName, definition.Name)
	assert.Contains(t, definition.Description.Value, "Caps at 100 matches")
	assert.Equal(t, []string{"pattern"}, definition.Parameters["required"])

	properties := definition.Parameters["properties"].(map[string]any)
	assert.Len(t, properties, 7)
	assert.Equal(t, "boolean", properties["ignore_case"].(map[string]any)["type"])
	assert.Equal(t, "boolean", properties["invert_match"].(map[string]any)["type"])
	assert.Equal(t, "boolean", properties["word_regexp"].(map[string]any)["type"])
	assert.Equal(t, "boolean", properties["files_with_matches"].(map[string]any)["type"])
	assert.Contains(t, properties["path"].(map[string]any)["description"], "The workspace is at /ws.")
}

func TestMakeGrepTool(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, []string{"a.go"}, map[string]string{"a.go": "hit\n"})

	registry := MakeToolRegistry(MakeGrepTool()(makeOSGrepDeps(root)))

	call, ok := option.Unwrap(ToToolCaller()(registry)(GrepName))
	require.True(t, ok)
	assert.Equal(t, "Found 1 matches\na.go:\n  Line 1: hit", runCall(t, call, `{"pattern":"hit"}`))
	assert.Equal(t, GrepName, toolDefinition(registry[GrepName]).Name)
}

func TestAsGrepDeps(t *testing.T) {
	deps := makeOSGrepDeps(t.TempDir())

	assert.Equal(t, deps, AsGrepDeps(deps))
	assert.Equal(t, FileDeps(deps), AsFileDeps(deps))
}

// fakeRoot is the workspace root of the tests over in-memory dependencies.
var fakeRoot = filepath.Join(string(filepath.Separator), "ws")

// makeFakeGrepDeps serves the in-memory file system files as the workspace
// at fakeRoot and the file contents keyed by absolute path.
func makeFakeGrepDeps(files fstest.MapFS, contents map[string]string) GrepDeps {
	return MakeGrepDeps(MakeWorkspaceDeps(fakeRoot), makeFakeWalkDeps(fakeRoot, files), makeFakeFileDeps(contents))
}

func runEffect[T any](t *testing.T, deps GrepDeps, eff Effect[GrepDeps, T]) result.Result[T] {
	t.Helper()

	return eff(deps)(t.Context())()
}

func entryPaths(entries []FileEntry) []string {
	return A.Map(MakeFileEntryPathLens().Get)(entries)
}

func TestMakeGrepDeps(t *testing.T) {
	deps := makeFakeGrepDeps(fstest.MapFS{"a.go": {}}, map[string]string{"x": "y"})

	assert.Equal(t, fakeRoot, deps.GetWorkspaceRoot())
	assert.Equal(t, result.Of([]byte("y")), deps.GetReadFile()("x")())
	assert.Equal(t, []string{"a.go"}, walkedPaths(t, deps.GetWalkDir(), true))
}

func TestMakeGrepScope(t *testing.T) {
	entries := []FileEntry{{Path: "a.go"}}

	assert.Equal(t, GrepScope{Dir: "/d", Entries: entries}, MakeGrepScope("/d", entries))
}

func TestWhenSet(t *testing.T) {
	exclaim := whenSet(S.Append("!"))

	assert.Equal(t, "a!", exclaim(true)("a"))
	assert.Equal(t, "a", exclaim(false)("a"))
}

func TestWordBoundaries(t *testing.T) {
	assert.Equal(t, `\b(?:a|b)\b`, wordBoundaries()("a|b"))
}

func TestMatchString(t *testing.T) {
	match := matchString(regexp.MustCompile("^a+$"))

	assert.True(t, match("aaa"))
	assert.False(t, match("ab"))
}

func TestIsFileNamed(t *testing.T) {
	isGoMod := isFileNamed()("go.mod")

	assert.True(t, isGoMod(FileEntry{Path: "go.mod"}))
	assert.False(t, isGoMod(FileEntry{Path: "go.mod", IsDir: true}))
	assert.False(t, isGoMod(FileEntry{Path: "a/go.mod"}))
	assert.False(t, isGoMod(FileEntry{Path: "go.sum"}))
}

func TestIsGitignore(t *testing.T) {
	is := isGitignore()

	assert.True(t, is(FileEntry{Path: ".gitignore"}))
	assert.True(t, is(FileEntry{Path: "a/b/.gitignore"}))
	assert.False(t, is(FileEntry{Path: "a/.gitignore", IsDir: true}))
	assert.False(t, is(FileEntry{Path: "a/x.gitignore"}))
	assert.False(t, is(FileEntry{Path: ".gitignore/x"}))
}

func TestAbsoluteEntry(t *testing.T) {
	assert.Equal(t, filepath.Join(fakeRoot, "a", "b.go"), absoluteEntry(fakeRoot)(FileEntry{Path: "a/b.go"}))
	assert.Equal(t, filepath.Join(fakeRoot, "c.go"), absoluteEntry(fakeRoot)(FileEntry{Path: "c.go"}))
}

func TestIsPatternLine(t *testing.T) {
	is := isPatternLine()

	assert.True(t, is("*.log"))
	assert.True(t, is("!keep.log"))
	assert.True(t, is(`\#literal`))
	assert.False(t, is(""))
	assert.False(t, is(" \t"))
	assert.False(t, is("# comment"))
}

func TestSkipUnreadable(t *testing.T) {
	deps := makeFakeGrepDeps(nil, nil)

	assert.Equal(t, result.Of([]byte("x")),
		runEffect(t, deps, skipUnreadable()(effect.Of[GrepDeps]([]byte("x")))))
	assert.Equal(t, result.Of[[]byte](nil),
		runEffect(t, deps, skipUnreadable()(effect.Fail[GrepDeps, []byte](errors.New("unreadable")))))
}

func TestReadBytes_GrepDeps(t *testing.T) {
	p := filepath.Join(fakeRoot, "a.go")
	deps := makeFakeGrepDeps(nil, map[string]string{p: "content"})

	assert.Equal(t, result.Of([]byte("content")), runEffect(t, deps, readBytes[GrepDeps](p)))

	_, err := result.Unwrap(runEffect(t, deps, readBytes[GrepDeps](filepath.Join(fakeRoot, "missing"))))
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestReadIgnore(t *testing.T) {
	p := filepath.Join(fakeRoot, "sub", ".gitignore")
	deps := makeFakeGrepDeps(nil, map[string]string{p: "# comment\n*.tmp\n\n!keep.tmp\n"})

	patterns, err := result.Unwrap(runEffect(t, deps, readIgnore()(IgnoreFile{Path: p, Domain: []string{"sub"}})))
	require.NoError(t, err)
	require.Len(t, patterns, 2)

	// the patterns apply within their domain only
	keep := notIgnored(nil, patterns)
	assert.False(t, keep(FileEntry{Path: "sub/a.tmp"}))
	assert.True(t, keep(FileEntry{Path: "sub/keep.tmp"}))
	assert.True(t, keep(FileEntry{Path: "a.tmp"}))

	// a missing file has no patterns
	missing, err := result.Unwrap(runEffect(t, deps, readIgnore()(IgnoreFile{Path: filepath.Join(fakeRoot, ".gitignore")})))
	require.NoError(t, err)
	assert.Empty(t, missing)
}

func TestMakeIgnoreFile(t *testing.T) {
	assert.Equal(t, IgnoreFile{Path: "/p", Domain: []string{"a"}}, makeIgnoreFile("/p", []string{"a"}))
}

func TestExcludeFile(t *testing.T) {
	assert.Equal(t, IgnoreFile{Path: filepath.Join(fakeRoot, ".git", "info", "exclude")}, excludeFile(fakeRoot))
}

func TestInits(t *testing.T) {
	prefixes := inits()([]string{"a", "b"})

	require.Len(t, prefixes, 3)
	assert.Empty(t, prefixes[0])
	assert.Equal(t, []string{"a"}, prefixes[1])
	assert.Equal(t, []string{"a", "b"}, prefixes[2])

	// the empty path has the empty prefix only
	empty := inits()(nil)
	require.Len(t, empty, 1)
	assert.Empty(t, empty[0])
}

func TestAncestorIgnore(t *testing.T) {
	assert.Equal(t,
		IgnoreFile{Path: filepath.Join(fakeRoot, "a", "b", ".gitignore"), Domain: []string{"a", "b"}},
		ancestorIgnore()(fakeRoot)([]string{"a", "b"}))

	root := ancestorIgnore()(fakeRoot)(nil)
	assert.Equal(t, filepath.Join(fakeRoot, ".gitignore"), root.Path)
	assert.Empty(t, root.Domain)
}

func TestAncestorFiles(t *testing.T) {
	files := ancestorFiles()(fakeRoot)([]string{"a", "b"})

	assert.Equal(t, []string{
		filepath.Join(fakeRoot, ".gitignore"),
		filepath.Join(fakeRoot, "a", ".gitignore"),
		filepath.Join(fakeRoot, "a", "b", ".gitignore"),
	}, A.Map(MakeIgnoreFilePathLens().Get)(files))
	assert.Empty(t, files[0].Domain)
	assert.Equal(t, []string{"a"}, files[1].Domain)
	assert.Equal(t, []string{"a", "b"}, files[2].Domain)

	// at the workspace root only its .gitignore
	assert.Len(t, ancestorFiles()(fakeRoot)(nil), 1)
}

func TestNestedIgnores(t *testing.T) {
	entries := []FileEntry{
		{Path: "a/b/.gitignore"},
		{Path: ".gitignore"},
		{Path: "a/.gitignore"},
		{Path: "x.go"},
		{Path: "c/.gitignore", IsDir: true},
	}

	// the .gitignore of the walked directory itself is not nested
	assert.Equal(t, []string{"a/.gitignore", "a/b/.gitignore"}, entryPaths(nestedIgnores()(entries)))
	assert.Empty(t, nestedIgnores()(nil))
}

func TestNestedIgnore(t *testing.T) {
	dir := filepath.Join(fakeRoot, "p")

	assert.Equal(t,
		IgnoreFile{Path: filepath.Join(dir, "c", "d", ".gitignore"), Domain: []string{"p", "c", "d"}},
		nestedIgnore()(dir)([]string{"p"})(FileEntry{Path: "c/d/.gitignore"}))

	// below the workspace root the domain is the entry's directory alone
	assert.Equal(t,
		IgnoreFile{Path: filepath.Join(fakeRoot, "c", ".gitignore"), Domain: []string{"c"}},
		nestedIgnore()(fakeRoot)(nil)(FileEntry{Path: "c/.gitignore"}))
}

func TestNestedFiles(t *testing.T) {
	dir := filepath.Join(fakeRoot, "p")
	entries := []FileEntry{{Path: "c/d/.gitignore"}, {Path: ".gitignore"}, {Path: "c/.gitignore"}, {Path: "x.go"}}

	assert.Equal(t, []IgnoreFile{
		{Path: filepath.Join(dir, "c", ".gitignore"), Domain: []string{"p", "c"}},
		{Path: filepath.Join(dir, "c", "d", ".gitignore"), Domain: []string{"p", "c", "d"}},
	}, nestedFiles()(dir)([]string{"p"})(entries))
	assert.Empty(t, nestedFiles()(dir)([]string{"p"})(nil))
}

func TestLoadIgnores(t *testing.T) {
	deps := makeFakeGrepDeps(nil, map[string]string{
		filepath.Join(fakeRoot, ".git", "info", "exclude"): "*.log\n",
		filepath.Join(fakeRoot, ".gitignore"):              "*.tmp\n",
		filepath.Join(fakeRoot, "sub", ".gitignore"):       "*.md\n",
	})
	args := GrepArgs{
		Name:  ".",
		Scope: GrepScope{Dir: fakeRoot, Entries: []FileEntry{{Path: "sub/.gitignore"}, {Path: "a.go"}}},
	}

	patterns, err := result.Unwrap(runEffect(t, deps, loadIgnores()(args)))
	require.NoError(t, err)
	require.Len(t, patterns, 3)

	keep := notIgnored(nil, patterns)
	assert.False(t, keep(FileEntry{Path: "x.log"}))
	assert.False(t, keep(FileEntry{Path: "x.tmp"}))
	assert.False(t, keep(FileEntry{Path: "sub/x.md"}))
	assert.True(t, keep(FileEntry{Path: "x.md"}))
	assert.True(t, keep(FileEntry{Path: "a.go"}))

	// without any ignore file there are no patterns
	none, err := result.Unwrap(runEffect(t, makeFakeGrepDeps(nil, nil), loadIgnores()(args)))
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestSearchScope(t *testing.T) {
	deps := makeFakeGrepDeps(fstest.MapFS{
		"a.go":     {Data: []byte("x")},
		"sub/b.go": {Data: []byte("x")},
		"sub/c.go": {Data: []byte("x")},
	}, nil)

	// a file is searched alone, in the scope of its parent directory
	scope, err := result.Unwrap(runEffect(t, deps, searchScope()(filepath.Join(fakeRoot, "a.go"))))
	require.NoError(t, err)
	assert.Equal(t, fakeRoot, scope.Dir)
	assert.Equal(t, []string{"a.go"}, entryPaths(scope.Entries))

	file, err := result.Unwrap(runEffect(t, deps, searchScope()(filepath.Join(fakeRoot, "sub", "b.go"))))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(fakeRoot, "sub"), file.Dir)
	assert.Equal(t, []string{"b.go"}, entryPaths(file.Entries))

	// a directory is walked recursively
	dir, err := result.Unwrap(runEffect(t, deps, searchScope()(filepath.Join(fakeRoot, "sub"))))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(fakeRoot, "sub"), dir.Dir)
	assert.ElementsMatch(t, []string{"b.go", "c.go"}, entryPaths(dir.Entries))

	// a path below a missing directory fails in the walk
	_, err = result.Unwrap(runEffect(t, deps, searchScope()(filepath.Join(fakeRoot, "missing", "x.go"))))
	assert.ErrorContains(t, err, "cannot list directory")
}

func TestKeepCandidate(t *testing.T) {
	isGo := func(p string) bool { return strings.HasSuffix(p, ".go") }
	patterns := []gitignore.Pattern{gitignore.ParsePattern("gen/", []string{"sub"})}
	keep := keepCandidate(isGo)([]string{"sub"})(patterns)

	assert.True(t, keep(FileEntry{Path: "a.go"}))
	assert.False(t, keep(FileEntry{Path: "a.md"}))
	assert.False(t, keep(FileEntry{Path: "x.go", IsDir: true}))
	// the patterns are matched by the workspace path below the prefix
	assert.False(t, keep(FileEntry{Path: "gen/b.go"}))
	assert.True(t, keepCandidate(isGo)(nil)(patterns)(FileEntry{Path: "gen/b.go"}))
}

func TestSelectCandidates(t *testing.T) {
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	isGo := func(p string) bool { return strings.HasSuffix(p, ".go") }
	patterns := []gitignore.Pattern{gitignore.ParsePattern("*_gen.go", nil)}
	entries := []FileEntry{
		{Path: "a.go", ModTime: old},
		{Path: "b.go", ModTime: old.Add(time.Hour)},
		{Path: "c_gen.go", ModTime: old.Add(2 * time.Hour)},
		{Path: "d.md", ModTime: old.Add(3 * time.Hour)},
	}

	// the newest first
	assert.Equal(t, []string{"b.go", "a.go"}, entryPaths(selectCandidates(isGo)(nil)(patterns)(entries)))
	assert.Empty(t, selectCandidates(isGo)(nil)(patterns)(nil))
}

func TestCandidates(t *testing.T) {
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	args := GrepArgs{
		Include: "*.go",
		Name:    ".",
		Ignores: []gitignore.Pattern{gitignore.ParsePattern("gen/", nil)},
		Scope: GrepScope{Entries: []FileEntry{
			{Path: "a.go", ModTime: old},
			{Path: "b.go", ModTime: old.Add(time.Hour)},
			{Path: "gen/c.go", ModTime: old.Add(2 * time.Hour)},
			{Path: "d.md", ModTime: old.Add(3 * time.Hour)},
			{Path: "e.go", IsDir: true},
		}},
	}

	assert.Equal(t,
		result.Of([]FileEntry{{Path: "b.go", ModTime: old.Add(time.Hour)}, {Path: "a.go", ModTime: old}}),
		candidates()(args))

	// an empty include keeps every file
	all, err := result.Unwrap(candidates()(MakeGrepArgsIncludeLens().Set("")(args)))
	require.NoError(t, err)
	assert.Equal(t, []string{"d.md", "b.go", "a.go"}, entryPaths(all))

	_, err = result.Unwrap(candidates()(MakeGrepArgsIncludeLens().Set("[a")(args)))
	assert.ErrorContains(t, err, "is malformed")
}

func TestIsText(t *testing.T) {
	assert.True(t, isText()([]byte("plain\ntext")))
	assert.True(t, isText()(nil))
	assert.False(t, isText()([]byte("bin\x00ary")))
}

func TestHasLines(t *testing.T) {
	assert.True(t, hasLines()([]byte("x")))
	assert.True(t, hasLines()([]byte("\n")))
	assert.False(t, hasLines()(nil))
	assert.False(t, hasLines()([]byte{}))
	assert.False(t, hasLines()([]byte("bin\x00ary")))
}

func TestGrepContent_Empty(t *testing.T) {
	notX := P.Not(S.Includes("x"))

	// empty content has no lines, so even invert_match finds nothing
	assert.Empty(t, grepContent(notX)("a.go")(nil))
	// a lone line break is one empty line
	assert.Equal(t, []GrepLine{{File: "a.go", Number: 1, Text: ""}}, grepContent(notX)("a.go")([]byte("\n")))
}

// TestSlice_Reused guards against the fp-go bug fixed in v2.5.1: A.Slice and
// A.SliceRight resolved their bounds in place, so a reused operator kept the
// bounds of an earlier input.
func TestSlice_Reused(t *testing.T) {
	take := A.Slice[int](0, 3)
	assert.Equal(t, []int{1}, take([]int{1}))
	assert.Equal(t, []int{1, 2, 3}, take([]int{1, 2, 3, 4}))

	lastTwo := A.Slice[int](-2, 10)
	assert.Equal(t, []int{2, 3}, lastTwo([]int{1, 2, 3}))
	assert.Equal(t, []int{5, 6}, lastTwo([]int{1, 2, 3, 4, 5, 6}))

	right := A.SliceRight[int](-2)
	assert.Equal(t, []int{2, 3}, right([]int{1, 2, 3}))
	assert.Equal(t, []int{5, 6}, right([]int{1, 2, 3, 4, 5, 6}))
}

func TestMatchingLine(t *testing.T) {
	keep := matchingLine(S.Includes("o"), "a.go")

	assert.Equal(t, option.Some(GrepLine{File: "a.go", Number: 3, Text: "foo"}), keep(2, "foo"))
	assert.Equal(t, option.None[GrepLine](), keep(0, "bar"))
}

func TestSearchFile(t *testing.T) {
	dir := filepath.Join(fakeRoot, "sub")
	deps := makeFakeGrepDeps(nil, map[string]string{
		filepath.Join(dir, "a.go"): "foo\nbar\nfoo bar\n",
	})
	search := searchFile(S.Includes("foo"), dir, "sub")
	name := filepath.Join("sub", "a.go")

	// the name is relative to the workspace
	assert.Equal(t,
		result.Of([]GrepLine{{File: name, Number: 1, Text: "foo"}, {File: name, Number: 3, Text: "foo bar"}}),
		runEffect(t, deps, search(FileEntry{Path: "a.go"})))

	// a file that cannot be read has no matches
	missing, err := result.Unwrap(runEffect(t, deps, search(FileEntry{Path: "missing.go"})))
	require.NoError(t, err)
	assert.Empty(t, missing)
}

func TestSearchLines(t *testing.T) {
	deps := makeFakeGrepDeps(nil, map[string]string{
		filepath.Join(fakeRoot, "a.go"): "hit\nmiss\n",
		filepath.Join(fakeRoot, "b.go"): "miss\nhit\n",
	})
	args := GrepArgs{
		Regexp:     regexp.MustCompile("hit"),
		Name:       ".",
		Scope:      GrepScope{Dir: fakeRoot},
		Candidates: []FileEntry{{Path: "b.go"}, {Path: "a.go"}, {Path: "missing.go"}},
	}

	// in the order of the candidates
	assert.Equal(t,
		result.Of([]GrepLine{{File: "b.go", Number: 2, Text: "hit"}, {File: "a.go", Number: 1, Text: "hit"}}),
		runEffect(t, deps, searchLines()(args)))

	// invert_match keeps the other lines
	assert.Equal(t,
		result.Of([]GrepLine{{File: "b.go", Number: 1, Text: "miss"}, {File: "a.go", Number: 2, Text: "miss"}}),
		runEffect(t, deps, searchLines()(MakeGrepArgsInvertMatchLens().Set(true)(args))))

	none, err := result.Unwrap(runEffect(t, deps, searchLines()(MakeGrepArgsCandidatesLens().Set(nil)(args))))
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestLineText(t *testing.T) {
	assert.Equal(t, "  Line 7: \tx := 1", lineText(GrepLine{File: "a.go", Number: 7, Text: "\tx := 1"}))
}

func TestFileBlock(t *testing.T) {
	lines := []GrepLine{
		{File: "a.go", Number: 1, Text: "x"},
		{File: "b.go", Number: 2, Text: "y"},
		{File: "a.go", Number: 5, Text: "z"},
	}

	assert.Equal(t, "a.go:\n  Line 1: x\n  Line 5: z", fileBlock(lines)("a.go"))
	assert.Equal(t, "b.go:\n  Line 2: y", fileBlock(lines)("b.go"))
}

func TestGroupByFile(t *testing.T) {
	lines := []GrepLine{
		{File: "b.go", Number: 1, Text: "x"},
		{File: "a.go", Number: 2, Text: "y"},
		{File: "b.go", Number: 4, Text: "z"},
	}

	// the files in the order of their first line
	assert.Equal(t, "b.go:\n  Line 1: x\n  Line 4: z\n\na.go:\n  Line 2: y", groupByFile()(lines))
	assert.Equal(t, "", groupByFile()(nil))
}

func TestCapped(t *testing.T) {
	render := capped("Got %d\n", " (more)", F.Flow2(A.Map(strconv.Itoa), S.Join(",")))

	assert.Equal(t, noMatches, render(nil))
	assert.Equal(t, "Got 3\n1,2,3", render([]int{1, 2, 3}))

	many := A.MakeBy(maxGrepResults+5, F.Identity[int])
	out := render(many)
	assert.True(t, strings.HasPrefix(out, "Got 105\n0,1,"))
	assert.True(t, strings.HasSuffix(out, ",99 (more)"))

	// exactly at the cap: everything, no note
	assert.False(t, strings.HasSuffix(render(many[:maxGrepResults]), " (more)"))
}

func TestCapped_Reused(t *testing.T) {
	render := renderMatches()
	line := func(i int) GrepLine { return GrepLine{File: "a.go", Number: i + 1, Text: "x"} }

	// a short call first must not cut the lines of a later, longer one
	assert.Equal(t, "Found 1 matches\na.go:\n  Line 1: x", render(A.MakeBy(1, line)))
	assert.Contains(t, render(A.MakeBy(maxGrepResults+1, line)), "  Line 100: x")
}

func TestRenderGrep(t *testing.T) {
	args := GrepArgs{Lines: []GrepLine{{File: "a.go", Number: 1, Text: "x"}, {File: "a.go", Number: 2, Text: "y"}}}

	assert.Equal(t, "Found 2 matches\na.go:\n  Line 1: x\n  Line 2: y", renderGrep()(args))
	assert.Equal(t, "Found 1 files with matches\na.go", renderGrep()(MakeGrepArgsFilesWithMatchesLens().Set(true)(args)))
	assert.Equal(t, noMatches, renderGrep()(GrepArgs{}))
}

func TestGrep_FakeDeps(t *testing.T) {
	deps := makeFakeGrepDeps(
		fstest.MapFS{
			".gitignore": {Data: []byte("*.log\n")},
			"a.go":       {Data: []byte("hit\n")},
			"a.log":      {Data: []byte("hit\n")},
		},
		map[string]string{
			filepath.Join(fakeRoot, ".gitignore"): "*.log\n",
			filepath.Join(fakeRoot, "a.go"):       "hit\n",
			filepath.Join(fakeRoot, "a.log"):      "hit\n",
		})

	assert.Equal(t, result.Of("Found 1 matches\na.go:\n  Line 1: hit"), runGrep(t, deps, `{"pattern":"hit"}`))
}

func TestGrep_EmptyFile(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, []string{"empty.go", "a.go"}, map[string]string{"empty.go": "", "a.go": "x\n"})

	assert.Equal(t, result.Of(noMatches),
		runGrep(t, makeOSGrepDeps(root), `{"pattern":"x","invert_match":true}`))
}

func TestMakeGrepTool_RepeatedCalls(t *testing.T) {
	root := t.TempDir()
	many := strings.Repeat("hit\n", maxGrepResults+1)
	writeTree(t, root, []string{"one.go", "many.go"}, map[string]string{"one.go": "hit\n", "many.go": many})

	call, ok := option.Unwrap(ToToolCaller()(MakeToolRegistry(MakeGrepTool()(makeOSGrepDeps(root))))(GrepName))
	require.True(t, ok)

	assert.Equal(t, "Found 1 matches\none.go:\n  Line 1: hit", runCall(t, call, `{"pattern":"hit","path":"one.go"}`))
	out := runCall(t, call, `{"pattern":"hit","path":"many.go"}`)
	assert.Contains(t, out, "  Line 100: hit")
	assert.True(t, strings.HasSuffix(out, matchesTruncated))
}
