package prompt

import (
	"testing"

	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/stretchr/testify/assert"
)

func TestSectionTags(t *testing.T) {
	rendered := RenderSections()(A.From(
		MakeSection("role_definition", "You are a coding agent."),
		MakeSection("project_rules", "<agents_md>\n<rule>\nUse fp-go.\n</rule>\n</agents_md>"),
		MakeSection("environment_info", "Operating system: linux"),
	))

	assert.Equal(t, []string{"role_definition", "project_rules", "environment_info"}, SectionTags()(rendered))
}

func TestSectionTagsSkipsTextAndInlineTags(t *testing.T) {
	text := "Preamble <b>inline</b>\r\n<tool_use>\r\nUse <one> tool.\r\n</tool_use>\r\n\r\ntrailing text\r\n<markdown_rules>\r\nLinks.\r\n</markdown_rules>"

	assert.Equal(t, []string{"tool_use", "markdown_rules"}, SectionTags()(text))
}

func TestSectionTagsUnclosed(t *testing.T) {
	assert.Equal(t, []string{"base_rules"}, SectionTags()("<base_rules>\nno end\n<tool_use>\n</tool_use>"))
	assert.Empty(t, SectionTags()(""))
}

func TestCompareTags(t *testing.T) {
	diff := CompareTags()(pair.MakePair(
		[]string{"role_definition", "available_skills", "tool_use", "environment_info"},
		[]string{"role_definition", "environment_info", "tool_use", "custom"},
	))

	assert.Equal(t, []string{"available_skills"}, diff.Missing)
	assert.Equal(t, []string{"custom"}, diff.Extra)
	assert.Equal(t, []string{"role_definition", "tool_use", "environment_info"}, diff.Recorded)
	assert.Equal(t, []string{"role_definition", "environment_info", "tool_use"}, diff.Assembled)
}

func TestCompareSectionsSamePrompt(t *testing.T) {
	rendered := RenderSections()(A.From(
		MakeSection("tool_use", "Use tools."),
		MakeSection("base_rules", "Stay in the workspace."),
	))

	diff := CompareSections()(pair.Of(rendered))

	assert.Empty(t, diff.Missing)
	assert.Empty(t, diff.Extra)
	assert.Equal(t, []string{"tool_use", "base_rules"}, diff.Recorded)
	assert.Equal(t, diff.Recorded, diff.Assembled)
}
