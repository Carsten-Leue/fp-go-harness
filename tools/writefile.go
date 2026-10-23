package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/ioresult/file"
	J "github.com/IBM/fp-go/v2/json"
	N "github.com/IBM/fp-go/v2/number"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	openai "github.com/openai/openai-go/v3"
)

// WriteFileName is the name under which the model calls the write_file tool.
const WriteFileName = "write_file"

const (
	// dirPerm is the permission of the parent directories write_file creates.
	dirPerm os.FileMode = 0o755
	// filePerm is the permission of the files write_file creates.
	filePerm os.FileMode = 0o644
)

// FileWriteDeps gives the tools write access to the file system.
type FileWriteDeps interface {
	// GetMakeDirs creates a directory together with its missing parents.
	GetMakeDirs() ioresult.Kleisli[string, string]
	// GetWriteFile replaces the file at a path with the given bytes.
	GetWriteFile() func(string) ioresult.Kleisli[[]byte, []byte]
}

type fileWriteDeps struct {
	makeDirs  ioresult.Kleisli[string, string]
	writeFile func(string) ioresult.Kleisli[[]byte, []byte]
}

func (d *fileWriteDeps) GetMakeDirs() ioresult.Kleisli[string, string] {
	return d.makeDirs
}

func (d *fileWriteDeps) GetWriteFile() func(string) ioresult.Kleisli[[]byte, []byte] {
	return d.writeFile
}

// MakeFileWriteDeps is wiring: it builds [FileWriteDeps] that create directories with
// makeDirs and write files with writeFile.
func MakeFileWriteDeps(makeDirs ioresult.Kleisli[string, string], writeFile func(string) ioresult.Kleisli[[]byte, []byte]) FileWriteDeps {
	return &fileWriteDeps{makeDirs, writeFile}
}

// MakeDefaultFileWriteDeps builds [FileWriteDeps] over the operating system's
// file system.
func MakeDefaultFileWriteDeps() FileWriteDeps {
	return MakeFileWriteDeps(
		F.Bind2nd(file.MkdirAll, dirPerm),
		F.Bind2nd(file.WriteFile, filePerm),
	)
}

// AsFileWriteDeps is wiring: it narrows any wider dependencies to
// [FileWriteDeps], for use with [reader.Local] and [effect.Local].
func AsFileWriteDeps[R FileWriteDeps](r R) FileWriteDeps {
	return r
}

// WriteFileDeps is what the write_file tool needs: the workspace its paths
// are resolved against and the file system it writes to.
type WriteFileDeps interface {
	WorkspaceDeps
	FileWriteDeps
}

type writeFileDeps struct {
	WorkspaceDeps
	FileWriteDeps
}

// MakeWriteFileDeps is wiring: it combines a workspace and write access to
// the file system into [WriteFileDeps].
func MakeWriteFileDeps(w WorkspaceDeps, f FileWriteDeps) WriteFileDeps {
	return &writeFileDeps{w, f}
}

// AsWriteFileDeps is wiring: it narrows any wider dependencies to
// [WriteFileDeps], for use with [reader.Local] and [effect.Local].
func AsWriteFileDeps[R WriteFileDeps](r R) WriteFileDeps {
	return r
}

// WriteFileArgs are the arguments of a write_file call. Name is not part of
// the call; it is filled in while the call runs.
//
// fp-go:Lens
type WriteFileArgs struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	LineCount int    `json:"line_count"`
	Name      string `json:"-"`
}

// countLines counts the lines of a text. The last line counts with or without
// a trailing line break; an empty text has no lines.
func countLines() func(string) int {
	return F.Flow3(
		F.Bind2nd(strings.SplitAfter, "\n"),
		A.Filter(S.IsNonEmpty),
		A.Size[string],
	)
}

// contentLines counts the lines of the content of a call.
func contentLines() Reader[WriteFileArgs, int] {
	contentLens := MakeWriteFileArgsContentLens()

	return F.Flow2(contentLens.Get, countLines())
}

// isComplete tests whether the content has at least the announced number of
// lines. More lines than announced pass: models often miscount the last line.
func isComplete() P.Predicate[WriteFileArgs] {
	lineCountLens := MakeWriteFileArgsLineCountLens()

	return F.Pipe1(
		F.Flow3(lineCountLens.Get, N.LessThan[int], P.Not[int]),
		reader.Ap[bool](contentLines()),
	)
}

// truncatedError is a leaf: one formatter over several fields.
func truncatedError(a WriteFileArgs) error {
	return fmt.Errorf(
		"the content has a line count of %d, but line_count is %d. It looks truncated: send the complete content of the file, not only a part of it",
		contentLines()(a),
		a.LineCount,
	)
}

