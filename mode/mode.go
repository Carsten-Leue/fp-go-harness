package mode

import (
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	B "github.com/IBM/fp-go/v2/bytes"
	ER "github.com/IBM/fp-go/v2/errors"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/ord"
	"github.com/IBM/fp-go/v2/pair"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	"go.yaml.in/yaml/v3"
)

// IDs of the built-in modes.
const (
	// AgentID carries out tasks with every tool, including those that change
	// the workspace.
	AgentID = "agent"
	// PlanID investigates the workspace with the read-only tools and answers
	// with a plan.
	PlanID = "plan"
	// AskID answers questions with the read-only tools.
	AskID = "ask"
)

// ModeFilePattern matches the mode files of a directory, see [ReadModes].
const ModeFilePattern = "*.md"

// Mode is a mode of the agent: its role and the tools the model may call.
//
// A mode is read from a Markdown file: the YAML front matter holds every
// field but the prompt, which is the Markdown body, see [ParseMode].
//
// fp-go:Lens
type Mode struct {
	// ID names the mode on the command line and in its rules directory,
	// see [prompt.ModeRulesDir].
	ID string `yaml:"id"`
	// Name is the display name of the mode.
	Name string `yaml:"name"`
	// Description says in one sentence what the mode is for.
	Description string `yaml:"description"`
	// Order sorts the modes where they are offered, lowest first.
	Order int `yaml:"order"`
	// AllowedTools are the names of the tools the model may call.
	AllowedTools []string `yaml:"tools"`
	// Prompt is the role of the mode, the body of its role_definition
	// section.
	Prompt string `yaml:"-"`
}

//go:embed modes/*.md
var builtinFiles embed.FS

// builtin are the built-in modes, read once from the embedded files. They are
// part of the binary, so a broken one is a programming error that fails
// every test of the package.
var builtin = mustModes(fs.Sub(builtinFiles, "modes"))

// mustModes is a leaf at the boundary: it reads the built-in modes from their
// file system and panics on a broken file, like [regexp.MustCompile].
func mustModes(fsys fs.FS, err error) []Mode {
	var modes []Mode
	if err == nil {
		modes, err = result.Unwrap(ReadModes()(fsys)())
	}
	if err != nil {
		panic(fmt.Sprintf("built-in modes: %v", err))
	}
	return modes
}

// errMissingID is the error of a mode file without an id.
var errMissingID = errors.New("mode without id")

// decodeFrontMatter is a leaf around the YAML decoder: it decodes the front
// matter of a mode file into a [Mode]. Keys that aren't fields of [Mode] are
// errors, so a misspelt key in a mode file doesn't go unnoticed. An empty
// front matter has no document, which is [io.EOF].
func decodeFrontMatter(text string) Result[Mode] {
	var m Mode
	decoder := yaml.NewDecoder(strings.NewReader(text))
	decoder.KnownFields(true)
	// Decode runs before m is read: in one call expression, Go doesn't fix
	// the order of reading m and calling Decode.
	err := decoder.Decode(&m)
	return result.TryCatchError(m, err)
}

// decodeYAML decodes the front matter of a mode file; an empty one is the
// empty [Mode].
func decodeYAML() result.Kleisli[string, Mode] {
	return F.Flow2(
		decodeFrontMatter,
		result.CatchIs(io.EOF, F.Constant1[error](result.Of(Mode{}))),
	)
}

// modeOf builds a [Mode] from its decoded front matter, with the body,
// without surrounding blank lines, as its prompt. The front matter and the
// body are independent reads of the split file.
func modeOf() result.Kleisli[FrontMatter, Mode] {
	promptLens := MakeModePromptLens()

	return F.Pipe2(
		F.Flow3(pair.Tail[string, string], strings.TrimSpace, promptLens.Set),
		reader.Map[FrontMatter](result.Map[Mode, Mode]),
		reader.Ap[Result[Mode]](F.Flow2(pair.Head[string, string], decodeYAML())),
	)
}

// isTool tells whether a name is the name of a tool.
func isTool() P.Predicate[string] {
	return F.Bind1st(slices.Contains[[]string], tools.ToolNames())
}

