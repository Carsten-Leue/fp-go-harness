package prompt

import (
	"testing"

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
	assert.Equal(t,
		A.From(RoleDefinitionTag, ToolUseTag, MarkdownRulesTag, BaseRulesTag),
		A.Map(pair.Head[string, string])(StaticSections()),
	)
}

func TestStaticSectionsBodies(t *testing.T) {
	for _, s := range StaticSections() {
		body := pair.Tail(s)
		assert.True(t, S.IsNonEmpty(body), pair.Head(s))
		assert.NotContains(t, body, "\r", pair.Head(s))
		assert.NotRegexp(t, `\s$`, body, pair.Head(s))
	}
}

func TestStaticSectionsRender(t *testing.T) {
	rendered := RenderSections()(StaticSections())

	assert.Regexp(t, `^<role_definition>\nYou are `, rendered)
	assert.Regexp(t, `\S\n</base_rules>$`, rendered)
	assert.NotContains(t, rendered, "\n\n</")
}
