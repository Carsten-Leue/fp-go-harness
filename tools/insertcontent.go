package tools

import (
	"errors"
	"fmt"
	"math"
	"strings"

	B "github.com/IBM/fp-go/v2/bytes"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	J "github.com/IBM/fp-go/v2/json"
	"github.com/IBM/fp-go/v2/lazy"
	N "github.com/IBM/fp-go/v2/number"
	"github.com/IBM/fp-go/v2/option"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	openai "github.com/openai/openai-go/v3"
)

// InsertContentName is the name under which the model calls the
// insert_content tool.
const InsertContentName = "insert_content"

// InsertContentDeps is what the insert_content tool needs: the workspace its
// paths are resolved against and the file system it reads from and writes to.
type InsertContentDeps interface {
	WorkspaceDeps
	FileDeps
	FileWriteDeps
}

type insertContentDeps struct {
	WorkspaceDeps
	FileDeps
	FileWriteDeps
}

// MakeInsertContentDeps is wiring: it combines a workspace, read access and
// write access to the file system into [InsertContentDeps].
func MakeInsertContentDeps(w WorkspaceDeps, r FileDeps, f FileWriteDeps) InsertContentDeps {
	return &insertContentDeps{w, r, f}
}

// AsInsertContentDeps is wiring: it narrows any wider dependencies to
// [InsertContentDeps], for use with [reader.Local] and [effect.Local].
func AsInsertContentDeps[R InsertContentDeps](r R) InsertContentDeps {
	return r
}

// InsertContentArgs are the arguments of an insert_content call. Line is the
// 1-based line the content is inserted before; 0 appends it to the end of the
// file. The fields without a JSON name are not part of the call; they are
// filled in while the call runs: Text is the content of the file.
//
// fp-go:Lens
type InsertContentArgs struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Content string `json:"content"`
	Name    string `json:"-"`
	Text    string `json:"-"`
	CRLF    bool   `json:"-"`
}

var (
	errEmptyContent    = errors.New("content must not be empty")
	errNegativeInsLine = errors.New("line must not be negative: use 0 to append to the end of the file")
)

// checkInsertArgs fails for empty content and for a negative line.
func checkInsertArgs() result.Kleisli[InsertContentArgs, InsertContentArgs] {
	contentLens := MakeInsertContentArgsContentLens()
	lineLens := MakeInsertContentArgsLineLens()

	return F.Flow2(
		result.FromPredicate(F.Flow2(contentLens.Get, S.IsNonEmpty), F.Constant1[InsertContentArgs](errEmptyContent)),
		result.Chain(result.FromPredicate(F.Flow2(lineLens.Get, P.Not(N.LessThan(0))), F.Constant1[InsertContentArgs](errNegativeInsLine))),
	)
}

// lacksLineBreak tests whether a text is not empty and does not end with a
// line break.
func lacksLineBreak() P.Predicate[string] {
	return F.Pipe1(
		S.IsNonEmpty,
		P.And(P.Not(F.Bind2nd(strings.HasSuffix, "\n"))),
	)
}

// terminated ends a text that is not empty with a line break, so that lines
// inserted after it start on a line of their own.
func terminated() Endomorphism[string] {
	return P.Fold(F.Identity[string], S.Append("\n"))(lacksLineBreak())
}

// insertIndex is the index of the line the content of a call is inserted
// before; line 0 is the end of the file.
func insertIndex() Reader[InsertContentArgs, int] {
	lineLens := MakeInsertContentArgsLineLens()

	return F.Flow3(
		lineLens.Get,
		option.FromPredicate(N.MoreThan(0)),
		option.Fold(lazy.Of(math.MaxInt), N.Add(-1)),
	)
}

// insertedLines are the lines of a call to insert, with LF line endings and
// a final line break.
func insertedLines() Reader[InsertContentArgs, string] {
	contentLens := MakeInsertContentArgsContentLens()

	return F.Flow3(contentLens.Get, toLF, terminated())
}

// fileLines are the lines of the text of a call, each with its line break,
// the last one included.
func fileLines() Reader[InsertContentArgs, []string] {
	textLens := MakeInsertContentArgsTextLens()

	return F.Flow3(textLens.Get, terminated(), splitAfterLines())
}

// insertAt inserts lines before the line at an index of a file's lines.
func insertAt() Reader[int, Reader[string, Reader[[]string, string]]] {
	return F.Pipe2(
		F.Curry3(replaceLines),
		reader.Flatten,
		reader.Map[int](reader.Local[Reader[[]string, string]](F.Constant1[string, string])),
	)
}

// insertedAt inserts the lines of a call into the text of the file before the
// line at an index.
func insertedAt() reader.Kleisli[InsertContentArgs, int, string] {
	return F.Flow3(
		insertAt(),
		reader.Local[Reader[[]string, string]](insertedLines()),
		reader.Ap[string](fileLines()),
	)
}

// insertedText inserts the lines of a call into the text of the file before
// the line of the call. A line beyond the end of the file appends the lines.
func insertedText() Reader[InsertContentArgs, string] {
	return F.Pipe1(
		insertIndex(),
		reader.Chain(insertedAt()),
	)
}

