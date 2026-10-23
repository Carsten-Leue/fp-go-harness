package tools

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"

	A "github.com/IBM/fp-go/v2/array"
	B "github.com/IBM/fp-go/v2/bytes"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	ER "github.com/IBM/fp-go/v2/errors"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	J "github.com/IBM/fp-go/v2/json"
	"github.com/IBM/fp-go/v2/lazy"
	N "github.com/IBM/fp-go/v2/number"
	L "github.com/IBM/fp-go/v2/optics/lens"
	"github.com/IBM/fp-go/v2/option"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	openai "github.com/openai/openai-go/v3"
)

// SearchAndReplaceName is the name under which the model calls the
// search_and_replace tool.
const SearchAndReplaceName = "search_and_replace"

// SearchAndReplaceDeps is what the search_and_replace tool needs: the
// workspace its paths are resolved against and the file system it reads from
// and writes to.
type SearchAndReplaceDeps interface {
	WorkspaceDeps
	FileDeps
	FileWriteDeps
}

type searchAndReplaceDeps struct {
	WorkspaceDeps
	FileDeps
	FileWriteDeps
}

// MakeSearchAndReplaceDeps is wiring: it combines a workspace, read access and
// write access to the file system into [SearchAndReplaceDeps].
func MakeSearchAndReplaceDeps(w WorkspaceDeps, r FileDeps, f FileWriteDeps) SearchAndReplaceDeps {
	return &searchAndReplaceDeps{w, r, f}
}

// AsSearchAndReplaceDeps is wiring: it narrows any wider dependencies to
// [SearchAndReplaceDeps], for use with [reader.Local] and [effect.Local].
func AsSearchAndReplaceDeps[R SearchAndReplaceDeps](r R) SearchAndReplaceDeps {
	return r
}

// SearchAndReplaceArgs are the arguments of a search_and_replace call.
// StartLine and EndLine are 1-based and inclusive; 0 leaves that end of the
// range open. The fields without a JSON name are not part of the call; they
// are filled in while the call runs.
//
// fp-go:Lens
type SearchAndReplaceArgs struct {
	Path       string         `json:"path"`
	Search     string         `json:"search"`
	Replace    string         `json:"replace"`
	UseRegex   bool           `json:"use_regex"`
	IgnoreCase bool           `json:"ignore_case"`
	StartLine  int            `json:"start_line"`
	EndLine    int            `json:"end_line"`
	Pattern    *regexp.Regexp `json:"-"`
	Name       string         `json:"-"`
	Content    string         `json:"-"`
	CRLF       bool           `json:"-"`
	Count      int            `json:"-"`
}

var (
	errEmptySearch  = errors.New("search must not be empty")
	errNegativeLine = errors.New("start_line and end_line must not be negative")
	errLineOrder    = errors.New("end_line must not be before start_line")
)

// toLF and toCRLF convert between line endings. The content is edited with
// LF line endings, the way read_file shows it, so that a search over several
// lines also matches in a file with CRLF line endings.
var (
	toLF   = strings.NewReplacer("\r\n", "\n").Replace
	toCRLF = strings.NewReplacer("\n", "\r\n").Replace
)

// hasCRLF tests whether a text has CRLF line endings.
func hasCRLF() P.Predicate[string] {
	return F.Bind2nd(strings.Contains, "\r\n")
}

// withLineEndings is the text of the arguments of an editing call, read by
// the text lens, with the line endings of the file: CRLF when the crlf lens
// is set, else LF.
//
// It is a generic combinator over the two lenses of the arguments and keeps
// its parameters: Go can't infer T through a nullary call.
func withLineEndings[T any](text L.Lens[T, string], crlf L.Lens[T, bool]) Reader[T, string] {
	return P.Fold(text.Get, F.Flow2(text.Get, toCRLF))(crlf.Get)
}

// nonNegativeLines tests whether both line numbers of a call are 0 or more.
func nonNegativeLines() P.Predicate[SearchAndReplaceArgs] {
	startLineLens := MakeSearchAndReplaceArgsStartLineLens()
	endLineLens := MakeSearchAndReplaceArgsEndLineLens()
	nonNegative := P.Not(N.LessThan(0))

	return F.Pipe1(
		F.Flow2(startLineLens.Get, nonNegative),
		P.And(F.Flow2(endLineLens.Get, nonNegative)),
	)
}

