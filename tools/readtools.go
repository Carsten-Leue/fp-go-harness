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

// MakeDefaultReadToolsDeps is wiring: it builds [ReadToolsDeps] over the operating system's
// file system, for the workspace at root.
func MakeDefaultReadToolsDeps(root string) ReadToolsDeps {
	return MakeGrepDeps(MakeWorkspaceDeps(root), MakeDefaultWalkDeps(), MakeDefaultFileDeps())
}

// ReadToolNames are the names of the tools of [MakeReadTools], e.g. for the
// tools a read-only mode allows.
func ReadToolNames() []string {
	return A.From(ReadFileName, ListFilesName, GlobName, GrepName)
}

// readTools are the tools that read the workspace without changing it:
// read_file, list_files, glob and grep. Each tool reads its own dependencies
// from R through [reader.Local].
func readTools[R ReadToolsDeps]() []Reader[R, Tool] {
	return A.From(
		reader.Local[Tool](AsReadFileDeps[R])(MakeReadFileTool()),
		reader.Local[Tool](AsDirectoryDeps[R])(MakeListFilesTool()),
		reader.Local[Tool](AsDirectoryDeps[R])(MakeGlobTool()),
		reader.Local[Tool](AsGrepDeps[R])(MakeGrepTool()),
	)
}

// toRegistry builds the registry of tools that each read the same
// dependencies.
func toRegistry[R any]() func([]Reader[R, Tool]) Reader[R, ToolRegistry] {
	return F.Flow2(
		reader.SequenceArray[R, Tool],
		reader.Map[R](ToToolRegistry()),
	)
}

// MakeReadTools builds the registry of the read-only tools: read_file,
// list_files, glob and grep.
func MakeReadTools() Reader[ReadToolsDeps, ToolRegistry] {
	return toRegistry[ReadToolsDeps]()(readTools[ReadToolsDeps]())
}
