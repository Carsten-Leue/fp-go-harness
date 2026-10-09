package tools

import (
	"bytes"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	A "github.com/IBM/fp-go/v2/array"
	B "github.com/IBM/fp-go/v2/bytes"
	"github.com/IBM/fp-go/v2/effect"
	ER "github.com/IBM/fp-go/v2/errors"
	"github.com/IBM/fp-go/v2/file"
	F "github.com/IBM/fp-go/v2/function"
	J "github.com/IBM/fp-go/v2/json"
	"github.com/IBM/fp-go/v2/lazy"
	M "github.com/IBM/fp-go/v2/monoid"
	N "github.com/IBM/fp-go/v2/number"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/ord"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/readerresult"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	gitignore "github.com/go-git/go-git/v5/plumbing/format/gitignore"
	openai "github.com/openai/openai-go/v3"
)

// GrepName is the name under which the model calls the grep tool.
const GrepName = "grep"

// maxGrepResults caps the number of matches, or of files with
// files_with_matches, that a grep call shows, as Bob does.
const maxGrepResults = 100

// gitignoreFile is the name of the files whose patterns grep skips.
const gitignoreFile = ".gitignore"

// noMatches is the text of a search without matches, as recorded.
const noMatches = "No files found: No matches"

var (
	matchesTruncated = S.Format[int]("\n\n(Showing the first %d matches. Use a more specific pattern, path or include to see the others.)")(maxGrepResults)
	filesTruncated   = S.Format[int]("\n\n(Showing the first %d files. Use a more specific pattern, path or include to see the others.)")(maxGrepResults)
)

// GrepDeps is what grep needs: the workspace its paths are resolved
// against, the directory listings and the file system it reads from.
type GrepDeps interface {
	WorkspaceDeps
	WalkDeps
	FileDeps
}

type grepDeps struct {
	WorkspaceDeps
	WalkDeps
	FileDeps
}

func MakeGrepDeps(ws WorkspaceDeps, w WalkDeps, f FileDeps) GrepDeps {
	return &grepDeps{ws, w, f}
}

func AsGrepDeps[R GrepDeps](r R) GrepDeps {
	return r
}

// GrepScope is the absolute directory a grep call searches and the entries
// below it; for a file argument it is the parent directory and the file.
//
// fp-go:Lens
type GrepScope struct {
	Dir     string
	Entries []FileEntry
}

// GrepLine is a matching line. File is relative to the workspace, Number is
// 1-based.
//
// fp-go:Lens
type GrepLine struct {
	File   string
	Number int
	Text   string
}

// IgnoreFile is a file with gitignore patterns: its absolute path and the
// workspace-relative path segments of the directory its patterns apply to.
//
// fp-go:Lens
type IgnoreFile struct {
	Path   string
	Domain []string
}

// GrepArgs are the arguments of a grep call. The fields without a JSON name
// are not part of the call; they are filled in while the call runs.
//
// fp-go:Lens
type GrepArgs struct {
	Pattern          string              `json:"pattern"`
	Path             string              `json:"path"`
	Include          string              `json:"include"`
	IgnoreCase       bool                `json:"ignore_case"`
	InvertMatch      bool                `json:"invert_match"`
	WordRegexp       bool                `json:"word_regexp"`
	FilesWithMatches bool                `json:"files_with_matches"`
	Regexp           *regexp.Regexp      `json:"-"`
	Scope            GrepScope           `json:"-"`
	Name             string              `json:"-"`
	Ignores          []gitignore.Pattern `json:"-"`
	Candidates       []FileEntry         `json:"-"`
	Lines            []GrepLine          `json:"-"`
}

func MakeGrepScope(dir string, entries []FileEntry) GrepScope {
	return GrepScope{Dir: dir, Entries: entries}
}

// whenSet applies on when the flag is set, and leaves the value alone otherwise.
func whenSet[T any](on Endomorphism[T]) func(bool) Endomorphism[T] {
	return P.Fold(F.Constant1[bool](F.Identity[T]), F.Constant1[bool](on))(F.Identity[bool])
}

// wordBoundaries makes a pattern match whole words only.
func wordBoundaries() Endomorphism[string] {
	return F.Flow2(S.Prepend(`\b(?:`), S.Append(`)\b`))
}

