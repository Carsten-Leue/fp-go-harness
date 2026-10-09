package tools

import (
	"path/filepath"

	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	J "github.com/IBM/fp-go/v2/json"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	S "github.com/IBM/fp-go/v2/string"
	openai "github.com/openai/openai-go/v3"
)

// ListFilesName is the name under which the model calls the list_files tool.
const ListFilesName = "list_files"

// noFilesFound is the text of a listing without entries.
const noFilesFound = "No files found"

// ListFilesArgs are the arguments of a list_files call. Name and Entries are
// not part of the call; they are filled in while the call runs.
//
// fp-go:Lens
type ListFilesArgs struct {
	Path      string      `json:"path"`
	Recursive bool        `json:"recursive"`
	Name      string      `json:"-"`
	Entries   []FileEntry `json:"-"`
}

// entryName is a leaf: one formatter over the walked directory and the entry.
// It names the entry relative to the workspace, given the workspace-relative
// name dir of the walked directory; a directory ends in a separator.
func entryName(dir string) func(FileEntry) string {
	return func(e FileEntry) string {
		name := filepath.Join(dir, filepath.FromSlash(e.Path))
		if e.IsDir {
			return name + string(filepath.Separator)
		}
		return name
	}
}

// joinLines joins names line by line, or yields "No files found" for none.
func joinLines() func([]string) string {
	return P.Fold(S.Join("\n"), F.Constant1[[]string](noFilesFound))(A.IsEmpty[string])
}

// listingOf reads the name of the directory and renders its entries in the
// format of Bob: "Directory listing for <dir>:\n\n<path>\n...". The joined
// names and the header are two independent readers over the name, composed
// in the order they run.
func listingOf() Reader[string, func([]FileEntry) string] {
	return F.Pipe2(
		F.Flow3(
			entryName,
			A.Map[FileEntry, string],
			reader.Map[[]FileEntry](joinLines()),
		),
		reader.Map[string](reader.Compose[string, []FileEntry, string]),
		reader.Ap[func([]FileEntry) string](F.Flow2(S.Format[string]("Directory listing for %s:\n\n"), S.Prepend)),
	)
}

// renderListing renders the entries of a list_files call.
func renderListing() Reader[ListFilesArgs, string] {
	nameLens := MakeListFilesArgsNameLens()
	entriesLens := MakeListFilesArgsEntriesLens()

	return F.Pipe1(
		F.Flow2(nameLens.Get, listingOf()),
		reader.Ap[string](entriesLens.Get),
	)
}

// ListFiles runs a list_files call: it decodes the JSON arguments, resolves
// the directory against the workspace and lists its entries, recursively or
// only its direct children, relative to the workspace root and in lexical
// order. Directories end in a path separator, .git directories are skipped.
//
// Invalid arguments, a path outside the workspace and a directory that cannot
// be read fail the effect; [MakeToolCall] turns the failure into a tool
// message for the model.
func ListFiles() effect.Kleisli[DirectoryDeps, string, string] {
	pathLens := MakeListFilesArgsPathLens()
	recursiveLens := MakeListFilesArgsRecursiveLens()
	nameLens := MakeListFilesArgsNameLens()
	entriesLens := MakeListFilesArgsEntriesLens()

	walk := F.Pipe1(
		F.Flow2(recursiveLens.Get, walkDir[DirectoryDeps]),
		reader.Ap[Effect[DirectoryDeps, []FileEntry]](pathLens.Get),
	)

	return F.Flow5(
		F.Flow3(S.ToBytes, J.Unmarshal[ListFilesArgs], effect.FromResult[DirectoryDeps, ListFilesArgs]),
		effect.Bind(pathLens.Set, F.Flow2(pathLens.Get, resolveIn[DirectoryDeps]())),
		effect.Bind(nameLens.Set, F.Flow2(pathLens.Get, relativeIn[DirectoryDeps]())),
		effect.Bind(entriesLens.Set, walk),
		effect.Map[DirectoryDeps](renderListing()),
	)
}

// listFilesDefinition describes list_files to the model, following the
// recorded definition.
func listFilesDefinition(root string) openai.FunctionDefinitionParam {
	return openai.FunctionDefinitionParam{
		Name:        ListFilesName,
		Description: openai.String("Request to list files and directories within the specified directory. If recursive is true, it will list all files and directories recursively. If recursive is false or not provided, it will only list the top-level contents."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": `The path of the directory to list contents for (absolute, or relative to the workspace). The workspace is at ` + root + `. Use "." to list the workspace root.`,
				},
				"recursive": map[string]any{
					"type":        "boolean",
					"description": "Whether to list files recursively. Use true for recursive listing, false or omit for top-level only.",
				},
			},
			"required": []string{"path"},
		},
	}
}

// MakeListFilesTool builds the list_files [Tool] over the given dependencies.
func MakeListFilesTool() Reader[DirectoryDeps, Tool] {
	return F.Pipe2(
		F.Flow2(DirectoryDeps.GetWorkspaceRoot, listFilesDefinition),
		reader.Map[DirectoryDeps](F.Curry2(MakeTool)),
		reader.Ap[Tool](F.Flip(ListFiles())),
	)
}
