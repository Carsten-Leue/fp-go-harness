package tools

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	A "github.com/IBM/fp-go/v2/array"
	B "github.com/IBM/fp-go/v2/bytes"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/endomorphism"
	ER "github.com/IBM/fp-go/v2/errors"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	J "github.com/IBM/fp-go/v2/json"
	"github.com/IBM/fp-go/v2/lazy"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/ord"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	openai "github.com/openai/openai-go/v3"
)

// ApplyDiffName is the name under which the model calls the apply_diff tool.
const ApplyDiffName = "apply_diff"

// ApplyDiffDeps is what the apply_diff tool needs: the workspace its paths
// are resolved against and the file system it reads from and writes to.
type ApplyDiffDeps interface {
	WorkspaceDeps
	FileDeps
	FileWriteDeps
}

type applyDiffDeps struct {
	WorkspaceDeps
	FileDeps
	FileWriteDeps
}

func MakeApplyDiffDeps(w WorkspaceDeps, r FileDeps, f FileWriteDeps) ApplyDiffDeps {
	return &applyDiffDeps{w, r, f}
}

func AsApplyDiffDeps[R ApplyDiffDeps](r R) ApplyDiffDeps {
	return r
}

// DiffBlock is one SEARCH/REPLACE block of a diff. StartLine is the 1-based
// line near which the search is expected; 0 means no hint.
//
// fp-go:Lens
type DiffBlock struct {
	StartLine int
	Search    string
	Replace   string
}

// ApplyDiffArgs are the arguments of an apply_diff call. The fields without a
// JSON name are not part of the call; they are filled in while the call runs.
//
// fp-go:Lens
type ApplyDiffArgs struct {
	Path    string      `json:"path"`
	Diff    string      `json:"diff"`
	Blocks  []DiffBlock `json:"-"`
	Name    string      `json:"-"`
	Content string      `json:"-"`
	CRLF    bool        `json:"-"`
}

var (
	errNoBlocks    = errors.New("the diff has no SEARCH/REPLACE block. Each block is <<<<<<< SEARCH, optionally :start_line:N and -------, the search text, =======, the replacement and >>>>>>> REPLACE, each marker on a line of its own")
	errEmptyBlock  = errors.New("the SEARCH section of a block must not be empty")
	diffBlockRegex = regexp.MustCompile(`(?ms)^<<<<<<< SEARCH[ \t]*\n(?::start_line:[ \t]*(\d+)[ \t]*\n)?(?:-------[ \t]*\n)?(.*?)\n?^=======[ \t]*\n(.*?)\n?^>>>>>>> REPLACE[ \t]*$`)
)

// parseStartLine parses the optional start line of a block; a missing one is 0.
func parseStartLine() result.Kleisli[string, int] {
	return F.Flow2(
		option.FromPredicate(S.IsNonEmpty),
		option.Fold(lazy.Of(result.Of(0)), result.Eitherize1(strconv.Atoi)),
	)
}

// andThenSet takes the builder of a [DiffBlock] built so far and the value of
// one more field, and chains the setter of that field after the builder.
func andThenSet[T any](set func(T) Endomorphism[DiffBlock]) func(Endomorphism[DiffBlock]) func(T) Endomorphism[DiffBlock] {
	return F.Flip(F.Flow2(set, endomorphism.Chain[DiffBlock]))
}

// makeDiffBlock is the curried constructor of a [DiffBlock], the start line
// last. Each field is set by its lens, the setters are chained in the
// endomorphism monad and the result is built from the zero block.
func makeDiffBlock() func(string) func(string) func(int) DiffBlock {
	lenses := MakeDiffBlockLenses()

	return F.Flow2(
		lenses.Search.Set,
		F.Flow2(
			andThenSet(lenses.Replace.Set),
			reader.Map[string](F.Flow2(
				andThenSet(lenses.StartLine.Set),
				reader.Map[int](endomorphism.Build[DiffBlock]),
			)),
		),
	)
}

// blockOf is a leaf: it builds a block from the groups of a match of
// [diffBlockRegex].
func blockOf(m []string) Result[DiffBlock] {
	return F.Pipe1(parseStartLine()(m[1]), result.Map(makeDiffBlock()(m[2])(m[3])))
}

