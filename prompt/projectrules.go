package prompt

import (
	"errors"
	"io/fs"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
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

// readRule reads the rule in the file at the absolute path. A missing or
// empty file gives None; the text has LF line endings and no trailing line
// breaks. It is an I/O leaf and keeps its parameter, as tools' walkDir does.
func readRule(path string) Effect[ProjectRulesDeps, Option[string]] {
	return F.Pipe2(
		effect.Asks(ProjectRulesDeps.GetReadFile),
		effect.ChainThunkK[ProjectRulesDeps](F.Flow3(
			reader.Read[ioresult.IOResult[[]byte]](path),
			missingAsNone[[]byte](),
			thunk.FromIOResult[Option[[]byte]],
		)),
		effect.Map[ProjectRulesDeps](option.Chain(F.Flow3(
			bytesToString,
			normalizeBody(),
			option.FromPredicate(S.IsNonEmpty),
		))),
	)
}

// ruleFiles lists the absolute paths of the files directly in dir, in the
// order of the walker (lexical for [tools.WalkFS]). A missing directory has
// no files. It is an I/O leaf and keeps its parameter, as tools' walkDir does.
func ruleFiles(dir string) Effect[ProjectRulesDeps, []string] {
	lenses := tools.MakeFileEntryLenses()

	return F.Pipe2(
		effect.Asks(ProjectRulesDeps.GetWalkDir),
		effect.ChainThunkK[ProjectRulesDeps](F.Flow4(
			reader.Read[ioresult.Kleisli[string, []tools.FileEntry]](false),
			reader.Read[ioresult.IOResult[[]tools.FileEntry]](dir),
			missingAsNone[[]tools.FileEntry](),
			thunk.FromIOResult[Option[[]tools.FileEntry]],
		)),
		effect.Map[ProjectRulesDeps](F.Flow3(
			option.GetOrElse(A.Empty[tools.FileEntry]),
			A.Filter(P.Not(lenses.IsDir.Get)),
			A.Map(F.Flow2(lenses.Path.Get, F.Flip(file.Join)(dir))),
		)),
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
		effect.Chain(readRule),
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
		effect.Chain(ruleFiles),
		effect.Chain(effect.TraverseArray(readRule)),
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
