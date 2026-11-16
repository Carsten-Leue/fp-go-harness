package skill

import (
	"io/fs"
	"path/filepath"

	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/endomorphism"
	ER "github.com/IBM/fp-go/v2/errors"
	"github.com/IBM/fp-go/v2/file"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/ioresult"
	Mg "github.com/IBM/fp-go/v2/magma"
	M "github.com/IBM/fp-go/v2/monoid"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
)

// The skill directories of a root, the home directory or the workspace, see
// [DirsIn].
const (
	ClaudeSkillsDir = ".claude/skills"
	BobSkillsDir    = ".bob/skills"
)

// SkillFilePattern matches the skill files of a skill directory: one
// [SkillFile] in each directory directly below it.
const SkillFilePattern = "*/" + SkillFile

// DirsIn are the skill directories of a root, lowest precedence first:
// [ClaudeSkillsDir], then [BobSkillsDir], so that of two skills with the same
// name in one root the one for Bob wins.
func DirsIn() A.Kleisli[string, string] {
	return reader.TraverseArray(file.Join)(A.From(ClaudeSkillsDir, BobSkillsDir))
}

// inSkillFile is a leaf: it adds the location of a skill file to an error.
func inSkillFile(location string) Endomorphism[error] {
	return ER.OnError("skill file %q", location)
}

// located sets the location of a skill file in the skill read from it and in
// the error of reading it. The two changes are independent, so their order
// doesn't matter.
func located() Reader[string, Endomorphism[IOResult[Skill]]] {
	locationLens := MakeSkillLocationLens()
	changes := reader.ApplicativeMonoid[string](endomorphism.Monoid[IOResult[Skill]]())

	return changes.Concat(
		F.Flow2(locationLens.Set, ioresult.Map[Skill, Skill]),
		F.Flow2(inSkillFile, ioresult.MapLeft[Skill, error]),
	)
}

// locationIn is the path of a file of a skill directory, by its
// slash-separated name in the directory's file system.
func locationIn() Reader[string, Endomorphism[string]] {
	return F.Flip(F.Flow2(filepath.FromSlash, file.Join))
}

// parseSkillFile reads and parses one skill file of a file system.
func parseSkillFile() Reader[fs.FS, ioresult.Kleisli[string, Skill]] {
	return F.Flow2(
		F.Curry2(ioresult.Eitherize2(fs.ReadFile)),
		reader.Map[string](ioresult.ChainResultK(ParseSkill())),
	)
}

// readSkillFile reads and parses one skill file of a directory, by its name
// in the directory's file system, with its location in the skill and in the
// error. The name is read twice: for the location and for the file.
func readSkillFile() Reader[Directory, ioresult.Kleisli[string, Skill]] {
	return F.Pipe2(
		F.Flow3(pair.Head[string, fs.FS], locationIn(), reader.Map[string](located())),
		reader.Map[Directory](F.Flip(reader.Ap[IOResult[Skill], string, IOResult[Skill]])),
		reader.Ap[ioresult.Kleisli[string, Skill]](F.Flow2(pair.Tail[string, fs.FS], parseSkillFile())),
	)
}

// skillFiles are the names of the skill files of a file system, sorted. A
// directory that doesn't exist has none: [fs.Glob] ignores the error of
// reading it.
func skillFiles() ioresult.Kleisli[fs.FS, []string] {
	return F.Bind2nd(ioresult.Eitherize2(fs.Glob), SkillFilePattern)
}

// readEach reads files with the reader of one, concurrently, keeping the
// result of each: a broken file doesn't fail the others.
func readEach() func(ioresult.Kleisli[string, Skill]) ioresult.Operator[[]string, []Result[Skill]] {
	return F.Flow3(
		io.TraverseArray[string, Result[Skill]],
		reader.Map[[]string](ioresult.FromIO[[]Result[Skill]]),
		ioresult.Chain[[]string, []Result[Skill]],
	)
}

// allResults turns a failure to list a directory into its only result.
func allResults() func(Result[[]Result[Skill]]) []Result[Skill] {
	return result.GetOrElse(F.Flow2(result.Left[Skill], A.Of[Result[Skill]]))
}

