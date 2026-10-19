package prompt

import (
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/pair"
	S "github.com/IBM/fp-go/v2/string"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeBody(t *testing.T) {
	assert.Equal(t, "a\nb", normalizeBody()("a\r\nb\r\n\r\n"))
	assert.Equal(t, "a\nb", normalizeBody()("a\nb\n"))
	assert.Equal(t, "", normalizeBody()(""))
}

func TestStaticSectionsTags(t *testing.T) {
	for _, allowed := range [][]string{tools.ToolNames(), tools.ReadToolNames()} {
		assert.Equal(t,
			A.From(ToolUseTag, MarkdownRulesTag, BaseRulesTag),
			A.Map(pair.Head[string, string])(StaticSections()(allowed)),
		)
	}
}

func TestStaticSectionsBodies(t *testing.T) {
	for _, allowed := range [][]string{tools.ToolNames(), tools.ReadToolNames()} {
		for _, s := range StaticSections()(allowed) {
			body := pair.Tail(s)
			assert.True(t, S.IsNonEmpty(body), pair.Head(s))
			assert.NotContains(t, body, "\r", pair.Head(s))
			assert.NotRegexp(t, `\s$`, body, pair.Head(s))
		}
	}
}

func TestStaticSectionsRender(t *testing.T) {
	rendered := RenderSections()(StaticSections()(tools.ToolNames()))

	assert.Regexp(t, `^<tool_use>\n- `, rendered)
	assert.Regexp(t, `\S\n</base_rules>$`, rendered)
	assert.NotContains(t, rendered, "\n\n</")
}

func TestStaticSectionsAllTools(t *testing.T) {
	rendered := RenderSections()(StaticSections()(tools.ToolNames()))

	assert.Equal(t, RenderSections()(A.Map(pair.Map[string](normalizeBody()))(staticBodies())), rendered)
	assert.Contains(t, rendered, "write_file")
	assert.Contains(t, rendered, "execute_command")
}

func TestStaticSectionsReadOnlyTools(t *testing.T) {
	rendered := RenderSections()(StaticSections()(tools.ReadToolNames()))

	for _, name := range []string{
		tools.WriteFileName, tools.SearchAndReplaceName, tools.ApplyDiffName,
		tools.InsertContentName, tools.ExecuteCommandName,
	} {
		assert.NotContains(t, rendered, name)
	}
	assert.Contains(t, rendered, "- Choose the most appropriate tool for each step.\n")
	assert.Contains(t, rendered, "- Follow existing code style and project conventions.\n")
}

func TestAllowedRules(t *testing.T) {
	body := "- use read_file\n- use write_file\n- use write_files\n- no tool"

	assert.Equal(t, "- use read_file\n- use write_files\n- no tool",
		allowedRules()(tools.ReadToolNames())(body))
	assert.Equal(t, body, allowedRules()(tools.ToolNames())(body))
	assert.Equal(t, "- use write_files\n- no tool", allowedRules()(nil)(body))
}

func TestRoleDefinition(t *testing.T) {
	assert.Equal(t,
		MakeSection(RoleDefinitionTag, "You are an agent.\n\nGuidelines"),
		RoleDefinition()("You are an agent.\r\n\r\nGuidelines\r\n"),
	)
}
