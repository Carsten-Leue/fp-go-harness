package tools

import (
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/reader"
)

// ToolsDeps are the dependencies of every tool: those of the read-only tools,
// write access to the file system for write_file, search_and_replace and
// apply_diff, and the processes of execute_command.
type ToolsDeps interface {
	ReadToolsDeps
	FileWriteDeps
	ProcessDeps
}

type toolsDeps struct {
	ReadToolsDeps
	FileWriteDeps
	ProcessDeps
}

func MakeToolsDeps(r ReadToolsDeps, w FileWriteDeps, p ProcessDeps) ToolsDeps {
	return &toolsDeps{r, w, p}
}

func AsToolsDeps[R ToolsDeps](r R) ToolsDeps {
	return r
}

// MakeDefaultToolsDeps builds [ToolsDeps] over the operating system's file
// system and shell, for the workspace at root. Background processes write
// their logs to logDir, which should lie inside the workspace so that
// read_file can read them.
//
// Creating the deps allocates the process table, hence the IO.
func MakeDefaultToolsDeps(root, logDir string) IO[ToolsDeps] {
	return F.Pipe1(
		MakeDefaultProcessDeps(logDir),
		io.Map(F.Bind12of3(MakeToolsDeps)(MakeDefaultReadToolsDeps(root), MakeDefaultFileWriteDeps())),
	)
}

// MakeTools builds the registry of every tool: the read-only ones,
// write_file, search_and_replace, apply_diff and execute_command. Each tool reads its own dependencies from
// [ToolsDeps] through [reader.Local].
func MakeTools() Reader[ToolsDeps, ToolRegistry] {
	return F.Pipe2(
		readTools[ToolsDeps](),
		A.Concat(A.From(
			reader.Local[Tool](AsWriteFileDeps[ToolsDeps])(MakeWriteFileTool()),
			reader.Local[Tool](AsSearchAndReplaceDeps[ToolsDeps])(MakeSearchAndReplaceTool()),
			reader.Local[Tool](AsApplyDiffDeps[ToolsDeps])(MakeApplyDiffTool()),
			reader.Local[Tool](AsExecuteCommandDeps[ToolsDeps])(MakeExecuteCommandTool()),
		)),
		toRegistry[ToolsDeps](),
	)
}