// isOrderedLines is a leaf: it compares two fields, and an end_line of 0
// leaves the end of the range open.
func isOrderedLines(a SearchAndReplaceArgs) bool {
	return a.EndLine == 0 || a.StartLine <= a.EndLine
}

// checkArgs fails for an empty search and for an invalid line range.
func checkArgs() result.Kleisli[SearchAndReplaceArgs, SearchAndReplaceArgs] {
	searchLens := MakeSearchAndReplaceArgsSearchLens()

	return F.Flow3(
		result.FromPredicate(F.Flow2(searchLens.Get, S.IsNonEmpty), F.Constant1[SearchAndReplaceArgs](errEmptySearch)),
		result.Chain(result.FromPredicate(nonNegativeLines(), F.Constant1[SearchAndReplaceArgs](errNegativeLine))),
		result.Chain(result.FromPredicate(isOrderedLines, F.Constant1[SearchAndReplaceArgs](errLineOrder))),
	)
}

// patternSource is the regular expression of a call: the search itself for a
// regex search, the quoted search for a literal one, case-insensitive when
// ignore_case is set.
func patternSource() Reader[SearchAndReplaceArgs, string] {
	searchLens := MakeSearchAndReplaceArgsSearchLens()
	ignoreCaseLens := MakeSearchAndReplaceArgsIgnoreCaseLens()
	useRegexLens := MakeSearchAndReplaceArgsUseRegexLens()

	return F.Pipe1(
		F.Flow2(ignoreCaseLens.Get, whenSet(S.Prepend("(?i)"))),
		reader.Ap[string](P.Fold(
			F.Flow2(searchLens.Get, regexp.QuoteMeta),
			searchLens.Get,
		)(useRegexLens.Get)),
	)
}

// compileSearch compiles the regular expression of a call.
func compileSearch() result.Kleisli[SearchAndReplaceArgs, *regexp.Regexp] {
	return F.Flow3(
		patternSource(),
		result.Eitherize1(regexp.Compile),
		result.MapLeft[*regexp.Regexp](ER.OnError("search is not a valid regular expression")),
	)
}

// firstIndex is the index of the first line of the range; an open start is
// the first line of the file.
func firstIndex() Reader[SearchAndReplaceArgs, int] {
	startLineLens := MakeSearchAndReplaceArgsStartLineLens()

	return F.Flow4(
		startLineLens.Get,
		option.FromPredicate(N.MoreThan(0)),
		option.GetOrElse(lazy.Of(1)),
		N.Add(-1),
	)
}

// endIndex is the index after the last line of the range; an open end is the
// end of the file.
func endIndex() Reader[SearchAndReplaceArgs, int] {
	endLineLens := MakeSearchAndReplaceArgsEndLineLens()

	return F.Flow3(
		endLineLens.Get,
		option.FromPredicate(N.MoreThan(0)),
		option.GetOrElse(lazy.Of(math.MaxInt)),
	)
}

// joinLineRange joins the lines low to high (exclusive) of a text split after its
// line breaks. Indices beyond the end of the text select no lines.
func joinLineRange() Reader[int, Reader[int, Reader[[]string, string]]] {
	return F.Flow2(
		F.Curry2(A.Slice[string]),
		reader.Map[int](reader.Map[[]string](S.Join(""))),
	)
}

// replaceLines is a constructor that [F.Curry3] lifts: it applies f to the
// lines low to high (exclusive) of a text split after its line breaks and
// keeps the lines before and after them.
func replaceLines(low, high int, f Endomorphism[string]) Reader[[]string, string] {
	join := joinLineRange()

	rest := F.Pipe1(
		F.Flow3(join(low)(high), f, S.Prepend),
		reader.Ap[string](join(high)(math.MaxInt)),
	)

	return F.Pipe1(
		F.Flow2(join(0)(low), S.Prepend),
		reader.Ap[string](rest),
	)
}

// splitAfterLines splits a text after its line breaks, so that joining the
// lines gives the text back.
func splitAfterLines() A.Kleisli[string, string] {
	return F.Bind2nd(strings.SplitAfter, "\n")
}

// literalReplace is a leaf: it reads the pattern and the replacement, which is
// inserted as it is.
func literalReplace(a SearchAndReplaceArgs) Endomorphism[string] {
	return F.Bind2nd(a.Pattern.ReplaceAllLiteralString, a.Replace)
}

// expandingReplace is a leaf: it reads the pattern and the replacement, in
// which $1 and ${name} expand to the groups of the match.
func expandingReplace(a SearchAndReplaceArgs) Endomorphism[string] {
	return F.Bind2nd(a.Pattern.ReplaceAllString, a.Replace)
}