// compilePattern compiles the pattern of a grep call: whole words only when
// word_regexp is set, case-insensitive when ignore_case is set.
func compilePattern() ReaderResult[GrepArgs, *regexp.Regexp] {
	patternLens := MakeGrepArgsPatternLens()
	ignoreCaseLens := MakeGrepArgsIgnoreCaseLens()
	wordRegexpLens := MakeGrepArgsWordRegexpLens()

	return F.Pipe4(
		F.Flow2(wordRegexpLens.Get, whenSet(wordBoundaries())),
		reader.Map[GrepArgs](reader.Compose[string, string, string]),
		reader.Ap[Endomorphism[string]](F.Flow2(ignoreCaseLens.Get, whenSet(S.Prepend("(?i)")))),
		reader.Ap[string](patternLens.Get),
		reader.Map[GrepArgs](F.Flow2(
			result.Eitherize1(regexp.Compile),
			result.MapLeft[*regexp.Regexp](ER.OnError("grep pattern is not a valid regular expression")),
		)),
	)
}

// matchString is a leaf: the predicate of a compiled pattern.
func matchString(re *regexp.Regexp) P.Predicate[string] {
	return re.MatchString
}

// lineMatcher tests a line against the pattern of a grep call; with
// invert_match it keeps the lines the pattern does not match.
func lineMatcher() Reader[GrepArgs, P.Predicate[string]] {
	regexpLens := MakeGrepArgsRegexpLens()
	invertMatchLens := MakeGrepArgsInvertMatchLens()

	return F.Pipe1(
		F.Flow2(invertMatchLens.Get, whenSet(P.Not[string])),
		reader.Ap[P.Predicate[string]](F.Flow2(regexpLens.Get, matchString)),
	)
}

// includeMatcher compiles the include glob; an empty one includes every file.
func includeMatcher() result.Kleisli[string, P.Predicate[string]] {
	return P.Fold(globMatcher(), F.Constant1[string](result.Of(P.Always[string]())))(S.IsEmpty)
}

// isFileNamed tests for the file, not directory, at the slash-separated path.
func isFileNamed() func(string) P.Predicate[FileEntry] {
	pathLens := MakeFileEntryPathLens()
	isDirLens := MakeFileEntryIsDirLens()

	return F.Flow3(
		S.Equals,
		P.ContraMap(pathLens.Get),
		P.And(P.Not(isDirLens.Get)),
	)
}

// isGitignore tests for a .gitignore file at any depth.
func isGitignore() P.Predicate[FileEntry] {
	pathLens := MakeFileEntryPathLens()
	isDirLens := MakeFileEntryIsDirLens()

	return F.Pipe1(
		P.ContraMap(F.Flow2(pathLens.Get, path.Base))(S.Equals(gitignoreFile)),
		P.And(P.Not(isDirLens.Get)),
	)
}

// byDepth orders entries by the number of directories above them, so that
// the patterns of a nested .gitignore come after, and win over, the outer ones.
func byDepth() []ord.Ord[FileEntry] {
	pathLens := MakeFileEntryPathLens()

	return A.Of(ord.ContraMap(F.Flow2(pathLens.Get, F.Bind2nd(strings.Count, "/")))(ord.FromStrictCompare[int]()))
}

// absoluteEntry is the absolute path of an entry below dir.
func absoluteEntry(dir string) func(FileEntry) string {
	pathLens := MakeFileEntryPathLens()

	return F.Flow3(pathLens.Get, filepath.FromSlash, F.Flip(file.Join)(dir))
}

// segments splits a relative path into its segments; "." has none.
func segments() func(string) []string {
	return F.Flow2(
		filepath.ToSlash,
		P.Fold(F.Bind2nd(strings.Split, "/"), F.Constant1[string]([]string(nil)))(S.Equals(".")),
	)
}

// ignoreDomain is the directory of a .gitignore entry as path segments
// relative to the walked directory.
func ignoreDomain() func(FileEntry) []string {
	pathLens := MakeFileEntryPathLens()

	return F.Flow3(pathLens.Get, path.Dir, segments())
}

// isPatternLine tests for a line of a .gitignore file that is neither blank
// nor a comment.
func isPatternLine() P.Predicate[string] {
	return P.Not(F.Pipe1(F.Flow2(strings.TrimSpace, S.IsEmpty), P.Or(S.HasPrefix("#"))))
}