// parseBlocks parses the SEARCH/REPLACE blocks of a diff. It fails for a diff
// without blocks and for a block with an empty search.
func parseBlocks() result.Kleisli[string, []DiffBlock] {
	return F.Flow4(
		toLF,
		F.Bind2nd(diffBlockRegex.FindAllStringSubmatch, -1),
		result.TraverseArray(F.Flow2(
			blockOf,
			result.Filter(F.Flow2(MakeDiffBlockSearchLens().Get, S.IsNonEmpty), errEmptyBlock),
		)),
		result.Filter(A.IsNonEmpty[DiffBlock], errNoBlocks),
	)
}

// lineDistance is a leaf: the distance between the start line and the line on
// which a match in the content begins.
func lineDistance(start int, content string, loc []int) int {
	line := strings.Count(content[:loc[0]], "\n") + 1
	return max(line-start, start-line)
}

// matchesOf finds the locations of every occurrence of a literal text.
func matchesOf(search string) Reader[string, [][]int] {
	return F.Bind2nd(regexp.MustCompile(regexp.QuoteMeta(search)).FindAllStringIndex, -1)
}

// nearestMatch is the location of the occurrence of the search of a block
// that begins nearest its start line; without a start line it is the first
// occurrence. Of two occurrences equally near, the first wins.
func nearestMatch(b DiffBlock) Reader[string, Option[[]int]] {
	byDistance := F.Curry2(A.SortByKey[int, []int])(ord.FromStrictCompare[int]())

	return F.Flow2(
		F.Pipe1(
			F.Flow2(F.Curry3(lineDistance)(b.StartLine), byDistance),
			reader.Ap[[][]int](matchesOf(b.Search)),
		),
		A.Head[[]int],
	)
}

// splice is a leaf: it replaces the text at a location of the content.
func splice(replace, content string, loc []int) string {
	return content[:loc[0]] + replace + content[loc[1]:]
}

// noMatch is the error of the block with index i that matches nothing.
func noMatch(i int) func() error {
	return ER.OnNone("block %d of the diff does not match: its SEARCH section was not found. Read the file again: the search must match its current content exactly, including whitespace and indentation", i+1)
}

// applyBlock replaces the occurrence of the search of the block with index i
// nearest its start line and fails when there is none.
func applyBlock(i int, b DiffBlock) result.Kleisli[string, string] {
	return F.Pipe1(
		F.Flow2(F.Curry3(splice)(b.Replace), result.Map[[]int, string]),
		reader.Ap[Result[string]](F.Flow2(nearestMatch(b), result.FromOption[[]int](noMatch(i)))),
	)
}

// thenBlock is a leaf: it runs the block with index i after the blocks
// before it.
func thenBlock(i int, applied result.Kleisli[string, string], b DiffBlock) result.Kleisli[string, string] {
	return F.Flow2(applied, result.Chain(applyBlock(i, b)))
}

// applyBlocks applies the blocks of a diff in order, each to the result of
// the one before, and fails on the first block that doesn't match.
func applyBlocks() func([]DiffBlock) result.Kleisli[string, string] {
	return A.ReduceWithIndex(thenBlock, result.Of[string])
}

// patchedContent applies the blocks of a call to its content.
func patchedContent() result.Kleisli[ApplyDiffArgs, string] {
	return F.Pipe1(
		F.Flow2(MakeApplyDiffArgsBlocksLens().Get, applyBlocks()),
		reader.Ap[Result[string]](MakeApplyDiffArgsContentLens().Get),
	)
}

// patchedText is the content of a call with the line endings of the file.
func patchedText() Reader[ApplyDiffArgs, string] {
	contentLens := MakeApplyDiffArgsContentLens()

	return P.Fold(contentLens.Get, F.Flow2(contentLens.Get, toCRLF))(MakeApplyDiffArgsCRLFLens().Get)
}

// writePatchedWith applies a file writer to the path and the patched content
// of a call.
func writePatchedWith(write func(string) ioresult.Kleisli[[]byte, []byte]) Reader[ApplyDiffArgs, ioresult.IOResult[[]byte]] {
	return F.Pipe1(
		F.Flow2(MakeApplyDiffArgsPathLens().Get, write),
		reader.Ap[ioresult.IOResult[[]byte]](F.Flow2(patchedText(), S.ToBytes)),
	)
}