// replaceAll replaces every match of the pattern of a call in a text.
func replaceAll() Reader[SearchAndReplaceArgs, Endomorphism[string]] {
	useRegexLens := MakeSearchAndReplaceArgsUseRegexLens()

	return P.Fold(literalReplace, expandingReplace)(useRegexLens.Get)
}

// contentSplit is the content of a call split after its line breaks.
func contentSplit() Reader[SearchAndReplaceArgs, []string] {
	contentLens := MakeSearchAndReplaceArgsContentLens()

	return F.Flow2(contentLens.Get, splitAfterLines())
}

// rangeRegion is the text of the lines within the line range of a call.
func rangeRegion() Reader[SearchAndReplaceArgs, string] {
	return F.Pipe2(
		F.Flow2(firstIndex(), joinLineRange()),
		reader.Ap[Reader[[]string, string]](endIndex()),
		reader.Ap[string](contentSplit()),
	)
}

// countMatches counts the matches of a regular expression in a text.
func countMatches() Reader[*regexp.Regexp, Reader[string, int]] {
	return F.Flow2(
		F.Curry2(F.Bind3of3((*regexp.Regexp).FindAllStringIndex)(-1)),
		reader.Map[string](A.Size[[]int]),
	)
}

// matchCount counts the matches of the pattern of a call within its line
// range of the content.
func matchCount() Reader[SearchAndReplaceArgs, int] {
	patternLens := MakeSearchAndReplaceArgsPatternLens()

	return F.Pipe1(
		F.Flow2(patternLens.Get, countMatches()),
		reader.Ap[int](rangeRegion()),
	)
}

// editedContent replaces the matches of a call within its line range of the
// content.
func editedContent() Reader[SearchAndReplaceArgs, string] {
	return F.Pipe3(
		F.Flow2(firstIndex(), F.Curry3(replaceLines)),
		reader.Ap[func(Endomorphism[string]) Reader[[]string, string]](endIndex()),
		reader.Ap[Reader[[]string, string]](replaceAll()),
		reader.Ap[string](contentSplit()),
	)
}

// noMatchError is a leaf: one formatter over several fields.
func noMatchError(a SearchAndReplaceArgs) error {
	return fmt.Errorf("no match for %q in %s. Read the file again: the search must match its current content exactly", a.Search, a.Name)
}

// edit counts the matches of a call, fails when there are none and replaces
// them in the content.
func edit() result.Kleisli[SearchAndReplaceArgs, SearchAndReplaceArgs] {
	countLens := MakeSearchAndReplaceArgsCountLens()
	contentLens := MakeSearchAndReplaceArgsContentLens()

	return F.Flow3(
		F.Flow2(result.Of[SearchAndReplaceArgs], result.Let(countLens.Set, matchCount())),
		result.Chain(result.FromPredicate(F.Flow2(countLens.Get, N.MoreThan(0)), noMatchError)),
		result.Map(F.Pipe1(editedContent(), reader.Chain(contentLens.Set))),
	)
}

// editedText is the content of a call with the line endings of the file.
func editedText() Reader[SearchAndReplaceArgs, string] {
	contentLens := MakeSearchAndReplaceArgsContentLens()
	crlfLens := MakeSearchAndReplaceArgsCRLFLens()

	return withLineEndings(contentLens, crlfLens)
}

// writeEditedWith applies a file writer to the path and the edited content of
// a call.
func writeEditedWith() Reader[func(string) ioresult.Kleisli[[]byte, []byte], Reader[SearchAndReplaceArgs, ioresult.IOResult[[]byte]]] {
	pathLens := MakeSearchAndReplaceArgsPathLens()

	return F.Flow2(
		reader.Local[ioresult.Kleisli[[]byte, []byte]](pathLens.Get),
		reader.Ap[ioresult.IOResult[[]byte]](F.Flow2(editedText(), S.ToBytes)),
	)
}

// writeEdited writes the edited content of a call to its resolved path
// through the [FileWriteDeps] of the call.
func writeEdited() effect.Kleisli[SearchAndReplaceDeps, SearchAndReplaceArgs, []byte] {
	return F.Flow2(
		F.Flip(F.Flow2(SearchAndReplaceDeps.GetWriteFile, writeEditedWith())),
		reader.Map[SearchAndReplaceDeps](thunk.FromIOResult[[]byte]),
	)
}

