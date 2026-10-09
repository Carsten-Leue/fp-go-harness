package tools

import (
	"io/fs"
	"os"
	"time"

	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	ER "github.com/IBM/fp-go/v2/errors"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/reader"
)

// gitDir is skipped while walking: it is large and never what the model looks for.
const gitDir = ".git"

// FileEntry is a file or directory found while walking a directory. Path is
// slash-separated and relative to the directory that was walked.
//
// fp-go:Lens
type FileEntry struct {
	Path    string
	IsDir   bool
	ModTime time.Time
}

// WalkDir lists the absolute directory it is given, recursively or only its
// direct children.
type WalkDir = func(recursive bool) ioresult.Kleisli[string, []FileEntry]

// WalkDeps gives the tools access to directory listings.
type WalkDeps interface {
	GetWalkDir() WalkDir
}

type walkDeps struct {
	walkDir WalkDir
}

func (d *walkDeps) GetWalkDir() WalkDir {
	return d.walkDir
}

// MakeWalkDeps builds [WalkDeps] that list directories with walkDir.
func MakeWalkDeps(walkDir WalkDir) WalkDeps {
	return &walkDeps{walkDir}
}

// MakeDefaultWalkDeps builds [WalkDeps] over the operating system's file system.
func MakeDefaultWalkDeps() WalkDeps {
	return MakeWalkDeps(WalkFS(os.DirFS))
}

func AsWalkDeps[R WalkDeps](r R) WalkDeps {
	return r
}

// DirectoryDeps is what list_files and glob need: the workspace their paths
// are resolved against and the directory listings.
type DirectoryDeps interface {
	WorkspaceDeps
	WalkDeps
}

type directoryDeps struct {
	WorkspaceDeps
	WalkDeps
}

func MakeDirectoryDeps(ws WorkspaceDeps, w WalkDeps) DirectoryDeps {
	return &directoryDeps{ws, w}
}

func AsDirectoryDeps[R DirectoryDeps](r R) DirectoryDeps {
	return r
}

// walkEntries is a leaf: [fs.WalkDir] reports the entries through a callback.
// It lists everything below the root of fsys, or only its direct children
// when recursive is false, and skips .git directories. An error at the root
// fails the walk; an entry below it that cannot be read is left out.
func walkEntries(recursive bool) func(fs.FS) ([]FileEntry, error) {
	return func(fsys fs.FS) ([]FileEntry, error) {
		var entries []FileEntry
		err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if path == "." {
				return err
			}
			if err != nil {
				return nil
			}
			if d.IsDir() && d.Name() == gitDir {
				return fs.SkipDir
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			entries = append(entries, FileEntry{Path: path, IsDir: d.IsDir(), ModTime: info.ModTime()})
			if d.IsDir() && !recursive {
				return fs.SkipDir
			}
			return nil
		})
		return entries, err
	}
}

// WalkFS builds a [WalkDir] that walks the file system dirFS opens for a
// directory, e.g. [os.DirFS]. The error of a failed walk names the directory,
// because the file system reports the root as ".".
func WalkFS(dirFS func(string) fs.FS) WalkDir {
	return func(recursive bool) ioresult.Kleisli[string, []FileEntry] {
		walk := F.Flow2(dirFS, ioresult.Eitherize1(walkEntries(recursive)))

		return func(dir string) ioresult.IOResult[[]FileEntry] {
			return F.Pipe1(
				walk(dir),
				ioresult.MapLeft[[]FileEntry](ER.OnError("cannot list directory %s", dir)),
			)
		}
	}
}

// walkDir lists the absolute directory dir through the [WalkDeps] of R.
func walkDir[R WalkDeps](recursive bool) effect.Kleisli[R, string, []FileEntry] {
	return func(dir string) Effect[R, []FileEntry] {
		return F.Pipe1(
			effect.Asks(F.Flow2(AsWalkDeps[R], WalkDeps.GetWalkDir)),
			effect.ChainThunkK[R](F.Flow3(
				reader.Read[ioresult.Kleisli[string, []FileEntry]](recursive),
				reader.Read[ioresult.IOResult[[]FileEntry]](dir),
				thunk.FromIOResult[[]FileEntry],
			)),
		)
	}
}
