package prompt

import (
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/pair"
	S "github.com/IBM/fp-go/v2/string"
)

// sectionSeparator separates two rendered sections: a blank line.
const sectionSeparator = "\n\n"

// MakeSection is a leaf: the constructor of a [Section] from its tag and body.
func MakeSection(tag, body string) Section {
	return pair.MakePair(tag, body)
}

// renderTagged is a leaf: one formatter over the tag and the body.
func renderTagged(tag, body string) string {
	return "<" + tag + ">\n" + body + "\n</" + tag + ">"
}

// RenderSection renders a section as its body enclosed in the opening and
// closing tag, each on its own line.
func RenderSection() func(Section) string {
	return pair.Paired(renderTagged)
}

// SectionMonoid concatenates rendered sections with a blank line in between.
// The empty string is its identity, so an empty rendering adds no separator.
func SectionMonoid() Monoid[string] {
	return S.IntersperseMonoid(sectionSeparator)
}

// RenderSections renders the sections in order and concatenates them with
// [SectionMonoid]. No sections render as the empty string.
func RenderSections() func([]Section) string {
	return F.Pipe1(
		RenderSection(),
		A.FoldMap[Section](SectionMonoid()),
	)
}
