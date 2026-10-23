package session

import (
	"os"
	"path/filepath"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	"github.com/IBM/fp-go/v2/effect"
	ER "github.com/IBM/fp-go/v2/errors"
	"github.com/IBM/fp-go/v2/file"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	fileio "github.com/IBM/fp-go/v2/ioresult/file"
	J "github.com/IBM/fp-go/v2/json"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
)

// TasksDir is the directory of the task files, relative to the home
// directory.
var TasksDir = filepath.Join(".fp-go-harness", "tasks")

// taskFileExt is the extension of a task file; its name is the task's ID.
const taskFileExt = ".json"

// currentDir is the path element of the current directory, not a task ID.
const currentDir = "."

// TaskID names a stored [Session]. It is the name of its file without the
// extension, so it must be a plain file name: no path separators, not "." or
// "..".
type TaskID = string

// Task is a session together with the ID it is stored under.
type Task = Pair[TaskID, Session]

// TaskStoreDeps is what [Save] and [Load] need: the directory of the task
// files and read and write access to the file system.
type TaskStoreDeps interface {
	tools.FileDeps
	tools.FileWriteDeps
	// GetTasksDir returns the absolute directory of the task files.
	GetTasksDir() string
}

type taskStoreDeps struct {
	tools.FileDeps
	tools.FileWriteDeps
	tasksDir string
}

func (d *taskStoreDeps) GetTasksDir() string {
	return d.tasksDir
}

// MakeTaskStoreDeps is wiring: it combines the directory of the task files
// and access to the file system into [TaskStoreDeps].
func MakeTaskStoreDeps(tasksDir string, r tools.FileDeps, w tools.FileWriteDeps) TaskStoreDeps {
	return &taskStoreDeps{r, w, tasksDir}
}

// MakeDefaultTaskStoreDeps is wiring: it builds [TaskStoreDeps] that keep the
// task files in tasksDir on the operating system's file system.
func MakeDefaultTaskStoreDeps(tasksDir string) TaskStoreDeps {
	return MakeTaskStoreDeps(tasksDir, tools.MakeFileDeps(fileio.ReadFile), tools.MakeDefaultFileWriteDeps())
}

// AsTaskStoreDeps is wiring: it narrows any wider dependencies to
// [TaskStoreDeps], for use with [reader.Local] and [effect.Local].
func AsTaskStoreDeps[R TaskStoreDeps](r R) TaskStoreDeps {
	return r
}

// DefaultTasksDir is wiring: [TasksDir] in the home directory of the user.
func DefaultTasksDir() IOResult[string] {
	return F.Pipe1(
		ioresult.Eitherize0(os.UserHomeDir)(),
		ioresult.Map(file.Join(TasksDir)),
	)
}

// isTaskID is a leaf: a task ID is a single local path element other than
// ".", so its file stays inside the tasks directory. [filepath.IsLocal]
// rejects "", ".." and absolute paths, but accepts ".".
func isTaskID(id TaskID) bool {
	return filepath.IsLocal(id) && filepath.Base(id) == id && id != currentDir
}

// validateTaskID fails for an ID that is not a plain file name.
func validateTaskID() result.Kleisli[TaskID, TaskID] {
	return result.FromPredicate(isTaskID, ER.OnSome[TaskID]("invalid task id %q: it must be a file name without a directory"))
}

// taskFile is a leaf: the path of the file of a task in a directory.
func taskFile(dir string, id TaskID) string {
	return filepath.Join(dir, id+taskFileExt)
}

// taskPath resolves a task ID to the path of its file in a directory. An
// invalid ID fails.
func taskPath() Reader[string, result.Kleisli[TaskID, string]] {
	return F.Flow3(
		F.Curry2(taskFile),
		result.Map[TaskID, string],
		reader.Local[Result[string]](validateTaskID()),
	)
}

// pathOf resolves a task ID to the path of its file in the tasks directory of
// the deps.
func pathOf() effect.Kleisli[TaskStoreDeps, TaskID, string] {
	return F.Flow2(
		F.Flip(F.Flow2(TaskStoreDeps.GetTasksDir, taskPath())),
		effect.FromReaderResult[TaskStoreDeps, string],
	)
}

