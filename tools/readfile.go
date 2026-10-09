package tools

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	A "github.com/IBM/fp-go/v2/array"
	B "github.com/IBM/fp-go/v2/bytes"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	ER "github.com/IBM/fp-go/v2/errors"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/ioresult/file"
	J "github.com/IBM/fp-go/v2/json"
	N "github.com/IBM/fp-go/v2/number"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/readerresult"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	openai "github.com/openai/openai-go/v3"
)

// ReadFileName is the name under which the model calls the read_file tool.
const ReadFileName = "read_file"

// FileDeps gives the tools access to the file system.
type FileDeps interface {
	GetReadFile() ioresult.Kleisli[string, []byte]
}

type fileDeps struct {
	readFile ioresult.Kleisli[string, []byte]
}

func (d *fileDeps) GetReadFile() ioresult.Kleisli[string, []byte] {
	return d.readFile
}

// MakeFileDeps builds [FileDeps] that read files with readFile.
func MakeFileDeps(readFile ioresult.Kleisli[string, []byte]) FileDeps {
	return &fileDeps{readFile}
}

// MakeDefaultFileDeps builds [FileDeps] over the operating system's file system.
func MakeDefaultFileDeps() FileDeps {
	return MakeFileDeps(file.ReadFile)
}

func AsFileDeps[R FileDeps](r R) FileDeps {
	return r
}

// ReadFileDeps is what the read_file tool needs: the workspace its paths are
// resolved against and the file system it reads from.
type ReadFileDeps interface {
	WorkspaceDeps
	FileDeps
}

type readFileDeps struct {
	WorkspaceDeps
	FileDeps
}

func MakeReadFileDeps(w WorkspaceDeps, f FileDeps) ReadFileDeps {
	return &readFileDeps{w, f}
}

func AsReadFileDeps[R ReadFileDeps](r R) ReadFileDeps {
	return r
}

// ReadFileArgs are the arguments of a read_file call. Name and Content are
// not part of the call; they are filled in while the call runs.
//
// fp-go:Lens
type ReadFileArgs struct {
	Path    string `json:"path"`
	Range   string `json:"range"`
	Name    string `json:"-"`
	Content []byte `json:"-"`
}

var (
	errRangeFormat    = errors.New("range must have the format start-end, e.g. 10-20")
	errRangeUnordered = errors.New("the start of the range must not be after its end")
)

// cutRange is a leaf: [strings.Cut] returns three values.
func cutRange(rng string) Option[Pair[string, string]] {
	start, end, found := strings.Cut(rng, "-")
	return option.FromPredicate(F.Constant1[Pair[string, string]](found))(pair.MakePair(start, end))
}

// parseLineNumber parses a 1-based line number.
func parseLineNumber() result.Kleisli[string, int] {
	return F.Flow3(
		strings.TrimSpace,
		result.Eitherize1(strconv.Atoi),
		result.Chain(result.FromPredicate(N.MoreThan(0), ER.OnSome[int]("line number %d must be at least 1"))),
	)
}

// lineRange parses both ends of a range.
func lineRange() result.Kleisli[Pair[string, string], Pair[int, int]] {
	parse := parseLineNumber()

	return F.Flow3(
		pair.BiMap(parse, parse),
		pair.Paired(result.SequenceT2[int, int]),
		result.Map(pair.FromTuple[int, int]),
	)
}

func isOrdered(p Pair[int, int]) bool {
	return pair.Head(p) <= pair.Tail(p)
}

// parseRange parses a "start-end" range of 1-based, inclusive line numbers.
func parseRange() result.Kleisli[string, Pair[int, int]] {
	return F.Flow3(
		F.Flow2(cutRange, result.FromOption[Pair[string, string]](F.Constant(errRangeFormat))),
		result.Chain(lineRange()),
		result.Chain(result.FromPredicate(isOrdered, F.Constant1[Pair[int, int]](errRangeUnordered))),
	)
}

// numberedLine is a leaf: one formatter over the line number and the line.
func numberedLine(first int) func(int, string) string {
	return func(i int, line string) string {
		return fmt.Sprintf("%d | %s", first+i, line)
	}
}

// numberLines prefixes each line with its number, counting from first, and
// joins them. As in the recordings, an empty line renders as "N | ", except
// the last one: trailing blanks of the result are dropped, so it is "N |".
func numberLines(first int) func([]string) string {
	return F.Flow3(
		A.MapWithIndex(numberedLine(first)),
		S.Join("\n"),
		F.Bind2nd(strings.TrimRight, " "),
	)
}

// pastEndError is a leaf: one formatter over the start and the line count.
func pastEndError(start int) func([]string) error {
	return func(lines []string) error {
		return fmt.Errorf("range starts at line %d, but the file has %d lines", start, len(lines))
	}
}