// replacedText is a leaf: one formatter over several fields.
func replacedText(a SearchAndReplaceArgs) string {
	return fmt.Sprintf("Replaced %d occurrence(s) in %s.", a.Count, a.Name)
}

// SearchAndReplace runs a search_and_replace call: it decodes and checks the
// JSON arguments, compiles the search into a regular expression, resolves the
// path against the workspace, reads the file, replaces every match within the
// optional line range and writes the file back.
//
// The file is edited with LF line endings and keeps CRLF line endings if it
// has them, so a search over several lines matches either way.
//
// Invalid arguments, an invalid regular expression, a path outside the
// workspace, a search without a match and a read or write error fail the
// effect; [MakeToolCall] turns the failure into a tool message for the model.
func SearchAndReplace() effect.Kleisli[SearchAndReplaceDeps, string, string] {
	pathLens := MakeSearchAndReplaceArgsPathLens()
	nameLens := MakeSearchAndReplaceArgsNameLens()
	contentLens := MakeSearchAndReplaceArgsContentLens()
	patternLens := MakeSearchAndReplaceArgsPatternLens()
	crlfLens := MakeSearchAndReplaceArgsCRLFLens()

	return F.Flow8(
		F.Flow5(
			S.ToBytes,
			J.Unmarshal[SearchAndReplaceArgs],
			result.Chain(checkArgs()),
			result.Bind(patternLens.Set, compileSearch()),
			effect.FromResult[SearchAndReplaceDeps, SearchAndReplaceArgs],
		),
		effect.Bind(pathLens.Set, F.Flow2(pathLens.Get, resolveIn[SearchAndReplaceDeps]())),
		effect.Bind(nameLens.Set, F.Flow2(pathLens.Get, relativeIn[SearchAndReplaceDeps]())),
		effect.Bind(contentLens.Set, F.Flow3(pathLens.Get, readBytes[SearchAndReplaceDeps], effect.Map[SearchAndReplaceDeps](B.ToString))),
		effect.Let[SearchAndReplaceDeps](crlfLens.Set, F.Flow2(contentLens.Get, hasCRLF())),
		effect.ChainResultK[SearchAndReplaceDeps](F.Flow2(contentLens.Modify(toLF), edit())),
		effect.ChainFirst(writeEdited()),
		effect.Map[SearchAndReplaceDeps](replacedText),
	)
}

// searchAndReplaceDefinition describes search_and_replace to the model.
//
// It is wiring: a getter that builds the plain definition value for root.
func searchAndReplaceDefinition(root string) openai.FunctionDefinitionParam {
	return openai.FunctionDefinitionParam{
		Name:        SearchAndReplaceName,
		Description: openai.String("Request to replace every occurrence of a text or a regular expression in a file, optionally only within a range of lines. The call fails without changing the file when nothing matches. Prefer it over write_file for small changes to an existing file."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "File path (absolute, or relative to the workspace). The workspace is at " + root,
				},
				"search": map[string]any{
					"type":        "string",
					"description": "The text to search for, or a Go regular expression (RE2 syntax) with use_regex. It may span several lines; lines end with \\n.",
				},
				"replace": map[string]any{
					"type":        "string",
					"description": "The replacement. With use_regex, $1 and ${name} insert the groups of the match.",
				},
				"use_regex": map[string]any{
					"type":        "boolean",
					"description": "Treat search as a regular expression. Default: false. Use the flags (?m) for ^ and $ at line boundaries and (?s) for . to match line breaks.",
				},
				"ignore_case": map[string]any{
					"type":        "boolean",
					"description": "Ignore case when matching. Default: false.",
				},
				"start_line": map[string]any{
					"type":        "integer",
					"description": "First line (1-based) of the range to replace in. Default: the first line of the file.",
				},
				"end_line": map[string]any{
					"type":        "integer",
					"description": "Last line (1-based, inclusive) of the range to replace in. Default: the last line of the file.",
				},
			},
			"required": []string{"path", "search", "replace"},
		},
	}
}

// MakeSearchAndReplaceTool builds the search_and_replace [Tool] over the
// given dependencies.
func MakeSearchAndReplaceTool() Reader[SearchAndReplaceDeps, Tool] {
	return F.Pipe2(
		F.Flow2(SearchAndReplaceDeps.GetWorkspaceRoot, searchAndReplaceDefinition),
		reader.Map[SearchAndReplaceDeps](F.Curry2(MakeTool)),
		reader.Ap[Tool](F.Flip(SearchAndReplace())),
	)
}
