package tools

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errLocked = errors.New("locked")

// faultyFS fails to read the directory named locked and adds an entry whose
// info cannot be read to the root.
type faultyFS struct {
	fstest.MapFS
	locked string
}

func (f faultyFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == f.locked {
		return nil, errLocked
	}
	entries, err := f.MapFS.ReadDir(name)
	if name == "." {
		entries = append(entries, ghostEntry{})
	}
	return entries, err
}

// ghostEntry is a directory entry whose info cannot be read.
type ghostEntry struct{}

func (ghostEntry) Name() string               { return "ghost" }
func (ghostEntry) IsDir() bool                { return false }
func (ghostEntry) Type() fs.FileMode          { return 0 }
func (ghostEntry) Info() (fs.FileInfo, error) { return nil, errLocked }

// makeFakeWalkDeps serves the in-memory file system files as the workspace
// root; a directory below root is opened with [fs.Sub].
func makeFakeWalkDeps(root string, files fstest.MapFS) WalkDeps {
	return MakeWalkDeps(WalkFS(func(dir string) fs.FS {
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == "." {
			return files
		}
		sub, err := fs.Sub(files, filepath.ToSlash(rel))
		if err != nil {
			return files
		}
		return sub
	}))
}

func walkedPaths(t *testing.T, walk WalkDir, recursive bool) []string {
	t.Helper()

	entries, err := result.Unwrap(walk(recursive)("ignored")())
	require.NoError(t, err)

	return A.Map(MakeFileEntryPathLens().Get)(entries)
}

func TestWalkFS(t *testing.T) {
	files := fstest.MapFS{
		"a.txt":       {},
		"dir/b.txt":   {},
		".git/config": {},
	}
	walk := WalkFS(func(string) fs.FS { return files })

	// .git is skipped, directories are listed
	assert.Equal(t, []string{"a.txt", "dir", "dir/b.txt"}, walkedPaths(t, walk, true))
	assert.Equal(t, []string{"a.txt", "dir"}, walkedPaths(t, walk, false))
}

func TestWalkFS_MissingDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")

	_, err := result.Unwrap(WalkFS(os.DirFS)(true)(missing)())
	assert.ErrorContains(t, err, "cannot list directory "+missing)
}

func TestWalkEntries(t *testing.T) {
	modTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	files := fstest.MapFS{
		"a.txt":     {ModTime: modTime},
		"dir/b.txt": {ModTime: modTime},
	}

	entries, err := walkEntries(true)(files)
	require.NoError(t, err)
	assert.Equal(t, []FileEntry{
		{Path: "a.txt", ModTime: modTime},
		{Path: "dir", IsDir: true},
		{Path: "dir/b.txt", ModTime: modTime},
	}, entries)
}

func TestWalkEntries_Empty(t *testing.T) {
	entries, err := walkEntries(true)(fstest.MapFS{})
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestWalkEntries_SkipsNestedGit(t *testing.T) {
	entries, err := walkEntries(true)(fstest.MapFS{"dir/.git/config": {}, "dir/a": {}})
	require.NoError(t, err)
	assert.Equal(t, []string{"dir", "dir/a"}, A.Map(MakeFileEntryPathLens().Get)(entries))
}

func TestWalkEntries_LeavesOutUnreadableEntries(t *testing.T) {
	fsys := faultyFS{MapFS: fstest.MapFS{"a.txt": {}, "locked/x.txt": {}}, locked: "locked"}

	// the locked directory is listed, its children and the ghost entry are not
	entries, err := walkEntries(true)(fsys)
	require.NoError(t, err)
	assert.Equal(t, []string{"a.txt", "locked"}, A.Map(MakeFileEntryPathLens().Get)(entries))
}

func TestWalkEntries_RootFails(t *testing.T) {
	fsys := faultyFS{MapFS: fstest.MapFS{"a.txt": {}}, locked: "."}

	_, err := walkEntries(false)(fsys)
	assert.ErrorIs(t, err, errLocked)
}

func TestWalkFS_OpensTheDirectory(t *testing.T) {
	var opened []string
	walk := WalkFS(func(dir string) fs.FS {
		opened = append(opened, dir)
		return fstest.MapFS{"a": {}}
	})

	assert.Equal(t, []string{"a"}, walkedPaths(t, walk, false))
	assert.Equal(t, []string{"ignored"}, opened)
}

func TestWalkFS_WrapsTheError(t *testing.T) {
	walk := WalkFS(func(string) fs.FS { return faultyFS{MapFS: fstest.MapFS{}, locked: "."} })

	_, err := result.Unwrap(walk(true)("/some/dir")())
	assert.ErrorIs(t, err, errLocked)
	assert.ErrorContains(t, err, "cannot list directory /some/dir")
}

func TestMakeWalkDeps(t *testing.T) {
	deps := MakeWalkDeps(func(recursive bool) ioresult.Kleisli[string, []FileEntry] {
		return func(dir string) ioresult.IOResult[[]FileEntry] {
			return ioresult.Of([]FileEntry{{Path: dir, IsDir: recursive}})
		}
	})

	assert.Equal(t, result.Of([]FileEntry{{Path: "/x", IsDir: true}}), deps.GetWalkDir()(true)("/x")())
}

func TestMakeDefaultWalkDeps(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "dir"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "dir", "b.txt"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), nil, 0o600))
	walk := MakeDefaultWalkDeps().GetWalkDir()

	paths := func(recursive bool) []string {
		entries, err := result.Unwrap(walk(recursive)(root)())
		require.NoError(t, err)
		return A.Map(MakeFileEntryPathLens().Get)(entries)
	}

	assert.Equal(t, []string{"a.txt", "dir", "dir/b.txt"}, paths(true))
	assert.Equal(t, []string{"a.txt", "dir"}, paths(false))
}

func TestWalkDir(t *testing.T) {
	deps := MakeWalkDeps(func(recursive bool) ioresult.Kleisli[string, []FileEntry] {
		return func(dir string) ioresult.IOResult[[]FileEntry] {
			return ioresult.Of([]FileEntry{{Path: dir, IsDir: recursive}})
		}
	})

	// the flag and the directory reach the walker of the deps
	assert.Equal(t, result.Of([]FileEntry{{Path: "/x", IsDir: true}}), walkDir[WalkDeps](true)("/x")(deps)(t.Context())())
	assert.Equal(t, result.Of([]FileEntry{{Path: "/y"}}), walkDir[WalkDeps](false)("/y")(deps)(t.Context())())
}

func TestWalkDir_Fails(t *testing.T) {
	deps := MakeWalkDeps(func(bool) ioresult.Kleisli[string, []FileEntry] {
		return func(string) ioresult.IOResult[[]FileEntry] {
			return ioresult.Left[[]FileEntry](errLocked)
		}
	})

	assert.Equal(t, result.Left[[]FileEntry](errLocked), walkDir[WalkDeps](true)("/x")(deps)(t.Context())())
}

func TestAsDirectoryDeps(t *testing.T) {
	deps := MakeDirectoryDeps(MakeWorkspaceDeps(t.TempDir()), MakeDefaultWalkDeps())

	assert.Equal(t, deps, AsDirectoryDeps(deps))
	assert.Equal(t, WalkDeps(deps), AsWalkDeps(deps))
}
