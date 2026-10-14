package prompt

import (
	"errors"
	"io/fs"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/file"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/option"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	S "github.com/IBM/fp-go/v2/string"
)

// Tags of the project_rules section and the sections nested in it, as in the
// recorded system prompts.
const (
	ProjectRulesTag = "project_rules"
	AgentsMdTag     = "agents_md"
	RuleTag         = "rule"
)

// AgentsMdFile is the file in the workspace root that holds the project rules.
const AgentsMdFile = "AGENTS.md"

// RulesDir is the directory in the workspace root whose files are further
// project rules, one rule per file.
const RulesDir = ".bob/rules"

// ProjectRulesDeps gives the project_rules section the workspace it reads the
// rules from, read access to its files and its directory listings.
type ProjectRulesDeps interface {
	tools.WorkspaceDeps
	tools.FileDeps
	tools.WalkDeps
}

type projectRulesDeps struct {
	tools.WorkspaceDeps
	tools.FileDeps
	tools.WalkDeps
}

// MakeProjectRulesDeps is wiring: it builds [ProjectRulesDeps] from its parts.
func MakeProjectRulesDeps(ws tools.WorkspaceDeps, f tools.FileDeps, w tools.WalkDeps) ProjectRulesDeps {
	return &projectRulesDeps{ws, f, w}
}

// MakeDefaultProjectRulesDeps is wiring: it builds [ProjectRulesDeps] over
// the operating system's file system.
func MakeDefaultProjectRulesDeps(ws tools.WorkspaceDeps) ProjectRulesDeps {
	return MakeProjectRulesDeps(ws, tools.MakeDefaultFileDeps(), tools.MakeDefaultWalkDeps())
}

// AsProjectRulesDeps is wiring: it narrows any wider dependencies to
// [ProjectRulesDeps], for use with [effect.Local].
func AsProjectRulesDeps[R ProjectRulesDeps](r R) ProjectRulesDeps {
	return r
}

// isNotExist is a leaf: it tests for an error of a missing file or directory.
func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

// missingAsNone turns a read of a missing file or directory into None, so the
// rules are optional. Every other error fails the read.
func missingAsNone[T any]() func(ioresult.IOResult[T]) ioresult.IOResult[Option[T]] {
	return F.Flow2(
		ioresult.Map(option.Some[T]),
		ioresult.OrElse(P.Fold(
			ioresult.Left[Option[T]],
			F.Constant1[error](ioresult.Of(option.None[T]())),
		)(isNotExist)),
	)
}

// bytesToString is a leaf: the conversion as a function.
func bytesToString(b []byte) string {
	return string(b)
}

// inWorkspace reads the workspace root and joins name to it.
func inWorkspace(name string) Effect[ProjectRulesDeps, string] {
	return effect.Asks(F.Flow2(
		ProjectRulesDeps.GetWorkspaceRoot,
		file.Join(name),
	))
}

// readOptional lifts a file system access behind a getter, a function of the
// path, into an effect that gives None for a missing path. Flipping the getter
// turns the path into the reader of the dependencies.
func readOptional[T any](get Reader[ProjectRulesDeps, ioresult.Kleisli[string, T]]) effect.Kleisli[ProjectRulesDeps, string, Option[T]] {
	return F.Flow3(
		F.Flip(get),
		effect.Asks[ProjectRulesDeps, ioresult.IOResult[T]],
		effect.ChainIOResultK[ProjectRulesDeps](missingAsNone[T]()),
	)
}

// readRule reads the rule in the file at an absolute path. A missing or empty
// file gives None; the text has LF line endings and no trailing line breaks.
func readRule() effect.Kleisli[ProjectRulesDeps, string, Option[string]] {
	return F.Flow2(
		readOptional(ProjectRulesDeps.GetReadFile),
		effect.Map[ProjectRulesDeps](option.Chain(F.Flow3(
			bytesToString,
			normalizeBody(),
			option.FromPredicate(S.IsNonEmpty),
		))),
	)
}