// readSkillDir reads every skill file of a directory, see
// [SkillFilePattern], each into a result of its own. The directory is read
// twice: for the names of the files and for each file.
func readSkillDir() Reader[Directory, IO[[]Result[Skill]]] {
	return F.Pipe3(
		readSkillFile(),
		reader.Map[Directory](readEach()),
		reader.Ap[IOResult[[]Result[Skill]]](F.Flow2(pair.Tail[string, fs.FS], skillFiles())),
		reader.Map[Directory](io.Map(allResults())),
	)
}

// indexed are what skill directories hold: the errors of the broken skill
// files, in order, and the skills by name.
type indexed = Pair[[]error, Record[string, Skill]]

// indexByName indexes skills by name; of two with the same name, the later
// one wins.
func indexByName() func([]Skill) Record[string, Skill] {
	nameLens := MakeSkillNameLens()

	return R.FromArrayMap[Skill, string](Mg.Second[Skill]())(F.Flow2(
		pair.Of[Skill],
		pair.MapHead[Skill](nameLens.Get),
	))
}

// indexSkills splits the results of a directory into the errors of its
// broken files and its skills by name.
func indexSkills() func([]Result[Skill]) indexed {
	return F.Flow2(
		A.PartitionMap[Result[Skill], error, Skill](F.Identity[Result[Skill]]),
		pair.MapTail[[]error](indexByName()),
	)
}

// indexedMonoid merges what directories hold, lowest precedence first: the
// errors are concatenated, and a skill replaces the skill with the same name
// of every earlier directory. The precedence is the monoid of the union in
// which the last one wins, as for the modes (D8).
func indexedMonoid() M.Monoid[indexed] {
	return pair.Monoid(A.Monoid[error](), R.UnionLastMonoid[string, Skill]())
}

// directoryOf opens a directory, by its path, with the environment.
func directoryOf() Reader[func(string) fs.FS, func(string) Directory] {
	return F.Flow2(
		pair.MapTail[string, string, fs.FS],
		reader.Local[Directory](pair.Of[string]),
	)
}

// readDirs reads skill directories, by their paths, opened with the
// environment, concurrently, and merges them, lowest precedence first.
func readDirs() Reader[func(string) fs.FS, io.Kleisli[[]string, indexed]] {
	return F.Flow4(
		directoryOf(),
		reader.Map[string](F.Flow2(readSkillDir(), io.Map(indexSkills()))),
		io.TraverseArray[string, indexed],
		reader.Map[[]string](io.Map(M.ConcatAll(indexedMonoid()))),
	)
}

// skillsOf are the skills of directories, sorted by name.
func skillsOf() func(indexed) []Skill {
	return F.Flow2(
		pair.Tail[[]error, Record[string, Skill]],
		R.ValuesOrd[Skill](S.Ord),
	)
}

// reported reports the broken skill files of directories and lists their
// skills. The files are reported one after the other, so the report keeps
// their order.
func reported() Reader[SkillDirsDeps, func(IO[indexed]) IO[[]Skill]] {
	return F.Flow4(
		SkillDirsDeps.GetReportBroken,
		io.TraverseArraySeq[error, F.Void],
		reader.Local[IO[[]F.Void]](pair.Head[[]error, Record[string, Skill]]),
		F.Flow2(io.Tap[indexed, []F.Void], reader.Map[IO[indexed]](io.Map(skillsOf()))),
	)
}

// ReadSkillDirs reads the skills of skill directories, by their paths,
// lowest precedence first: every <dir>/<skill>/SKILL.md (see [ParseSkill]),
// merged by name, a skill of a later directory replacing one of an earlier
// directory. The skills are sorted by name. The directories and their files
// are read concurrently.
//
// It never fails: a directory that doesn't exist has no skills, and a broken
// skill file is left out and reported through
// [SkillDirsDeps.GetReportBroken], with its location in the error.
func ReadSkillDirs() Reader[SkillDirsDeps, io.Kleisli[[]string, []Skill]] {
	return F.Pipe2(
		F.Flow2(SkillDirsDeps.GetOpenDir, readDirs()),
		reader.Map[SkillDirsDeps](reader.Compose[IO[[]Skill], []string, IO[indexed]]),
		reader.Ap[io.Kleisli[[]string, []Skill]](reported()),
	)
}
