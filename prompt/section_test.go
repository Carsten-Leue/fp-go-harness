package prompt

import (
	"testing"

	A "github.com/IBM/fp-go/v2/array"
	"github.com/stretchr/testify/assert"
)

func TestRenderSection(t *testing.T) {
	assert.Equal(t,
		"<role_definition>\nYou are an agent.\n</role_definition>",
		RenderSection()(MakeSection("role_definition", "You are an agent.")),
	)
}

func TestRenderSectionsEmpty(t *testing.T) {
	assert.Equal(t, "", RenderSections()(A.Empty[Section]()))
}

func TestRenderSectionsSingle(t *testing.T) {
	assert.Equal(t,
		"<a>\nx\n</a>",
		RenderSections()(A.Of(MakeSection("a", "x"))),
	)
}

func TestRenderSectionsInOrder(t *testing.T) {
	assert.Equal(t,
		"<a>\nx\n</a>\n\n<b>\ny\n</b>\n\n<c>\nz\n</c>",
		RenderSections()(A.From(
			MakeSection("a", "x"),
			MakeSection("b", "y"),
			MakeSection("c", "z"),
		)),
	)
}

func TestSectionMonoidIdentity(t *testing.T) {
	m := SectionMonoid()
	s := RenderSection()(MakeSection("a", "x"))

	assert.Equal(t, s, m.Concat(m.Empty(), s))
	assert.Equal(t, s, m.Concat(s, m.Empty()))
}

func TestSectionMonoidAssociative(t *testing.T) {
	m := SectionMonoid()

	assert.Equal(t,
		m.Concat(m.Concat("a", "b"), "c"),
		m.Concat("a", m.Concat("b", "c")),
	)
}