// unknownTools are the tools a mode allows that don't exist.
func unknownTools() A.Kleisli[Mode, string] {
	allowedToolsLens := MakeModeAllowedToolsLens()

	return F.Flow2(
		allowedToolsLens.Get,
		A.Filter(P.Not(isTool())),
	)
}

// unknownToolsError is a leaf: one formatter over the ID and the unknown
// tools of a mode.
func unknownToolsError(m Mode) error {
	return fmt.Errorf("mode %q allows unknown tools %s", m.ID, strings.Join(unknownTools()(m), ", "))
}

// validateMode checks that a mode has an ID and allows only tools that exist.
func validateMode() result.Kleisli[Mode, Mode] {
	idLens := MakeModeIDLens()

	return F.Flow2(
		result.FromPredicate(F.Flow2(idLens.Get, S.IsNonEmpty), F.Constant1[Mode](errMissingID)),
		result.Chain(result.FromPredicate(F.Flow2(unknownTools(), A.IsEmpty[string]), unknownToolsError)),
	)
}

// ParseMode parses a mode file: Markdown with YAML front matter. The front
// matter holds the fields of the [Mode], the body is its prompt:
//
//	---
//	id: review
//	name: Review
//	description: Reviews the changes with the read-only tools.
//	order: 4
//	tools: [read_file, list_files, glob, grep]
//	---
//
//	You are a code reviewer running in Review mode.
//
// Unknown keys, a missing id and unknown tools are errors.
func ParseMode() result.Kleisli[[]byte, Mode] {
	return F.Flow4(
		B.ToString,
		SplitFrontMatter(),
		result.Chain(modeOf()),
		result.Chain(validateMode()),
	)
}

// inModeFile is a leaf: it adds the name of a mode file to an error.
func inModeFile(name string) Endomorphism[error] {
	return ER.OnError("mode file %q", name)
}

// parseModeFile reads and parses one mode file of a file system.
func parseModeFile() Reader[fs.FS, ioresult.Kleisli[string, Mode]] {
	return F.Flow2(
		F.Curry2(ioresult.Eitherize2(fs.ReadFile)),
		reader.Map[string](ioresult.ChainResultK(ParseMode())),
	)
}

// readModeFile reads and parses one mode file of a file system, with the
// name of the file in the error. The name is read twice: for the file and
// for the error.
func readModeFile() Reader[fs.FS, ioresult.Kleisli[string, Mode]] {
	return F.Flow3(
		parseModeFile(),
		reader.Ap[IOResult[Mode], string, IOResult[Mode]],
		reader.Read[ioresult.Kleisli[string, Mode]](F.Flow2(inModeFile, ioresult.MapLeft[Mode, error])),
	)
}

// modeFiles are the names of the mode files of a file system, sorted.
func modeFiles() ioresult.Kleisli[fs.FS, []string] {
	return F.Bind2nd(ioresult.Eitherize2(fs.Glob), ModeFilePattern)
}

// sortModes sorts modes by their [Mode.Order]; the sort is stable, so modes
// of the same order keep the order of their files.
func sortModes() Endomorphism[[]Mode] {
	orderLens := MakeModeOrderLens()

	return A.SortByKey(ord.FromStrictCompare[int](), orderLens.Get)
}

// ReadModes reads the modes of the mode files at the top of a file system
// (see [ModeFilePattern] and [ParseMode]), sorted by [Mode.Order]. The
// file system is read twice: for the names of the files and for each file.
func ReadModes() ioresult.Kleisli[fs.FS, []Mode] {
	return F.Pipe3(
		readModeFile(),
		reader.Map[fs.FS](F.Flow2(ioresult.TraverseArray[string, Mode], ioresult.Chain[[]string, []Mode])),
		reader.Ap[IOResult[[]Mode]](modeFiles()),
		reader.Map[fs.FS](ioresult.Map(sortModes())),
	)
}

// Modes is the table of the built-in modes, in the order they are offered:
// Agent with every tool, Plan and Ask with the read-only tools. They are
// read from the files in modes/.
func Modes() []Mode {
	return builtin
}
