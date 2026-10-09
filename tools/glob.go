package tools

import (
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	ER "github.com/IBM/fp-go/v2/errors"
	F "github.com/IBM/fp-go/v2/function"
	J "github.com/IBM/fp-go/v2/json"
	N "github.com/IBM/fp-go/v2/number"
	"github.com/IBM/fp-go/v2/ord"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/readerresult"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	"github.com/bmatcuk/doublestar/v4"
	openai "github.com/openai/openai-go/v3"
)

// GlobName is the name under which the model calls the glob tool.
const GlobName = "glob"

// maxGlobResults caps the number of files a glob call returns, as Bob does.
const maxGlobResults = 100

// globTruncated is appended to a glob result that hit [maxGlobResults].
var globTruncated = S.Format[int]("\n\n(Showing the %d most recently modified files. Use a more specific pattern to see the others.)")(maxGlobResults)

// GlobArgs are the arguments of a glob call. Name and Entries are not part of
// the call; they are filled in while the call runs.
//
// fp-go:Lens
type GlobArgs struct {
	Pattern string      `json:"pattern"`
	Path    string      `json:"path"`
	Name    string      `json:"-"`
	Entries []FileEntry `json:"-"`
}

// The partial applications of the curried globListing, in the order of
// application. They are aliases because readerresult.Ap cannot infer them.
type (
	globFromEntries = func([]FileEntry) string
	globFromMatch   = func(P.Predicate[string]) globFromEntries
)

// anywhere lets a pattern without a slash match the base name at any depth,
// as the recordings show: "*.md" also finds "docs/README.md".
func anywhere() Endomorphism[string] {
	return P.Fold(S.Prepend("**/"), F.Identity[string])(S.Includes("/"))
}

// globMatcher compiles a glob pattern into a predicate over the
// slash-separated paths relative to the searched directory, with the syntax
// of [doublestar.Match]: "*" and "?" stay within a segment, "**" spans
// segments and "{a,b}" lists alternatives. A malformed pattern fails.
func globMatcher() result.Kleisli[string, P.Predicate[string]] {
	return F.Flow3(
		anywhere(),
		result.FromPredicate(doublestar.ValidatePattern, ER.OnSome[string]("glob pattern '%s' is malformed")),
		result.Map(F.Curry2(doublestar.MatchUnvalidated)),
	)
}

// newestFirst orders files by modification time, the newest first, and
// files with the same time by path.
func newestFirst() []ord.Ord[FileEntry] {
	modTimeLens := MakeFileEntryModTimeLens()
	pathLens := MakeFileEntryPathLens()

	return A.From(
		ord.Reverse(ord.ContraMap(modTimeLens.Get)(ord.OrdTime())),
		ord.ContraMap(pathLens.Get)(S.Ord),
	)
}

// capResults keeps the first [maxGlobResults] names and notes when it
// dropped some.
func capResults() func([]string) string {
	note := P.Fold(F.Constant1[[]string](""), F.Constant1[[]string](globTruncated))(F.Flow2(A.Size[string], N.MoreThan(maxGlobResults)))

	return F.Pipe1(
		F.Flow2(note, S.Append),
		reader.Ap[string](F.Flow2(A.Slice[string](0, maxGlobResults), joinLines())),
	)
}

// globListing renders the files below the directory named dir that match:
// newest first, relative to the workspace and capped at [maxGlobResults].
func globListing() func(string) globFromMatch {
	pathLens := MakeFileEntryPathLens()
	isDirLens := MakeFileEntryIsDirLens()

	// the files whose path matches
	keepMatching := F.Flow3(
		P.ContraMap(pathLens.Get),
		P.And(P.Not(isDirLens.Get)),
		A.Filter[FileEntry],
	)

	// the entries, newest first, relative to the workspace and capped
	render := F.Flow4(
		entryName,
		A.Map[FileEntry, string],
		reader.Map[[]FileEntry](capResults()),
		reader.Local[string](A.SortBy(newestFirst())),
	)

	return F.Flow2(
		render,
		F.Flip(F.Flow2(keepMatching, reader.Local[string, []FileEntry, []FileEntry])),
	)
}

// renderGlob compiles the pattern of a glob call and renders the matching
// entries.
func renderGlob() ReaderResult[GlobArgs, string] {
	nameLens := MakeGlobArgsNameLens()
	patternLens := MakeGlobArgsPatternLens()
	entriesLens := MakeGlobArgsEntriesLens()

	return F.Pipe3(
		readerresult.Of[GlobArgs](globListing()),
		readerresult.Ap[globFromMatch](F.Flow2(nameLens.Get, result.Of[string])),
		readerresult.Ap[globFromEntries](F.Flow2(patternLens.Get, globMatcher())),
		readerresult.Ap[string](F.Flow2(entriesLens.Get, result.Of[[]FileEntry])),
	)
}

// Glob runs a glob call: it decodes the JSON arguments, resolves the
// directory against the workspace (the workspace root when the path is
// empty), walks it and lists the files that match the pattern, relative to
// the workspace root, the most recently modified first, at most 100.
//
// Invalid arguments, a malformed pattern, a path outside the workspace and a
// directory that cannot be read fail the effect; [MakeToolCall] turns the
// failure into a tool message for the model.
func Glob() effect.Kleisli[DirectoryDeps, string, string] {
	pathLens := MakeGlobArgsPathLens()
	nameLens := MakeGlobArgsNameLens()
	entriesLens := MakeGlobArgsEntriesLens()

	orRoot := P.Fold(F.Identity[string], F.Constant1[string]("."))(S.IsEmpty)

	return F.Flow5(
		F.Flow3(S.ToBytes, J.Unmarshal[GlobArgs], effect.FromResult[DirectoryDeps, GlobArgs]),
		effect.Bind(pathLens.Set, F.Flow3(pathLens.Get, orRoot, resolveIn[DirectoryDeps]())),
		effect.Bind(nameLens.Set, F.Flow2(pathLens.Get, relativeIn[DirectoryDeps]())),
		effect.Bind(entriesLens.Set, F.Flow2(pathLens.Get, walkDir[DirectoryDeps](true))),
		effect.ChainResultK[DirectoryDeps](renderGlob()),
	)
}

// globDefinition describes glob to the model, following the recorded
// definition.
func globDefinition(root string) openai.FunctionDefinitionParam {
	return openai.FunctionDefinitionParam{
		Name:        GlobName,
		Description: openai.String(`Fast file pattern matching. Returns file paths matching a glob pattern, sorted by modification time. Use for finding files by name pattern (e.g. "**/*.ts", "src/**/*.test.ts"). Caps at 100 results.`),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": `Glob pattern to match files (e.g. "**/*.ts", "src/**/index.ts").`,
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Directory to search in (absolute, or relative to the workspace). The workspace is at " + root + ". Defaults to workspace root.",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

// MakeGlobTool builds the glob [Tool] over the given dependencies.
func MakeGlobTool() Reader[DirectoryDeps, Tool] {
	return F.Pipe2(
		F.Flow2(DirectoryDeps.GetWorkspaceRoot, globDefinition),
		reader.Map[DirectoryDeps](F.Curry2(MakeTool)),
		reader.Ap[Tool](F.Flip(Glob())),
	)
}
