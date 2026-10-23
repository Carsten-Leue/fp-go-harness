package mode

import (
	"fmt"
	"io/fs"
	"strings"

	A "github.com/IBM/fp-go/v2/array"
	ER "github.com/IBM/fp-go/v2/errors"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	Mg "github.com/IBM/fp-go/v2/magma"
	M "github.com/IBM/fp-go/v2/monoid"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
)

// ModesDir is the directory of the custom modes, both in the home directory
// and in the workspace root, see [ReadModeDirs].
const ModesDir = ".bob/modes"

// duplicateModeError is the error of a mode whose ID another mode of the
// same directory has too.
func duplicateModeError() result.Kleisli[Mode, Mode] {
	idLens := MakeModeIDLens()

	return F.Flow3(
		idLens.Get,
		ER.OnSome[string]("mode %q is defined twice"),
		result.Left[Mode],
	)
}

// duplicateModes combines two modes with the same ID into the error that the
// ID is defined twice. A mode that is an error already stays that error.
func duplicateModes() Mg.Magma[Result[Mode]] {
	return Mg.MakeMagma(F.Ignore2of2[Result[Mode]](result.Chain(duplicateModeError())))
}

// IndexModes indexes modes by their ID. Two modes with the same ID are an
// error.
func IndexModes() result.Kleisli[[]Mode, Record[string, Mode]] {
	idLens := MakeModeIDLens()

	return F.Flow2(
		R.FromArrayMap[Mode, string](duplicateModes())(F.Flow2(
			pair.Of[Mode],
			pair.BiMap(idLens.Get, result.Of[Mode]),
		)),
		result.SequenceRecord[string, Mode],
	)
}

// indexByID indexes modes that have distinct IDs by their ID.
func indexByID() func([]Mode) Record[string, Mode] {
	idLens := MakeModeIDLens()

	return R.FromArrayMap[Mode, string](Mg.Second[Mode]())(F.Flow2(
		pair.Of[Mode],
		pair.MapHead[Mode](idLens.Get),
	))
}

// MergeModes merges tables of modes indexed by ID, lowest precedence first:
// a mode replaces the mode with the same ID of every earlier table. This
// precedence is the monoid of the union in which the last table wins. The
// merged modes are sorted by [Mode.Order], modes of the same order by ID.
func MergeModes() A.Operator[Record[string, Mode], Mode] {
	return F.Flow3(
		M.ConcatAll(R.UnionLastMonoid[string, Mode]()),
		R.ValuesOrd[Mode](S.Ord),
		sortModes(),
	)
}

// mergeOverBuiltins merges tables of custom modes, lowest precedence first,
// over the built-in modes.
func mergeOverBuiltins() A.Operator[Record[string, Mode], Mode] {
	return F.Flow2(
		A.Prepend(indexByID()(Modes())),
		MergeModes(),
	)
}

// inModeDir is a leaf: it adds the name of a mode directory to an error.
func inModeDir(dir string) Endomorphism[error] {
	return ER.OnError("mode directory %q", dir)
}

// readModeIndex reads the modes of a file system, indexed by their ID.
func readModeIndex() ioresult.Kleisli[fs.FS, Record[string, Mode]] {
	return F.Flow2(
		ReadModes(),
		ioresult.ChainResultK(IndexModes()),
	)
}

// readModeDir reads the modes of a directory, indexed by their ID, with the
// name of the directory in the error. The environment opens the directory
// as a file system. The name is read twice: for the file system and for the
// error.
func readModeDir() Reader[func(string) fs.FS, ioresult.Kleisli[string, Record[string, Mode]]] {
	return F.Flow3(
		reader.Map[string](readModeIndex()),
		reader.Ap[IOResult[Record[string, Mode]], string, IOResult[Record[string, Mode]]],
		reader.Read[ioresult.Kleisli[string, Record[string, Mode]]](F.Flow2(inModeDir, ioresult.MapLeft[Record[string, Mode], error])),
	)
}

// ReadModeDirs reads the custom modes of directories, lowest precedence
// first, and merges them over the built-in modes, see [MergeModes]. The
// environment opens a directory as a file system: [os.DirFS], or a fake in
// tests. The directories are read concurrently.
//
// A directory that doesn't exist has no modes: [fs.Glob] ignores the error
// of reading it. Two mode files with the same ID in one directory are an
// error, and so is a broken mode file (see [ParseMode]).
func ReadModeDirs() Reader[func(string) fs.FS, ioresult.Kleisli[[]string, []Mode]] {
	return F.Flow3(
		readModeDir(),
		ioresult.TraverseArray[string, Record[string, Mode]],
		reader.Map[[]string](ioresult.Map(mergeOverBuiltins())),
	)
}

// idsOf are the IDs of modes, in their order.
func idsOf() A.Operator[Mode, string] {
	idLens := MakeModeIDLens()

	return A.Map(idLens.Get)
}

// IDs are the IDs of the modes of the table, in its order.
func IDs() Reader[ModeTableDeps, []string] {
	return F.Flow2(
		ModeTableDeps.GetModes,
		idsOf(),
	)
}

// unknownModeError is a leaf: one formatter over the known IDs and the ID.
func unknownModeError(ids []string, id string) func() error {
	return func() error {
		return fmt.Errorf("unknown mode %q, use one of %s", id, strings.Join(ids, ", "))
	}
}

// lookupIn finds a mode of a table by its ID. The lookup and the error for
// an unknown ID both read the table and the ID.
func lookupIn() Reader[[]Mode, result.Kleisli[string, Mode]] {
	return F.Pipe2(
		F.Flow3(
			idsOf(),
			F.Curry2(unknownModeError),
			reader.Map[string](result.FromOption[Mode]),
		),
		reader.Map[[]Mode](F.Flip(reader.Ap[Result[Mode], string, Option[Mode]])),
		reader.Ap[result.Kleisli[string, Mode]](F.Flow2(indexByID(), F.Flip(R.Lookup[Mode, string]))),
	)
}

// Lookup finds a mode of the table by its ID.
func Lookup() Reader[ModeTableDeps, result.Kleisli[string, Mode]] {
	return F.Flow2(
		ModeTableDeps.GetModes,
		lookupIn(),
	)
}