// parseIgnore parses the content of a .gitignore file into the patterns for
// the domain.
func parseIgnore(domain []string) func([]byte) []gitignore.Pattern {
	return F.Flow3(
		splitLines(),
		A.Filter(isPatternLine()),
		A.Map(F.Bind2nd(gitignore.ParsePattern, domain)),
	)
}

// skipUnreadable treats a file that cannot be read as empty.
func skipUnreadable() effect.Operator[GrepDeps, []byte, []byte] {
	return effect.Alt(lazy.Of(effect.Of[GrepDeps](B.Empty())))
}

// readIgnore reads the patterns of an ignore file: its path gives the content,
// its domain the parser. A missing file has none.
func readIgnore() effect.Kleisli[GrepDeps, IgnoreFile, []gitignore.Pattern] {
	pathLens := MakeIgnoreFilePathLens()
	domainLens := MakeIgnoreFileDomainLens()

	return F.Pipe1(
		F.Flow3(domainLens.Get, parseIgnore, effect.Map[GrepDeps, []byte, []gitignore.Pattern]),
		reader.Ap[Effect[GrepDeps, []gitignore.Pattern]](F.Flow3(pathLens.Get, readBytes[GrepDeps], skipUnreadable())),
	)
}

// makeIgnoreFile is a leaf: the constructor of an [IgnoreFile].
func makeIgnoreFile(path string, domain []string) IgnoreFile {
	return IgnoreFile{Path: path, Domain: domain}
}

// excludeFile is .git/info/exclude of the repository at the workspace root;
// its patterns apply to the whole workspace.
func excludeFile(root string) IgnoreFile {
	return IgnoreFile{Path: filepath.Join(root, gitDir, "info", "exclude")}
}

// inits lists the prefixes of segs, from the empty one up to segs itself.
func inits() func([]string) [][]string {
	return F.Pipe1(
		F.Flow3(A.Size[string], N.Inc[int], F.Curry2(A.MakeBy[func(int) []string, []string])),
		reader.Ap[[][]string](F.Flip(F.Bind1st(A.Slice[string], 0))),
	)
}

// ancestorIgnore is the .gitignore file of the directory below root with
// the given workspace-relative segments.
func ancestorIgnore() func(string) func([]string) IgnoreFile {
	return F.Flow3(
		F.Flip(file.Join),
		reader.Local[string](F.Flow2(S.Join("/"), filepath.FromSlash)),
		reader.Chain(F.Flow2(file.Join(gitignoreFile), F.Curry2(makeIgnoreFile))),
	)
}

// ancestorFiles lists the .gitignore files of the workspace root and of
// every directory down to the one with the workspace-relative segments it is
// given, the outer ones first.
func ancestorFiles() func(string) func([]string) []IgnoreFile {
	return F.Flow2(
		ancestorIgnore(),
		F.Flow2(A.Map[[]string, IgnoreFile], reader.Local[[]IgnoreFile](inits())),
	)
}

// nestedIgnores keeps the .gitignore files strictly below the walked
// directory, the outer ones first.
func nestedIgnores() func([]FileEntry) []FileEntry {
	return F.Flow2(
		A.Filter(F.Pipe1(isGitignore(), P.And(P.Not(isFileNamed()(gitignoreFile))))),
		A.SortBy(byDepth()),
	)
}

// nestedIgnore is the [IgnoreFile] of a .gitignore entry below the searched
// directory dir, whose workspace-relative segments are prefix.
func nestedIgnore() func(string) func([]string) func(FileEntry) IgnoreFile {
	domainUnder := F.Flip(F.Flow2(ignoreDomain(), A.Concat[string]))

	return F.Flow2(
		F.Flow2(absoluteEntry, reader.Map[FileEntry](F.Curry2(makeIgnoreFile))),
		F.Flip(F.Flow2(domainUnder, reader.Ap[IgnoreFile, FileEntry, []string])),
	)
}

