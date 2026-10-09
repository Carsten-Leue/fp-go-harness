package tools

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runGlob(t *testing.T, deps DirectoryDeps, arguments string) result.Result[string] {
	t.Helper()

	return Glob()(arguments)(deps)(t.Context())()
}

func TestAnywhere(t *testing.T) {
	for pattern, want := range map[string]string{
		"*.go":      "**/*.go",
		"a.go":      "**/a.go",
		"":          "**/",
		"src/*.go":  "src/*.go",
		"**/*.go":   "**/*.go",
		"/abs/*.go": "/abs/*.go",
	} {
		assert.Equal(t, want, anywhere()(pattern), pattern)
	}
}

func TestNewestFirst(t *testing.T) {
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	entries := []FileEntry{
		{Path: "b", ModTime: old},
		{Path: "c", ModTime: old.Add(time.Hour)},
		{Path: "a", ModTime: old},
	}

	// the newest first, equal times by path
	assert.Equal(t, []string{"c", "a", "b"}, A.Map(MakeFileEntryPathLens().Get)(A.SortBy(newestFirst())(entries)))
}

func TestCapResults(t *testing.T) {
	names := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = fmt.Sprintf("f%03d", i)
		}
		return out
	}

	assert.Equal(t, noFilesFound, capResults()(nil))
	assert.Equal(t, "a\nb", capResults()([]string{"a", "b"}))
	// exactly at the cap: no note
	assert.Equal(t, strings.Join(names(maxGlobResults), "\n"), capResults()(names(maxGlobResults)))
	// above the cap: the first ones and the note
	assert.Equal(t, strings.Join(names(maxGlobResults), "\n")+globTruncated, capResults()(names(maxGlobResults+1)))

	// a reused operator does not keep the length of a shorter, earlier input
	reused := capResults()
	assert.Equal(t, "a\nb", reused([]string{"a", "b"}))
	assert.Equal(t, strings.Join(names(maxGlobResults), "\n")+globTruncated, reused(names(maxGlobResults+1)))
}

func TestGlobListing(t *testing.T) {
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	entries := []FileEntry{
		{Path: "a.go", ModTime: old},
		{Path: "sub", IsDir: true, ModTime: old.Add(3 * time.Hour)},
		{Path: "sub/b.go", ModTime: old.Add(time.Hour)},
		{Path: "c.md", ModTime: old.Add(2 * time.Hour)},
	}
	isGo := func(p string) bool { return strings.HasSuffix(p, ".go") }

	assert.Equal(t,
		filepath.Join("tools", "sub", "b.go")+"\n"+filepath.Join("tools", "a.go"),
		globListing()("tools")(isGo)(entries))

	// directories never match, even for a pattern that matches everything
	assert.Equal(t,
		filepath.Join("tools", "c.md")+"\n"+filepath.Join("tools", "sub", "b.go")+"\n"+filepath.Join("tools", "a.go"),
		globListing()("tools")(F.Constant1[string](true))(entries))

	assert.Equal(t, noFilesFound, globListing()(".")(F.Constant1[string](false))(entries))
}

func TestRenderGlob(t *testing.T) {
	args := GlobArgs{Pattern: "*.go", Path: "/ignored", Name: "src", Entries: []FileEntry{{Path: "a.go"}, {Path: "a.md"}}}

	assert.Equal(t, result.Of(filepath.Join("src", "a.go")), renderGlob()(args))

	_, err := result.Unwrap(renderGlob()(GlobArgs{Pattern: "[a", Name: "."}))
	assert.EqualError(t, err, "glob pattern '**/[a' is malformed")
}

func TestGlobDefinition(t *testing.T) {
	definition := globDefinition("/ws")

	assert.Equal(t, GlobName, definition.Name)
	assert.Contains(t, definition.Description.Value, "Caps at 100 results")
	assert.Equal(t, "object", definition.Parameters["type"])
	assert.Equal(t, []string{"pattern"}, definition.Parameters["required"])

	properties := definition.Parameters["properties"].(map[string]any)
	assert.Equal(t, "string", properties["pattern"].(map[string]any)["type"])
	assert.Equal(t, "string", properties["path"].(map[string]any)["type"])
	assert.Contains(t, properties["path"].(map[string]any)["description"], "The workspace is at /ws.")
}

func TestGlob_PathVariants(t *testing.T) {
	root := t.TempDir()
	deps := MakeDirectoryDeps(MakeWorkspaceDeps(root), makeFakeWalkDeps(root, fstest.MapFS{
		"a.go":       {},
		"tools/b.go": {},
		"x.go/c.txt": {},
	}))

	// an absolute path inside the workspace
	assert.Equal(t,
		result.Of(filepath.Join("tools", "b.go")),
		runGlob(t, deps, `{"pattern":"*.go","path":`+strconv.Quote(filepath.Join(root, "tools"))+`}`))

	// an empty path is the workspace root; the directory x.go does not match
	assert.Equal(t,
		result.Of("a.go\n"+filepath.Join("tools", "b.go")),
		runGlob(t, deps, `{"pattern":"*.go","path":""}`))
}

