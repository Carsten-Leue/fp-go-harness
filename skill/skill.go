package skill

import (
	"errors"

	"github.com/Carsten-Leue/fp-go-harness/mode"
	B "github.com/IBM/fp-go/v2/bytes"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	"go.yaml.in/yaml/v3"
)

// SkillFile is the file of a skill, in the skill's own directory.
const SkillFile = "SKILL.md"

// Skill is a skill: what the model is told about it, and the text it loads
// with use_skill.
//
// A skill is read from its [SkillFile]: the YAML front matter holds the name
// and the description, see [ParseSkill].
//
// fp-go:Lens
type Skill struct {
	// Name identifies the skill; use_skill takes it.
	Name string `yaml:"name"`
	// Description says when to use the skill.
	Description string `yaml:"description"`
	// Location is the path of the skill file.
	Location string `yaml:"-"`
	// Content is the whole text of the skill file, front matter included:
	// the result of use_skill, as recorded.
	Content string `yaml:"-"`
}

// errMissingName is the error of a skill file without a name.
var errMissingName = errors.New("skill without name")

// decodeFrontMatter is a leaf around the YAML decoder: it decodes the front
// matter of a skill file into a [Skill].
//
// Unlike a mode file, unknown keys are not errors: skill files are shared
// with other agents and carry keys this harness doesn't read (license,
// allowed-tools, metadata, …). An empty front matter decodes to the empty
// skill, which has no name.
func decodeFrontMatter(text string) Result[Skill] {
	var s Skill
	// Unmarshal runs before s is read: in one call expression, Go doesn't
	// fix the order of reading s and calling Unmarshal.
	err := yaml.Unmarshal([]byte(text), &s)
	return result.TryCatchError(s, err)
}

// validateSkill checks that a skill has a name.
func validateSkill() result.Kleisli[Skill, Skill] {
	nameLens := MakeSkillNameLens()

	return result.FromPredicate(F.Flow2(nameLens.Get, S.IsNonEmpty), F.Constant1[Skill](errMissingName))
}

// frontMatterOf decodes and validates the front matter of the text of a
// skill file.
func frontMatterOf() result.Kleisli[string, Skill] {
	return F.Flow3(
		mode.SplitFrontMatter(),
		result.Chain(F.Flow2(pair.Head[string, string], decodeFrontMatter)),
		result.Chain(validateSkill()),
	)
}

// ParseSkill parses a skill file: Markdown with YAML front matter that holds
// the name and the description of the [Skill]; the whole text is its
// content:
//
//	---
//	name: fp-go
//	description: Use this skill whenever writing Go code with fp-go.
//	---
//
//	# fp-go
//
// A file without front matter, with YAML that doesn't decode, or without a
// name is an error. Other keys are ignored. The text is read twice: for the
// front matter and as the content.
func ParseSkill() result.Kleisli[[]byte, Skill] {
	contentLens := MakeSkillContentLens()

	return F.Flow2(
		B.ToString,
		F.Pipe2(
			contentLens.Set,
			reader.Map[string](result.Map[Skill, Skill]),
			reader.Ap[Result[Skill]](frontMatterOf()),
		),
	)
}