// encodeSession encodes a session as the JSON of its [SessionDTO].
func encodeSession() result.Kleisli[Session, []byte] {
	return F.Flow2(SessionIso().Get, J.MarshalIndent[SessionDTO])
}

// decodeSession decodes a session from the JSON of its [SessionDTO].
func decodeSession() result.Kleisli[[]byte, Session] {
	return F.Flow2(J.Unmarshal[SessionDTO], result.Map(SessionIso().ReverseGet))
}

// taskFileOf is the path and the content of the file of a task: the path is
// read from its ID and the deps, the content from its session, independently.
func taskFileOf() effect.Kleisli[TaskStoreDeps, Task, Pair[string, []byte]] {
	path := F.Flow3(
		pair.Head[TaskID, Session],
		pathOf(),
		effect.Map[TaskStoreDeps](F.Curry2(pair.MakePair[string, []byte])),
	)
	content := F.Flow3(
		pair.Tail[TaskID, Session],
		encodeSession(),
		effect.FromResult[TaskStoreDeps, []byte],
	)

	return F.Pipe2(
		content,
		reader.Map[Task](effect.Ap[Pair[string, []byte], TaskStoreDeps, []byte]),
		reader.Ap[Effect[TaskStoreDeps, Pair[string, []byte]]](path),
	)
}

// The file writer of [tools.FileWriteDeps], and the same writer over a pair
// of path and content.
type (
	fileWriter     = func(string) ioresult.Kleisli[[]byte, []byte]
	pairFileWriter = func(string, []byte) IOResult[[]byte]
)

// writePair turns a file writer into a writer of a pair of path and content.
func writePair() Reader[fileWriter, ioresult.Kleisli[Pair[string, []byte], []byte]] {
	return F.Flow2(F.Uncurry2[fileWriter], pair.Paired[pairFileWriter])
}

// writeTaskFile writes the content of a pair to its path through the deps.
func writeTaskFile() effect.Kleisli[TaskStoreDeps, Pair[string, []byte], []byte] {
	return F.Flow2(
		F.Flip(F.Flow2(TaskStoreDeps.GetWriteFile, writePair())),
		effect.FromReaderIOResult[TaskStoreDeps, []byte],
	)
}

// makeTasksDir creates the tasks directory of the deps together with its
// missing parents.
func makeTasksDir() Effect[TaskStoreDeps, string] {
	return F.Pipe2(
		TaskStoreDeps.GetMakeDirs,
		reader.Ap[IOResult[string]](TaskStoreDeps.GetTasksDir),
		effect.FromReaderIOResult[TaskStoreDeps, string],
	)
}

// Save stores the session of a task as JSON in the file named by its ID in
// the tasks directory, which it creates when it is missing. A stored task of
// the same ID is replaced.
//
// An invalid ID and an error of the encoding fail before anything is
// written.
func Save() effect.Kleisli[TaskStoreDeps, Task, F.Void] {
	return F.Flow4(
		taskFileOf(),
		effect.ChainFirst(F.Constant1[Pair[string, []byte]](makeTasksDir())),
		effect.Chain(writeTaskFile()),
		effect.Map[TaskStoreDeps](F.Constant1[[]byte](F.VOID)),
	)
}

// readTaskFile reads the file at a path through the deps.
func readTaskFile() effect.Kleisli[TaskStoreDeps, string, []byte] {
	return F.Flow2(
		F.Flip(TaskStoreDeps.GetReadFile),
		effect.FromReaderIOResult[TaskStoreDeps, []byte],
	)
}

// Load reads the session stored by [Save] under a task ID. The session is
// restored with its whole history, request, usage and iteration count, so
// [Continue] and [Run] carry on where it was saved.
//
// An unknown ID fails with an error that matches fs.ErrNotExist; an
// invalid ID and a file that doesn't decode fail too.
func Load() effect.Kleisli[TaskStoreDeps, TaskID, Session] {
	return F.Flow3(
		pathOf(),
		effect.Chain(readTaskFile()),
		effect.ChainResultK[TaskStoreDeps](decodeSession()),
	)
}
