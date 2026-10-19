package prompt

import (
	_ "embed"
	"strings"

	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/pair"
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

// StaticSections returns the sections that depend neither on the mode nor on
// the environment, in the order of the recorded system prompts: tool_use,
// markdown_rules and base_rules. Their bodies are embedded from prompts/.
func StaticSections() []Section {
	return F.Pipe1(
		A.From(
			MakeSection(ToolUseTag, toolUse),
			MakeSection(MarkdownRulesTag, markdownRules),
			MakeSection(BaseRulesTag, baseRules),
		),
		A.Map(pair.Map[string](normalizeBody())),
	)
}
