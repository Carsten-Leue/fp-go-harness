package tools

import (
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/reader"
)

// ReadToolsDeps are the dependencies of the read-only tools. They are the
// dependencies of grep, which include those of read_file, list_files and
// glob.
type ReadToolsDeps = GrepDeps

// MakeDefaultReadToolsDeps builds [ReadToolsDeps] over the operating system's
// file system, for the workspace at root.
func MakeDefaultReadToolsDeps(root string) ReadToolsDeps {
	return MakeGrepDeps(MakeWorkspaceDeps(root), MakeDefaultWalkDeps(), MakeDefaultFileDeps())
}

// MakeReadTools builds the registry of the tools that read the workspace
// without changing it: read_file, list_files, glob and grep. Each tool reads
// its own dependencies from [ReadToolsDeps] through [reader.Local].
func MakeReadTools() Reader[ReadToolsDeps, ToolRegistry] {
	return F.Pipe2(
		A.From(
			reader.Local[Tool](AsReadFileDeps[ReadToolsDeps])(MakeReadFileTool()),
			reader.Local[Tool](AsDirectoryDeps[ReadToolsDeps])(MakeListFilesTool()),
			reader.Local[Tool](AsDirectoryDeps[ReadToolsDeps])(MakeGlobTool()),
			MakeGrepTool(),
		),
		reader.SequenceArray[ReadToolsDeps, Tool],
		reader.Map[ReadToolsDeps](ToToolRegistry()),
	)
}
