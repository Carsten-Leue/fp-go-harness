package tools

import (
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/reader"
)

// ToolsDeps are the dependencies of every tool: those of the read-only tools,
// write access to the file system for write_file, search_and_replace,
// apply_diff and insert_content, and the processes of execute_command.
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

// MakeToolsDeps is wiring: it combines the read tools, write access and the
// processes into [ToolsDeps].
func MakeToolsDeps(r ReadToolsDeps, w FileWriteDeps, p ProcessDeps) ToolsDeps {
	return &toolsDeps{r, w, p}
}

// AsToolsDeps is wiring: it narrows any wider dependencies to [ToolsDeps], for
// use with [reader.Local] and [effect.Local].
func AsToolsDeps[R ToolsDeps](r R) ToolsDeps {
	return r
}

// MakeDefaultToolsDeps is wiring: it builds [ToolsDeps] over the operating system's file
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

// ToolNames are the names of the tools of [MakeTools]: the [ReadToolNames],
// followed by those that change the workspace.
func ToolNames() []string {
	return F.Pipe1(
		ReadToolNames(),
		A.Concat(A.From(
			WriteFileName,
			SearchAndReplaceName,
			ApplyDiffName,
			InsertContentName,
			ExecuteCommandName,
		)),
	)
}

// MakeTools builds the registry of every tool: the read-only ones,
// write_file, search_and_replace, apply_diff, insert_content and execute_command. Each tool reads its own dependencies from
// [ToolsDeps] through [reader.Local].
func MakeTools() Reader[ToolsDeps, ToolRegistry] {
	return F.Pipe2(
		readTools[ToolsDeps](),
		A.Concat(A.From(
			reader.Local[Tool](AsWriteFileDeps[ToolsDeps])(MakeWriteFileTool()),
			reader.Local[Tool](AsSearchAndReplaceDeps[ToolsDeps])(MakeSearchAndReplaceTool()),
			reader.Local[Tool](AsApplyDiffDeps[ToolsDeps])(MakeApplyDiffTool()),
			reader.Local[Tool](AsInsertContentDeps[ToolsDeps])(MakeInsertContentTool()),
			reader.Local[Tool](AsExecuteCommandDeps[ToolsDeps])(MakeExecuteCommandTool()),
		)),
		toRegistry[ToolsDeps](),
	)
}