// fileText is the text of a call with the line endings of the file.
func fileText() Reader[InsertContentArgs, string] {
	textLens := MakeInsertContentArgsTextLens()
	crlfLens := MakeInsertContentArgsCRLFLens()

	return withLineEndings(textLens, crlfLens)
}

// writeInsertedWith applies a file writer to the path and the text of a call.
func writeInsertedWith() Reader[func(string) ioresult.Kleisli[[]byte, []byte], Reader[InsertContentArgs, ioresult.IOResult[[]byte]]] {
	pathLens := MakeInsertContentArgsPathLens()

	return F.Flow2(
		reader.Local[ioresult.Kleisli[[]byte, []byte]](pathLens.Get),
		reader.Ap[ioresult.IOResult[[]byte]](F.Flow2(fileText(), S.ToBytes)),
	)
}

// writeInserted writes the text of a call to its resolved path through the
// [FileWriteDeps] of the call.
func writeInserted() effect.Kleisli[InsertContentDeps, InsertContentArgs, []byte] {
	return F.Flow2(
		F.Flip(F.Flow2(InsertContentDeps.GetWriteFile, writeInsertedWith())),
		effect.FromReaderIOResult[InsertContentDeps, []byte],
	)
}

// lineCount counts the lines of a call to insert.
func lineCount() Reader[InsertContentArgs, int] {
	return F.Flow2(insertedLines(), F.Bind2nd(strings.Count, "\n"))
}

// insertedAtLine is a leaf: one formatter over several fields.
func insertedAtLine(a InsertContentArgs) string {
	return fmt.Sprintf("Inserted %d line(s) into %s before line %d.", lineCount()(a), a.Name, a.Line)
}

// appendedText is a leaf: one formatter over several fields.
func appendedText(a InsertContentArgs) string {
	return fmt.Sprintf("Appended %d line(s) to %s.", lineCount()(a), a.Name)
}

// insertedMessage is the result text of a call.
func insertedMessage() Reader[InsertContentArgs, string] {
	lineLens := MakeInsertContentArgsLineLens()

	return P.Fold(appendedText, insertedAtLine)(F.Flow2(lineLens.Get, N.MoreThan(0)))
}

// InsertContent runs an insert_content call: it decodes and checks the JSON
// arguments, resolves the path against the workspace, reads the file, inserts
// the content before the given line (or appends it for line 0) and writes the
// file back.
//
// The content always ends with a line break, so it never joins the line it is
// inserted before; a file without a final line break gets one. The file is
// edited with LF line endings and keeps CRLF line endings if it has them.
//
// Invalid arguments, a path outside the workspace and a read or write error
// fail the effect; [MakeToolCall] turns the failure into a tool message for
// the model.
func InsertContent() effect.Kleisli[InsertContentDeps, string, string] {
	pathLens := MakeInsertContentArgsPathLens()
	nameLens := MakeInsertContentArgsNameLens()
	textLens := MakeInsertContentArgsTextLens()
	crlfLens := MakeInsertContentArgsCRLFLens()

	return F.Flow9(
		F.Flow4(
			S.ToBytes,
			J.Unmarshal[InsertContentArgs],
			result.Chain(checkInsertArgs()),
			effect.FromResult[InsertContentDeps, InsertContentArgs],
		),
		effect.BindL(pathLens, resolveIn[InsertContentDeps]()),
		effect.Bind(nameLens.Set, F.Flow2(pathLens.Get, relativeIn[InsertContentDeps]())),
		effect.Bind(textLens.Set, F.Flow3(pathLens.Get, readBytes[InsertContentDeps], effect.Map[InsertContentDeps](B.ToString))),
		effect.Let[InsertContentDeps](crlfLens.Set, F.Flow2(textLens.Get, hasCRLF())),
		effect.LetL[InsertContentDeps](textLens, toLF),
		effect.Let[InsertContentDeps](textLens.Set, insertedText()),
		effect.ChainFirst(writeInserted()),
		effect.Map[InsertContentDeps](insertedMessage()),
	)
}

// insertContentDefinition describes insert_content to the model.
//
// It is wiring: a getter that builds the plain definition value for root.
func insertContentDefinition(root string) openai.FunctionDefinitionParam {
	return openai.FunctionDefinitionParam{
		Name:        InsertContentName,
		Description: openai.String("Use this tool specifically for adding new lines of content into a file without modifying existing content. Specify the line number to insert before, or use line 0 to append to the end. Ideal for adding imports, functions, configuration blocks, log entries, or any multi-line text block."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path (absolute, or relative to the workspace). The workspace is at " + root,
				},
				"line": map[string]any{
					"type":        "integer",
					"description": "Line number where content will be inserted (1-based). Use 0 to append at end of file. Use any positive number to insert before that line",
				},
				"content": map[string]any{
					"type":        "string",
					"description": "The content to insert at the specified line",
				},
			},
			"required": []string{"path", "line", "content"},
		},
	}
}

// MakeInsertContentTool builds the insert_content [Tool] over the given
// dependencies.
func MakeInsertContentTool() Reader[InsertContentDeps, Tool] {
	return F.Pipe2(
		F.Flow2(InsertContentDeps.GetWorkspaceRoot, insertContentDefinition),
		reader.Map[InsertContentDeps](F.Curry2(MakeTool)),
		reader.Ap[Tool](F.Flip(InsertContent())),
	)
}