// nestedFiles lists the .gitignore files below the searched directory dir,
// whose workspace-relative segments are prefix, the outer ones first. The
// .gitignore of dir itself is one of the [ancestorFiles].
func nestedFiles() func(string) func([]string) func([]FileEntry) []IgnoreFile {
	return F.Flow2(
		nestedIgnore(),
		reader.Map[[]string](F.Flow2(A.Map[FileEntry, IgnoreFile], reader.Local[[]IgnoreFile](nestedIgnores()))),
	)
}

// ignoreFiles lists the files whose patterns apply to a grep call, in the
// order of increasing priority, as gitignore.NewMatcher expects them:
// .git/info/exclude, the .gitignore files from the workspace root down to
// the searched directory, then the ones below it.
func ignoreFiles(root string) func(GrepArgs) []IgnoreFile {
	nameLens := MakeGrepArgsNameLens()
	scopeLens := MakeGrepArgsScopeLens()
	dirLens := MakeGrepScopeDirLens()
	entriesLens := MakeGrepScopeEntriesLens()

	prefix := F.Flow2(nameLens.Get, segments())

	nested := F.Pipe2(
		F.Flow3(scopeLens.Get, dirLens.Get, nestedFiles()),
		reader.Ap[func([]FileEntry) []IgnoreFile](prefix),
		reader.Ap[[]IgnoreFile](F.Flow2(scopeLens.Get, entriesLens.Get)),
	)

	return M.ConcatAll(reader.ApplicativeMonoid[GrepArgs](A.Monoid[IgnoreFile]()))(A.From(
		F.Constant1[GrepArgs](A.Of(excludeFile(root))),
		F.Flow2(prefix, ancestorFiles()(root)),
		nested,
	))
}

// loadIgnores reads the patterns of every file in [ignoreFiles].
func loadIgnores() effect.Kleisli[GrepDeps, GrepArgs, []gitignore.Pattern] {
	root := effect.Asks(GrepDeps.GetWorkspaceRoot)

	return F.Flow3(
		F.Flip(ignoreFiles),
		reader.Map[string](F.Flow2(
			effect.TraverseArray(readIgnore()),
			effect.Map[GrepDeps](A.Flatten[gitignore.Pattern]),
		)),
		F.Flow2(
			effect.Chain[GrepDeps, string, []gitignore.Pattern],
			reader.Read[Effect[GrepDeps, []gitignore.Pattern]](root),
		),
	)
}

// notIgnored is a leaf over the gitignore matcher: it keeps the entries of
// the directory with the workspace-relative segments prefix that none of
// the patterns exclude.
func notIgnored(prefix []string, ps []gitignore.Pattern) P.Predicate[FileEntry] {
	m := gitignore.NewMatcher(ps)
	return func(e FileEntry) bool {
		return !m.Match(A.Concat(strings.Split(e.Path, "/"))(prefix), e.IsDir)
	}
}

// searchScope finds what the absolute path p names. A file is searched alone,
// in the scope of its parent directory; anything else is walked as a
// directory. A parent that cannot be listed leaves the decision to the walk.
func searchScope(p string) Effect[GrepDeps, GrepScope] {
	dir := filepath.Dir(p)

	walkScope := F.Pipe1(
		walkDir[GrepDeps](true)(p),
		effect.Map[GrepDeps](F.Curry2(MakeGrepScope)(p)),
	)
	fileScope := F.Flow2(F.Curry2(MakeGrepScope)(dir), effect.Of[GrepDeps, GrepScope])

	return F.Pipe3(
		walkDir[GrepDeps](false)(dir),
		effect.Alt(lazy.Of(effect.Of[GrepDeps](A.Empty[FileEntry]()))),
		effect.Map[GrepDeps](A.Filter(isFileNamed()(filepath.Base(p)))),
		effect.Chain(P.Fold(F.Constant1[[]FileEntry](walkScope), fileScope)(A.IsNonEmpty[FileEntry])),
	)
}

// keepCandidate selects the files to search: the ones the include predicate
// accepts for their path and none of the gitignore patterns exclude, given
// the workspace-relative segments of the searched directory.
func keepCandidate(include P.Predicate[string]) func([]string) func([]gitignore.Pattern) P.Predicate[FileEntry] {
	pathLens := MakeFileEntryPathLens()
	isDirLens := MakeFileEntryIsDirLens()

	base := F.Pipe1(
		P.ContraMap(pathLens.Get)(include),
		P.And(P.Not(isDirLens.Get)),
	)

	return F.Flow2(
		F.Curry2(notIgnored),
		reader.Map[[]gitignore.Pattern](F.Flow2(
			P.And[FileEntry],
			reader.Read[P.Predicate[FileEntry]](base),
		)),
	)
}