func TestGlobMatcher(t *testing.T) {
	for name, tc := range map[string]struct {
		pattern string
		match   []string
		miss    []string
	}{
		"base name anywhere": {"*.go", []string{"a.go", "x/y/a.go"}, []string{"a.md"}},
		"doublestar":         {"src/**/*.ts", []string{"src/a.ts", "src/x/y/a.ts"}, []string{"a.ts", "lib/a.ts"}},
		"alternatives":       {"*.{ts,tsx}", []string{"a.ts", "x/a.tsx"}, []string{"a.js"}},
		"anchored":           {"tools/*.go", []string{"tools/a.go"}, []string{"tools/x/a.go", "a.go"}},
	} {
		t.Run(name, func(t *testing.T) {
			match, err := result.Unwrap(globMatcher()(tc.pattern))
			require.NoError(t, err)
			for _, p := range tc.match {
				assert.True(t, match(p), p)
			}
			for _, p := range tc.miss {
				assert.False(t, match(p), p)
			}
		})
	}

	_, err := result.Unwrap(globMatcher()("[a"))
	assert.EqualError(t, err, "glob pattern '**/[a' is malformed")
}

func TestGlob(t *testing.T) {
	root := t.TempDir()
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	deps := MakeDirectoryDeps(MakeWorkspaceDeps(root), makeFakeWalkDeps(root, fstest.MapFS{
		"a.go":       {ModTime: old},
		"tools/b.go": {ModTime: old.Add(time.Hour)},
		"tools/c.md": {ModTime: old},
	}))

	// the newest first, relative to the workspace
	assert.Equal(t,
		result.Of(filepath.Join("tools", "b.go")+"\na.go"),
		runGlob(t, deps, `{"pattern":"**/*.go"}`))

	// a path narrows the search, the names stay relative to the workspace
	assert.Equal(t,
		result.Of(filepath.Join("tools", "c.md")),
		runGlob(t, deps, `{"pattern":"*.md","path":"tools"}`))

	assert.Equal(t, result.Of("No files found"), runGlob(t, deps, `{"pattern":"*.xyz"}`))
}

func TestGlob_Cap(t *testing.T) {
	root := t.TempDir()
	files := fstest.MapFS{}
	for i := range maxGlobResults + 5 {
		files[fmt.Sprintf("f%03d.txt", i)] = &fstest.MapFile{}
	}
	deps := MakeDirectoryDeps(MakeWorkspaceDeps(root), makeFakeWalkDeps(root, files))

	out, err := result.Unwrap(runGlob(t, deps, `{"pattern":"*.txt"}`))
	require.NoError(t, err)
	assert.Contains(t, out, "f099.txt")
	assert.NotContains(t, out, "f100.txt")
	assert.Contains(t, out, "Showing the 100 most recently modified files")
}

func TestGlob_Failures(t *testing.T) {
	root := t.TempDir()
	deps := MakeDirectoryDeps(MakeWorkspaceDeps(root), MakeDefaultWalkDeps())

	for name, tc := range map[string]struct {
		arguments string
		want      string
	}{
		"invalid json": {`{`, "unexpected end of JSON input"},
		"bad pattern":  {`{"pattern":"[a"}`, "is malformed"},
		"outside":      {`{"pattern":"*","path":".."}`, "is outside the workspace"},
		"missing dir":  {`{"pattern":"*","path":"missing"}`, "cannot list directory"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := result.Unwrap(runGlob(t, deps, tc.arguments))
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestMakeGlobTool(t *testing.T) {
	root := t.TempDir()
	deps := MakeDirectoryDeps(MakeWorkspaceDeps(root), makeFakeWalkDeps(root, fstest.MapFS{"a.go": {}}))

	registry := MakeToolRegistry(MakeGlobTool()(deps))

	call, ok := option.Unwrap(ToToolCaller()(registry)(GlobName))
	require.True(t, ok)
	assert.Equal(t, "a.go", runCall(t, call, `{"pattern":"*.go"}`))

	definition := toolDefinition(registry[GlobName])
	assert.Equal(t, GlobName, definition.Name)
	assert.Equal(t, []string{"pattern"}, definition.Parameters["required"])

	properties := definition.Parameters["properties"].(map[string]any)
	require.Len(t, properties, 2)
	assert.Contains(t, properties["path"].(map[string]any)["description"], root)
}