// checkComplete fails for content that has fewer lines than announced.
func checkComplete() result.Kleisli[WriteFileArgs, WriteFileArgs] {
	return result.FromPredicate(isComplete(), truncatedError)
}

// makeParentDirs creates the missing parent directories of an absolute path
// through the [FileWriteDeps] of the call.
func makeParentDirs() effect.Kleisli[WriteFileDeps, string, string] {
	return F.Flow2(
		F.Flip(F.Flow2(WriteFileDeps.GetMakeDirs, reader.Local[ioresult.IOResult[string]](filepath.Dir))),
		effect.FromReaderIOResult[WriteFileDeps, string],
	)
}

// writeWith applies a file writer to the path and the content of a call.
func writeWith() Reader[func(string) ioresult.Kleisli[[]byte, []byte], Reader[WriteFileArgs, ioresult.IOResult[[]byte]]] {
	pathLens := MakeWriteFileArgsPathLens()
	contentLens := MakeWriteFileArgsContentLens()

	return F.Flow2(
		reader.Local[ioresult.Kleisli[[]byte, []byte]](pathLens.Get),
		reader.Ap[ioresult.IOResult[[]byte]](F.Flow2(contentLens.Get, S.ToBytes)),
	)
}

// writeContent writes the content of a call to its resolved path through the
// [FileWriteDeps] of the call.
func writeContent() effect.Kleisli[WriteFileDeps, WriteFileArgs, []byte] {
	return F.Flow2(
		F.Flip(F.Flow2(WriteFileDeps.GetWriteFile, writeWith())),
		effect.FromReaderIOResult[WriteFileDeps, []byte],
	)
}

// writtenText is a leaf: one formatter over several fields.
func writtenText(a WriteFileArgs) string {
	return fmt.Sprintf("Wrote %s (line count: %d).", a.Name, contentLines()(a))
}

// WriteFile runs a write_file call: it decodes the JSON arguments, checks
// that the content is not shorter than line_count announces, resolves the
// path against the workspace, creates the missing parent directories and
// replaces the file with the content.
//
// Invalid arguments, truncated content, a path outside the workspace and a
// write error fail the effect; [MakeToolCall] turns the failure into a tool
// message for the model.
func WriteFile() effect.Kleisli[WriteFileDeps, string, string] {
	pathLens := MakeWriteFileArgsPathLens()
	nameLens := MakeWriteFileArgsNameLens()

	return F.Flow6(
		F.Flow4(S.ToBytes, J.Unmarshal[WriteFileArgs], result.Chain(checkComplete()), effect.FromResult[WriteFileDeps, WriteFileArgs]),
		effect.Bind(pathLens.Set, F.Flow2(pathLens.Get, resolveIn[WriteFileDeps]())),
		effect.Bind(nameLens.Set, F.Flow2(pathLens.Get, relativeIn[WriteFileDeps]())),
		effect.ChainFirst(F.Flow2(pathLens.Get, makeParentDirs())),
		effect.ChainFirst(writeContent()),
		effect.Map[WriteFileDeps](writtenText),
	)
}

// writeFileDefinition describes write_file to the model.
//
// It is wiring: a getter that builds the plain definition value for root.
func writeFileDefinition(root string) openai.FunctionDefinitionParam {
	return openai.FunctionDefinitionParam{
		Name:        WriteFileName,
		Description: openai.String("Request to write content to a file. If the file exists, it is overwritten; if not, it is created, together with any missing parent directories. Always provide the complete content of the file: the file is replaced with exactly what you send, so omissions such as \"// rest of the code unchanged\" end up in the file."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path (absolute, or relative to the workspace). The workspace is at " + root,
				},
				"content": map[string]any{
					"type":        "string",
					"description": "The complete content of the file, without line numbers.",
				},
				"line_count": map[string]any{
					"type":        "integer",
					"description": "The number of lines of the content, including empty lines. The call fails if the content has fewer lines, because it was most likely truncated.",
				},
			},
			"required": []string{"path", "content", "line_count"},
		},
	}
}

// MakeWriteFileTool builds the write_file [Tool] over the given dependencies.
func MakeWriteFileTool() Reader[WriteFileDeps, Tool] {
	return F.Pipe2(
		F.Flow2(WriteFileDeps.GetWorkspaceRoot, writeFileDefinition),
		reader.Map[WriteFileDeps](F.Curry2(MakeTool)),
		reader.Ap[Tool](F.Flip(WriteFile())),
	)
}