// selectCandidates keeps the files to search, the most recently modified first.
func selectCandidates(include P.Predicate[string]) func([]string) func([]gitignore.Pattern) Endomorphism[[]FileEntry] {
	return F.Flow2(
		keepCandidate(include),
		reader.Map[[]gitignore.Pattern](F.Flow2(
			A.Filter[FileEntry],
			reader.Map[[]FileEntry](A.SortBy(newestFirst())),
		)),
	)
}

// The partial applications of the curried selectCandidates, in the order of
// application. They are aliases because readerresult.Ap cannot infer them.
type (
	candidatesFromIgnores = func([]gitignore.Pattern) Endomorphism[[]FileEntry]
)

// candidates selects the files of the scope that a grep call searches. A
// malformed include pattern fails.
func candidates() ReaderResult[GrepArgs, []FileEntry] {
	includeLens := MakeGrepArgsIncludeLens()
	nameLens := MakeGrepArgsNameLens()
	ignoresLens := MakeGrepArgsIgnoresLens()
	scopeLens := MakeGrepArgsScopeLens()
	entriesLens := MakeGrepScopeEntriesLens()

	return F.Pipe3(
		F.Flow3(includeLens.Get, includeMatcher(), result.Map(selectCandidates)),
		readerresult.Ap[candidatesFromIgnores](F.Flow3(nameLens.Get, segments(), result.Of[[]string])),
		readerresult.Ap[Endomorphism[[]FileEntry]](F.Flow2(ignoresLens.Get, result.Of[[]gitignore.Pattern])),
		readerresult.Ap[[]FileEntry](F.Flow3(scopeLens.Get, entriesLens.Get, result.Of[[]FileEntry])),
	)
}

// isText tests for content without NUL bytes; grep skips binary files.
func isText() P.Predicate[[]byte] {
	return P.Not(F.Bind2nd(bytes.Contains, []byte{0}))
}

// hasLines tests for non-empty text content; empty content has no lines,
// not a single empty one.
func hasLines() P.Predicate[[]byte] {
	return F.Pipe1(isText(), P.And(F.Flow2(B.Size, N.MoreThan(0))))
}

// matchingLine is a leaf: it keeps the line with 0-based index i of the file
// name when match accepts it.
func matchingLine(match P.Predicate[string], name string) func(int, string) Option[GrepLine] {
	textLens := MakeGrepLineTextLens()

	return func(i int, line string) Option[GrepLine] {
		return F.Pipe1(
			GrepLine{File: name, Number: i + 1, Text: line},
			option.FromPredicate(F.Flow2(textLens.Get, match)),
		)
	}
}

// grepContent finds the lines of the content of the file name that match
// accepts. A final line break ends the last line, it does not start an
// empty one. Empty and binary content have none.
func grepContent(match P.Predicate[string]) func(string) func([]byte) []GrepLine {
	return func(name string) func([]byte) []GrepLine {
		return P.Fold(
			F.Constant1[[]byte]([]GrepLine(nil)),
			F.Flow3(
				F.Bind2nd(bytes.TrimSuffix, []byte("\n")),
				splitLines(),
				A.FilterMapWithIndex(matchingLine(match, name)),
			),
		)(hasLines())
	}
}

// searchFile is a leaf: the entry gives both the file to read, below the
// absolute directory dir, and its name relative to the workspace, below the
// workspace-relative name of dir. A file that cannot be read has no matches.
func searchFile(match P.Predicate[string], dir, name string) effect.Kleisli[GrepDeps, FileEntry, []GrepLine] {
	return func(e FileEntry) Effect[GrepDeps, []GrepLine] {
		return F.Pipe2(
			readBytes[GrepDeps](absoluteEntry(dir)(e)),
			skipUnreadable(),
			effect.Map[GrepDeps](grepContent(match)(entryName(name)(e))),
		)
	}
}