// numberRange numbers the lines start to end (1-based, inclusive). A range
// whose end lies beyond the end of the file is cut off; a range that starts
// beyond it fails.
func numberRange(start, end int) result.Kleisli[[]string, string] {
	return F.Flow2(
		result.FromPredicate(F.Flow2(A.Size[string], N.MoreThan(start-1)), pastEndError(start)),
		result.Map(F.Flow2(
			A.Slice[string](start-1, end),
			numberLines(start),
		)),
	)
}

// lineSelector turns the range argument into the function that numbers the
// selected lines. An empty range selects the whole file.
func lineSelector() result.Kleisli[string, result.Kleisli[[]string, string]] {
	return P.Fold(
		F.Flow2(parseRange(), result.Map(pair.Paired(numberRange))),
		F.Constant1[string](result.Of(F.Flow2(numberLines(1), result.Of[string]))),
	)(S.IsEmpty)
}

// splitLines splits content at line breaks and drops the carriage returns of
// CRLF line endings.
func splitLines() func([]byte) []string {
	return F.Flow3(
		B.ToString,
		F.Bind2nd(strings.Split, "\n"),
		A.Map(F.Bind2nd(strings.TrimSuffix, "\r")),
	)
}

// renderFile renders the selected lines of the content in the format of the
// recordings: "Contents of file <name>:\n\n1 | ...".
func renderFile() ReaderResult[ReadFileArgs, string] {
	nameLens := MakeReadFileArgsNameLens()
	rangeLens := MakeReadFileArgsRangeLens()
	contentLens := MakeReadFileArgsContentLens()

	body := F.Pipe2(
		F.Flow2(rangeLens.Get, lineSelector()),
		readerresult.Ap[Result[string]](F.Flow3(contentLens.Get, splitLines(), result.Of[[]string])),
		readerresult.ChainResultK[ReadFileArgs](F.Identity[Result[string]]),
	)

	return F.Pipe1(
		F.Flow4(nameLens.Get, S.Format[string]("Contents of file %s:\n\n"), S.Prepend, result.Of[Endomorphism[string]]),
		readerresult.Ap[string](body),
	)
}

// readBytes reads the file at an absolute path.
func readBytes(path string) Effect[ReadFileDeps, []byte] {
	return F.Pipe1(
		effect.Asks(ReadFileDeps.GetReadFile),
		effect.ChainThunkK[ReadFileDeps](F.Flow2(
			reader.Read[ioresult.IOResult[[]byte]](path),
			thunk.FromIOResult[[]byte],
		)),
	)
}

// ReadFile runs a read_file call: it decodes the JSON arguments, resolves the
// path against the workspace, reads the file and renders it with line
// numbers, optionally restricted to the 1-based, inclusive range "start-end".
//
// Invalid arguments, a path outside the workspace and a read error fail the
// effect; [MakeToolCall] turns the failure into a tool message for the model.
func ReadFile() effect.Kleisli[ReadFileDeps, string, string] {
	pathLens := MakeReadFileArgsPathLens()
	nameLens := MakeReadFileArgsNameLens()
	contentLens := MakeReadFileArgsContentLens()

	return F.Flow5(
		F.Flow3(S.ToBytes, J.Unmarshal[ReadFileArgs], effect.FromResult[ReadFileDeps, ReadFileArgs]),
		effect.Bind(pathLens.Set, F.Flow2(pathLens.Get, resolveIn[ReadFileDeps]())),
		effect.Bind(nameLens.Set, F.Flow2(pathLens.Get, relativeIn[ReadFileDeps]())),
		effect.Bind(contentLens.Set, F.Flow2(pathLens.Get, readBytes)),
		effect.ChainResultK[ReadFileDeps](renderFile()),
	)
}

// readFileDefinition describes read_file to the model, following the
// recorded definition.
func readFileDefinition(root string) openai.FunctionDefinitionParam {
	return openai.FunctionDefinitionParam{
		Name:        ReadFileName,
		Description: openai.String("Request to read the contents of a text file, with line numbers. Use line ranges to efficiently read specific portions of large files."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path (absolute, or relative to the workspace). The workspace is at " + root,
				},
				"range": map[string]any{
					"type":        "string",
					"description": `Single line range in format "start-end" (1-based, inclusive), e.g. "1683-1800"`,
				},
			},
			"required": []string{"path"},
		},
	}
}

// MakeReadFileTool builds the read_file [Tool] over the given dependencies.
func MakeReadFileTool() Reader[ReadFileDeps, Tool] {
	return F.Pipe2(
		F.Flow2(ReadFileDeps.GetWorkspaceRoot, readFileDefinition),
		reader.Map[ReadFileDeps](F.Curry2(MakeTool)),
		reader.Ap[Tool](F.Flip(ReadFile())),
	)
}
