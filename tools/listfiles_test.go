package tools

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"testing/fstest"

	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runListFiles(t *testing.T, deps DirectoryDeps, arguments string) result.Result[string] {
	t.Helper()

	return ListFiles()(arguments)(deps)(t.Context())()
}

func TestEntryName(t *testing.T) {
	sep := string(filepath.Separator)

	for name, tc := range map[string]struct {
		dir   string
		entry FileEntry
		want  string
	}{
		"file at the root":   {".", FileEntry{Path: "a.go"}, "a.go"},
		"directory at root":  {".", FileEntry{Path: "tools", IsDir: true}, "tools" + sep},
		"file below dir":     {"tools", FileEntry{Path: "sub/b.go"}, filepath.Join("tools", "sub", "b.go")},
		"directory below it": {"tools", FileEntry{Path: "sub", IsDir: true}, filepath.Join("tools", "sub") + sep},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, entryName(tc.dir)(tc.entry))
		})
	}
}

func TestJoinLines(t *testing.T) {
	assert.Equal(t, noFilesFound, joinLines()(nil))
	assert.Equal(t, noFilesFound, joinLines()([]string{}))
	assert.Equal(t, "a", joinLines()([]string{"a"}))
	assert.Equal(t, "a\nb\nc", joinLines()([]string{"a", "b", "c"}))
}

func TestListingOf(t *testing.T) {
	sep := string(filepath.Separator)

	assert.Equal(t,
		"Directory listing for tools:\n\n"+filepath.Join("tools", "a.go")+"\n"+filepath.Join("tools", "sub")+sep,
		listingOf()("tools")([]FileEntry{{Path: "a.go"}, {Path: "sub", IsDir: true}}))
	assert.Equal(t, "Directory listing for .:\n\nNo files found", listingOf()(".")(nil))
}

func TestRenderListing(t *testing.T) {
	assert.Equal(t,
		"Directory listing for tools:\n\n"+filepath.Join("tools", "a.go"),
		renderListing()(ListFilesArgs{Path: "/ignored", Name: "tools", Entries: []FileEntry{{Path: "a.go"}}}))
}

func TestListFilesDefinition(t *testing.T) {
	definition := listFilesDefinition("/ws")

	assert.Equal(t, ListFilesName, definition.Name)
	assert.Contains(t, definition.Description.Value, "list files and directories")
	assert.Equal(t, "object", definition.Parameters["type"])
	assert.Equal(t, []string{"path"}, definition.Parameters["required"])

	properties := definition.Parameters["properties"].(map[string]any)
	assert.Equal(t, "string", properties["path"].(map[string]any)["type"])
	assert.Contains(t, properties["path"].(map[string]any)["description"], "The workspace is at /ws.")
	assert.Equal(t, "boolean", properties["recursive"].(map[string]any)["type"])
}

func TestListFiles_AbsolutePath(t *testing.T) {
	root := t.TempDir()
	deps := MakeDirectoryDeps(MakeWorkspaceDeps(root), makeFakeWalkDeps(root, fstest.MapFS{"tools/a.go": {}}))

	assert.Equal(t,
		result.Of("Directory listing for tools:\n\n"+filepath.Join("tools", "a.go")),
		runListFiles(t, deps, `{"path":`+strconv.Quote(filepath.Join(root, "tools"))+`}`))
}

func TestListFiles_SkipsGit(t *testing.T) {
	root := t.TempDir()
	deps := MakeDirectoryDeps(MakeWorkspaceDeps(root), makeFakeWalkDeps(root, fstest.MapFS{
		".git/config": {},
		"a":           {},
	}))

	assert.Equal(t, result.Of("Directory listing for .:\n\na"), runListFiles(t, deps, `{"path":".","recursive":true}`))
}

func TestListFiles(t *testing.T) {
	root := t.TempDir()
	deps := MakeDirectoryDeps(MakeWorkspaceDeps(root), makeFakeWalkDeps(root, fstest.MapFS{
		"go.mod":         {},
		"tools/a.go":     {},
		"tools/sub/b.go": {},
	}))
	sep := string(filepath.Separator)

	assert.Equal(t,
		result.Of("Directory listing for .:\n\ngo.mod\ntools"+sep),
		runListFiles(t, deps, `{"path":"."}`))

	assert.Equal(t,
		result.Of("Directory listing for tools:\n\n"+
			filepath.Join("tools", "a.go")+"\n"+
			filepath.Join("tools", "sub")+sep+"\n"+
			filepath.Join("tools", "sub", "b.go")),
		runListFiles(t, deps, `{"path":"tools","recursive":true}`))
}

func TestListFiles_Empty(t *testing.T) {
	root := t.TempDir()
	deps := MakeDirectoryDeps(MakeWorkspaceDeps(root), makeFakeWalkDeps(root, fstest.MapFS{}))

	assert.Equal(t,
		result.Of("Directory listing for .:\n\nNo files found"),
		runListFiles(t, deps, `{"path":"."}`))
}

func TestListFiles_Failures(t *testing.T) {
	root := t.TempDir()
	deps := MakeDirectoryDeps(MakeWorkspaceDeps(root), MakeDefaultWalkDeps())

	for name, tc := range map[string]struct {
		arguments string
		want      string
	}{
		"invalid json": {`{`, "unexpected end of JSON input"},
		"empty path":   {`{"path":""}`, "path must not be empty"},
		"outside":      {`{"path":".."}`, "is outside the workspace"},
		"missing dir":  {`{"path":"missing"}`, "cannot list directory"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := result.Unwrap(runListFiles(t, deps, tc.arguments))
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

// TestListFiles_OperatingSystem lists a real directory through the default deps.
func TestListFiles_OperatingSystem(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "dir", ".git"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "dir", "x.txt"), nil, 0o600))
	deps := MakeDirectoryDeps(MakeWorkspaceDeps(root), MakeDefaultWalkDeps())

	assert.Equal(t,
		result.Of("Directory listing for dir:\n\n"+filepath.Join("dir", "x.txt")),
		runListFiles(t, deps, `{"path":"dir","recursive":true}`))
}

func TestMakeListFilesTool(t *testing.T) {
	root := t.TempDir()
	deps := MakeDirectoryDeps(MakeWorkspaceDeps(root), makeFakeWalkDeps(root, fstest.MapFS{"a": {}}))

	registry := MakeToolRegistry(MakeListFilesTool()(deps))

	call, ok := option.Unwrap(ToToolCaller()(registry)(ListFilesName))
	require.True(t, ok)
	assert.Equal(t, "Directory listing for .:\n\na", runCall(t, call, `{"path":"."}`))

	definition := toolDefinition(registry[ListFilesName])
	assert.Equal(t, ListFilesName, definition.Name)
	assert.Equal(t, []string{"path"}, definition.Parameters["required"])

	properties := definition.Parameters["properties"].(map[string]any)
	require.Len(t, properties, 2)
	assert.Contains(t, properties["path"].(map[string]any)["description"], root)
	assert.Equal(t, "boolean", properties["recursive"].(map[string]any)["type"])
}