// The partial applications of the curried searchFile, in the order of
// application. They are aliases because reader.Ap cannot infer them.
type (
	searchKleisli = effect.Kleisli[GrepDeps, FileEntry, []GrepLine]
	searchInName  = func(string) searchKleisli
)

// searchLines searches the candidates of a grep call, in their order.
func searchLines() Reader[GrepArgs, Effect[GrepDeps, []GrepLine]] {
	scopeLens := MakeGrepArgsScopeLens()
	dirLens := MakeGrepScopeDirLens()
	nameLens := MakeGrepArgsNameLens()
	candidatesLens := MakeGrepArgsCandidatesLens()

	return F.Pipe5(
		F.Flow2(lineMatcher(), F.Curry3(searchFile)),
		reader.Ap[searchInName](F.Flow2(scopeLens.Get, dirLens.Get)),
		reader.Ap[searchKleisli](nameLens.Get),
		reader.Map[GrepArgs](effect.TraverseArray[GrepDeps, FileEntry, []GrepLine]),
		reader.Ap[Effect[GrepDeps, [][]GrepLine]](candidatesLens.Get),
		reader.Map[GrepArgs](effect.Map[GrepDeps](A.Flatten[GrepLine])),
	)
}

// lineText is a leaf: one formatter over the line number and the text.
func lineText(l GrepLine) string {
	return fmt.Sprintf("  Line %d: %s", l.Number, l.Text)
}

// fileBlock is a leaf: it renders the lines of one file under its name.
func fileBlock(lines []GrepLine) func(string) string {
	fileLens := MakeGrepLineFileLens()

	return func(name string) string {
		return F.Pipe3(
			lines,
			A.Filter(P.ContraMap(fileLens.Get)(S.Equals(name))),
			A.Map(lineText),
			F.Flow2(S.Join("\n"), S.Prepend(name+":\n")),
		)
	}
}

// groupByFile renders matching lines grouped by file, the files in the order
// of their first line.
func groupByFile() func([]GrepLine) string {
	fileLens := MakeGrepLineFileLens()

	return F.Pipe2(
		F.Flow2(fileBlock, A.Map[string, string]),
		reader.Ap[[]string](F.Flow2(A.Map(fileLens.Get), A.Uniq(F.Identity[string]))),
		reader.Map[[]GrepLine](S.Join("\n\n")),
	)
}

// capped renders the first [maxGrepResults] items under a header with the
// total count, adds the note when it dropped some, and yields "No files
// found: No matches" for none.
func capped[T any](header, note string, body func([]T) string) func([]T) string {
	size := A.Size[T]

	withNote := F.Flow2(
		P.Fold(F.Constant1[[]T](""), F.Constant1[[]T](note))(F.Flow2(size, N.MoreThan(maxGrepResults))),
		S.Append,
	)
	withHeader := F.Flow3(size, S.Format[int](header), S.Prepend)

	render := F.Pipe1(
		withNote,
		reader.Ap[string](F.Pipe1(
			withHeader,
			reader.Ap[string](F.Flow2(A.Slice[T](0, maxGrepResults), body)),
		)),
	)

	return P.Fold(render, F.Constant1[[]T](noMatches))(A.IsEmpty[T])
}

// renderMatches renders matching lines in the format of the recordings:
// "Found N matches\n<file>:\n  Line 7: ...".
func renderMatches() func([]GrepLine) string {
	return capped("Found %d matches\n", matchesTruncated, groupByFile())
}

// renderFiles renders the files with matches in the format of the
// recordings: "Found N files with matches\n<file>\n...".
func renderFiles() func([]GrepLine) string {
	fileLens := MakeGrepLineFileLens()

	return F.Flow3(
		A.Map(fileLens.Get),
		A.Uniq(F.Identity[string]),
		capped("Found %d files with matches\n", filesTruncated, S.Join("\n")),
	)
}

// renderGrep renders the matching lines of a grep call, or only the files
// with files_with_matches.
func renderGrep() Reader[GrepArgs, string] {
	filesLens := MakeGrepArgsFilesWithMatchesLens()
	linesLens := MakeGrepArgsLinesLens()

	return F.Pipe1(
		F.Flow2(filesLens.Get, P.Fold(F.Constant1[bool](renderMatches()), F.Constant1[bool](renderFiles()))(F.Identity[bool])),
		reader.Ap[string](linesLens.Get),
	)
}