// writePatched writes the patched content of a call to its resolved path
// through the [FileWriteDeps] of the call.
func writePatched() effect.Kleisli[ApplyDiffDeps, ApplyDiffArgs, []byte] {
	return F.Flow2(
		F.Flip(F.Flow2(ApplyDiffDeps.GetWriteFile, writePatchedWith)),
		reader.Map[ApplyDiffDeps](thunk.FromIOResult[[]byte]),
	)
}

// appliedText is a leaf: one formatter over several fields.
func appliedText(a ApplyDiffArgs) string {
	return fmt.Sprintf("Applied %d block(s) to %s.", len(a.Blocks), a.Name)
}

// ApplyDiff runs an apply_diff call: it decodes the JSON arguments, parses
// the SEARCH/REPLACE blocks of the diff, resolves the path against the
// workspace, reads the file, applies the blocks in order and writes the file
// back.
//
// Each block replaces one occurrence of its search: the one nearest its
// :start_line: hint, or the first one without a hint. Since the blocks apply
// one after the other, a hint may be off by the lines that earlier blocks
// added or removed. The file is edited with LF line endings and keeps CRLF
// line endings if it has them.
//
// Invalid arguments, a diff without blocks, a path outside the workspace, a
// block that doesn't match and a read or write error fail the effect, and
// the file stays unchanged; [MakeToolCall] turns the failure into a tool
// message for the model.
func ApplyDiff() effect.Kleisli[ApplyDiffDeps, string, string] {
	diffLens := MakeApplyDiffArgsDiffLens()
	blocksLens := MakeApplyDiffArgsBlocksLens()
	pathLens := MakeApplyDiffArgsPathLens()
	nameLens := MakeApplyDiffArgsNameLens()
	contentLens := MakeApplyDiffArgsContentLens()
	crlfLens := MakeApplyDiffArgsCRLFLens()

	return F.Flow9(
		F.Flow4(
			S.ToBytes,
			J.Unmarshal[ApplyDiffArgs],
			result.Bind(blocksLens.Set, F.Flow2(diffLens.Get, parseBlocks())),
			effect.FromResult[ApplyDiffDeps, ApplyDiffArgs],
		),
		effect.BindL(pathLens, resolveIn[ApplyDiffDeps]()),
		effect.Bind(nameLens.Set, F.Flow2(pathLens.Get, relativeIn[ApplyDiffDeps]())),
		effect.Bind(contentLens.Set, F.Flow3(pathLens.Get, readBytes[ApplyDiffDeps], effect.Map[ApplyDiffDeps](B.ToString))),
		effect.Let[ApplyDiffDeps](crlfLens.Set, F.Flow2(contentLens.Get, hasCRLF())),
		effect.LetL[ApplyDiffDeps](contentLens, toLF),
		effect.BindResultK[ApplyDiffDeps](contentLens.Set, patchedContent()),
		effect.ChainFirst(writePatched()),
		effect.Map[ApplyDiffDeps](appliedText),
	)
}

// applyDiffDefinition describes apply_diff to the model.
func applyDiffDefinition(root string) openai.FunctionDefinitionParam {
	return openai.FunctionDefinitionParam{
		Name:        ApplyDiffName,
		Description: openai.String("Request to apply targeted changes to an existing file by replacing sections of its content. Each SEARCH/REPLACE block replaces one occurrence of its search text: the one nearest :start_line: if given, else the first one. Use several blocks in one call for several changes; they apply in order, and the call fails without changing the file if any block doesn't match.\n\nDiff format:\n```\n<<<<<<< SEARCH\n:start_line:10\n-------\n[exact content to find, including whitespace]\n=======\n[new content]\n>>>>>>> REPLACE\n```"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path (absolute, or relative to the workspace). The workspace is at " + root,
				},
				"diff": map[string]any{
					"type":        "string",
					"description": "One or more SEARCH/REPLACE blocks. The search must match the current content exactly; :start_line: is optional.",
				},
			},
			"required": []string{"path", "diff"},
		},
	}
}

// MakeApplyDiffTool builds the apply_diff [Tool] over the given dependencies.
func MakeApplyDiffTool() Reader[ApplyDiffDeps, Tool] {
	return F.Pipe2(
		F.Flow2(ApplyDiffDeps.GetWorkspaceRoot, applyDiffDefinition),
		reader.Map[ApplyDiffDeps](F.Curry2(MakeTool)),
		reader.Ap[Tool](F.Flip(ApplyDiff())),
	)
}
