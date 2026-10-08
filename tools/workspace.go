package tools

import (
	"errors"
	"path/filepath"

	ER "github.com/IBM/fp-go/v2/errors"
	"github.com/IBM/fp-go/v2/file"
	F "github.com/IBM/fp-go/v2/function"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
)

// WorkspaceDeps gives the tools the directory they work in. Every path a tool
// receives is resolved against it, and paths outside of it are rejected.
type WorkspaceDeps interface {
	GetWorkspaceRoot() string
}

type workspaceDeps struct {
	root string
}

func (d *workspaceDeps) GetWorkspaceRoot() string {
	return d.root
}

// MakeWorkspaceDeps builds [WorkspaceDeps] for the absolute directory root.
func MakeWorkspaceDeps(root string) WorkspaceDeps {
	return &workspaceDeps{filepath.Clean(root)}
}

func AsWorkspaceDeps[R WorkspaceDeps](r R) WorkspaceDeps {
	return r
}

var errEmptyPath = errors.New("path must not be empty")

// absolutePath reads the root, resolves a relative path against it and cleans
// an absolute one.
func absolutePath() Reader[string, Endomorphism[string]] {
	return F.Flow3(
		F.Flip(file.Join),
		F.Bind2nd(P.Fold[string, string], filepath.Clean),
		reader.Read[Endomorphism[string]](filepath.IsAbs),
	)
}

// outsideError is a leaf: [ER.OnSome] is variadic and needs root as argument.
func outsideError(root string) func(string) error {
	return ER.OnSome[string]("path '%s' is outside the workspace '%s'", root)
}

// isWithin reads the root and tests lexically whether a path is the root
// itself or lies below it.
func isWithin() Reader[string, P.Predicate[string]] {
	return F.Flow2(
		F.Curry2(result.Eitherize2(filepath.Rel)),
		reader.Map[string](result.Exists(F.Pipe1(
			F.Bind1st(S.Eq, "."),
			P.Or(filepath.IsLocal),
		))),
	)
}

// checkWithin reads the root and fails for a path outside of it.
func checkWithin() Reader[string, result.Kleisli[string, string]] {
	return F.Pipe2(
		isWithin(),
		reader.Map[string](F.Curry2(result.FromPredicate[string])),
		reader.Ap[result.Kleisli[string, string]](outsideError),
	)
}

// ResolvePath reads the workspace root and resolves a path given to a tool
// against it.
//
// A relative path is taken relative to root, an absolute one is kept. The
// result is clean and absolute. An empty path and a path that resolves to a
// location outside of root fail; the error text is meant for the model.
// The check is lexical, it does not follow symbolic links.
func ResolvePath() Reader[string, ReaderResult[string, string]] {
	nonEmpty := result.FromPredicate(S.IsNonEmpty, F.Constant1[string](errEmptyPath))

	return F.Pipe3(
		absolutePath(),
		reader.Map[string](reader.Compose[Result[string], string, string]),
		reader.Ap[ReaderResult[string, string]](checkWithin()),
		reader.Map[string](F.Flow2(
			result.Chain[string, string],
			reader.Compose[Result[string]](nonEmpty),
		)),
	)
}

// ResolveWorkspacePath is [ResolvePath] for the root of the [WorkspaceDeps].
func ResolveWorkspacePath() Reader[WorkspaceDeps, ReaderResult[string, string]] {
	return F.Flow2(
		WorkspaceDeps.GetWorkspaceRoot,
		ResolvePath(),
	)
}