// Grep runs a grep call: it decodes the JSON arguments, compiles the
// pattern, resolves the path against the workspace (the workspace root when
// it is empty) and searches the file or the files below the directory.
// It skips .git, binary files and what git would ignore: the patterns of
// .git/info/exclude at the workspace root, of the .gitignore files from the
// workspace root down to the searched directory and of the ones below it.
// Include narrows the files by a glob pattern, word_regexp matches whole
// words only, invert_match keeps the lines that do not match. The matches
// are grouped by file, the most recently modified file first, at most 100;
// files_with_matches lists only the files.
//
// Invalid arguments, a malformed pattern, a path outside the workspace and a
// path that cannot be read fail the effect; [MakeToolCall] turns the failure
// into a tool message for the model.
func Grep() effect.Kleisli[GrepDeps, string, string] {
	regexpLens := MakeGrepArgsRegexpLens()
	pathLens := MakeGrepArgsPathLens()
	scopeLens := MakeGrepArgsScopeLens()
	dirLens := MakeGrepScopeDirLens()
	nameLens := MakeGrepArgsNameLens()
	ignoresLens := MakeGrepArgsIgnoresLens()
	candidatesLens := MakeGrepArgsCandidatesLens()
	linesLens := MakeGrepArgsLinesLens()

	orRoot := P.Fold(F.Identity[string], F.Constant1[string]("."))(S.IsEmpty)

	return F.Flow9(
		F.Flow3(S.ToBytes, J.Unmarshal[GrepArgs], effect.FromResult[GrepDeps, GrepArgs]),
		effect.Bind(regexpLens.Set, F.Flow2(compilePattern(), effect.FromResult[GrepDeps, *regexp.Regexp])),
		effect.Bind(pathLens.Set, F.Flow3(pathLens.Get, orRoot, resolveIn[GrepDeps]())),
		effect.Bind(scopeLens.Set, F.Flow2(pathLens.Get, searchScope)),
		effect.Bind(nameLens.Set, F.Flow3(scopeLens.Get, dirLens.Get, relativeIn[GrepDeps]())),
		effect.Bind(ignoresLens.Set, loadIgnores()),
		effect.Bind(candidatesLens.Set, F.Flow2(candidates(), effect.FromResult[GrepDeps, []FileEntry])),
		effect.Bind(linesLens.Set, searchLines()),
		effect.Map[GrepDeps](renderGrep()),
	)
}

// grepDefinition describes grep to the model, following the recorded
// definition.
func grepDefinition(root string) openai.FunctionDefinitionParam {
	return openai.FunctionDefinitionParam{
		Name:        GrepName,
		Description: openai.String("Fast content search using regex patterns. Returns file paths with line numbers and matching lines, sorted by modification time. Use for quickly finding which files contain a pattern. Caps at 100 matches. Results are grouped by file with line numbers."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Regex pattern to search for in file contents (uses Go regexp syntax).",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "File or directory path to search in (absolute, or relative to the workspace). The workspace is at " + root + ". Accepts single path only. Defaults to workspace root.",
				},
				"include": map[string]any{
					"type":        "string",
					"description": `Glob pattern to filter files (e.g. "*.ts", "*.{ts,tsx}").`,
				},
				"ignore_case": map[string]any{
					"type":        "boolean",
					"description": "Case-insensitive matching (-i).",
				},
				"invert_match": map[string]any{
					"type":        "boolean",
					"description": "Return lines that do NOT match the pattern (-v).",
				},
				"word_regexp": map[string]any{
					"type":        "boolean",
					"description": "Only match whole words — pattern must be surrounded by word boundaries (-w).",
				},
				"files_with_matches": map[string]any{
					"type":        "boolean",
					"description": "Return only file paths instead of individual matching lines (-l). Useful for checking which files contain a pattern.",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

// MakeGrepTool builds the grep [Tool] over the given dependencies.
func MakeGrepTool() Reader[GrepDeps, Tool] {
	return F.Pipe2(
		F.Flow2(GrepDeps.GetWorkspaceRoot, grepDefinition),
		reader.Map[GrepDeps](F.Curry2(MakeTool)),
		reader.Ap[Tool](F.Flip(Grep())),
	)
}
