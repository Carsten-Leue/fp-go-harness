package prompt

import (
	_ "embed"
	"regexp"
	"strings"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/pair"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	S "github.com/IBM/fp-go/v2/string"
)

// Tags of the static sections, as in the recorded system prompts.
const (
	RoleDefinitionTag = "role_definition"
	ToolUseTag        = "tool_use"
	MarkdownRulesTag  = "markdown_rules"
	BaseRulesTag      = "base_rules"
)

//go:embed prompts/tool_use.txt
var toolUse string

//go:embed prompts/markdown_rules.txt
var markdownRules string

//go:embed prompts/base_rules.txt
var baseRules string

// normalizeBody turns an embedded file into a section body: LF line endings,
// because a checkout on Windows may have CRLF, and no trailing line breaks,
// because [RenderSection] puts the closing tag on its own line.
func normalizeBody() func(string) string {
	return F.Flow2(
		F.Bind23of3(strings.ReplaceAll)("\r\n", "\n"),
		F.Bind2nd(strings.TrimRight, "\n"),
	)
}

// RoleDefinition builds the role_definition section from the role of a mode.
// It comes first in the system prompt, before the [StaticSections]. Its body
// is normalized like theirs.
func RoleDefinition() func(string) Section {
	return F.Flow2(
		normalizeBody(),
		F.Bind1st(MakeSection, RoleDefinitionTag),
	)
}

// staticBodies are the static sections as embedded, in the order of the
// recorded system prompts: tool_use, markdown_rules and base_rules.
func staticBodies() []Section {
	return A.From(
		MakeSection(ToolUseTag, toolUse),
		MakeSection(MarkdownRulesTag, markdownRules),
		MakeSection(BaseRulesTag, baseRules),
	)
}

// wordRegex matches the words of a rule. A tool name such as write_file is
// one word.
var wordRegex = regexp.MustCompile(`\w+`)

// ruleWords is a leaf: the words of a rule.
func ruleWords(rule string) []string {
	return wordRegex.FindAllString(rule, -1)
}

// isAnyOf tests whether a word is one of the names.
func isAnyOf() func([]string) P.Predicate[string] {
	return F.Flip(F.Flow2(S.Equals, A.Any[string]))
}

// namesAnyOf tests whether a rule names one of the tools.
func namesAnyOf() func([]string) P.Predicate[string] {
	return F.Flow3(
		isAnyOf(),
		A.Any[string],
		reader.Local[bool](ruleWords),
	)
}

// otherTools are the names of the tools that are not allowed, out of
// [tools.ToolNames].
func otherTools() Endomorphism[[]string] {
	return F.Flow4(
		isAnyOf(),
		P.Not[string],
		A.Filter[string],
		reader.Read[[]string](tools.ToolNames()),
	)
}

// allowedRules keeps the rules of a section body, one per line, that name no
// tool other than the allowed ones.
func allowedRules() func([]string) Endomorphism[string] {
	return F.Flow6(
		otherTools(),
		namesAnyOf(),
		P.Not[string],
		A.Filter[string],
		reader.Local[[]string](F.Bind2nd(strings.Split, "\n")),
		reader.Map[string](S.Join("\n")),
	)
}

// StaticSections returns the sections that don't depend on the environment,
// in the order of the recorded system prompts: tool_use, markdown_rules and
// base_rules, for the tools a mode allows. Their bodies are embedded from
// prompts/, with one rule per line; a rule that names a tool the mode doesn't
// allow is left out, so a read-only mode isn't told to write files.
func StaticSections() func([]string) []Section {
	return F.Flow5(
		allowedRules(),
		// normalize first, so the rules split at LF
		reader.Local[string](normalizeBody()),
		pair.Map[string, string, string],
		A.Map[Section, Section],
		reader.Read[[]Section](staticBodies()),
	)
}