// The non-recursive walker, and the absolute paths of the files of a listing.
type (
	walkFiles   = ioresult.Kleisli[string, []tools.FileEntry]
	listedFiles = func([]tools.FileEntry) []string
)

// listFiles lists the files directly in an absolute directory, relative to
// it. A missing directory has no files, subdirectories are skipped.
func listFiles() effect.Kleisli[ProjectRulesDeps, string, []tools.FileEntry] {
	return F.Flow2(
		readOptional(F.Flow2(
			ProjectRulesDeps.GetWalkDir,
			reader.Read[walkFiles](false),
		)),
		effect.Map[ProjectRulesDeps](F.Flow2(
			option.GetOrElse(A.Empty[tools.FileEntry]),
			A.Filter(P.Not(tools.MakeFileEntryIsDirLens().Get)),
		)),
	)
}

// filesOf reads the directory and joins it to the paths of its listing.
func filesOf() Reader[string, listedFiles] {
	return F.Pipe1(
		F.Flip(file.Join),
		reader.Map[string](F.Flow2(
			reader.Local[string](tools.MakeFileEntryPathLens().Get),
			A.Map[tools.FileEntry, string],
		)),
	)
}

// ruleFiles lists the absolute paths of the files directly in a directory, in
// the order of the walker (lexical for [tools.WalkFS]). A missing directory
// has no files. The directory is read twice, by [listFiles] and by [filesOf].
func ruleFiles() effect.Kleisli[ProjectRulesDeps, string, []string] {
	return F.Pipe2(
		filesOf(),
		reader.Map[string](effect.Map[ProjectRulesDeps, []tools.FileEntry, []string]),
		reader.Ap[Effect[ProjectRulesDeps, []string]](listFiles()),
	)
}

// ruleSection wraps the text of one rule.
func ruleSection() func(string) Section {
	return F.Bind1st(MakeSection, RuleTag)
}

// agentsMdSection wraps the rule from AGENTS.md as <agents_md><rule>…
func agentsMdSection() func(string) Section {
	return F.Flow3(
		ruleSection(),
		RenderSection(),
		F.Bind1st(MakeSection, AgentsMdTag),
	)
}

// agentsMd reads AGENTS.md from the workspace root: no section when it is
// missing or empty, else the agents_md section.
func agentsMd() Effect[ProjectRulesDeps, []Section] {
	return F.Pipe2(
		inWorkspace(AgentsMdFile),
		effect.Chain(readRule()),
		effect.Map[ProjectRulesDeps](option.Fold(
			A.Empty[Section],
			F.Flow2(agentsMdSection(), A.Of[Section]),
		)),
	)
}

// rulesDirSections reads every file directly in .bob/rules as a rule
// section, skipping empty files.
func rulesDirSections() Effect[ProjectRulesDeps, []Section] {
	return F.Pipe3(
		inWorkspace(RulesDir),
		effect.Chain(ruleFiles()),
		effect.Chain(effect.TraverseArray(readRule())),
		effect.Map[ProjectRulesDeps](F.Flow2(
			option.CompactArray[string],
			A.Map(ruleSection()),
		)),
	)
}

// ProjectRules builds the project_rules section from AGENTS.md in the
// workspace root, wrapped as <agents_md><rule>…, followed by one rule section
// per file in .bob/rules. Without any rules it gives None, which yields no
// section. A file or directory that exists but cannot be read fails.
func ProjectRules() Effect[ProjectRulesDeps, Option[Section]] {
	return F.Pipe1(
		// Concat keeps the order of its arguments: AGENTS.md first.
		effect.ApplicativeMonoid[ProjectRulesDeps](A.Monoid[Section]()).Concat(agentsMd(), rulesDirSections()),
		effect.Map[ProjectRulesDeps](F.Flow2(
			option.FromPredicate(A.IsNonEmpty[Section]),
			option.Map(F.Flow2(RenderSections(), F.Bind1st(MakeSection, ProjectRulesTag))),
		)),
	)
}
