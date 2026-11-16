package skill

import (
	"github.com/Carsten-Leue/fp-go-harness/mode"
	"github.com/Carsten-Leue/fp-go-harness/prompt"
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/reader"
	S "github.com/IBM/fp-go/v2/string"
)

// Tags of the available_skills section and the elements nested in it, as in
// the recorded system prompts.
const (
	AvailableSkillsTag = "available_skills"
	SkillTag           = "skill"
)

// skillsIntro introduces the skills in the available_skills section,
// following the recorded one.
const skillsIntro = "You have access to skills that provide specialized instructions for specific tasks. " +
	"Review the available skills below. When a user's request matches a skill's description, activate it using the " +
	UseSkillName + " tool to load detailed instructions before proceeding. Only activate skills that are clearly " +
	"relevant to the current task. Activate each skill once per current context window. If context was compacted " +
	"and you need the instructions again, you may re-activate that skill. Only call " + UseSkillName +
	" for skills listed here — do not attempt to invoke skills not present in this list."

// renderSkill is a leaf: one formatter over the name, the description and
// the location of a skill, as a skill element of the section.
func renderSkill(s Skill) string {
	return "<" + SkillTag + ">\n" +
		"  <name>" + s.Name + "</name>\n" +
		"  <description>" + s.Description + "</description>\n" +
		"  <location>" + s.Location + "</location>\n" +
		"</" + SkillTag + ">"
}

// AvailableSkills renders skills as the available_skills section: a short
// introduction, then one skill element per skill with its name, description
// and location, in order. Without skills there is no section.
func AvailableSkills() func([]Skill) Option[Section] {
	return F.Flow2(
		option.FromPredicate(A.IsNonEmpty[Skill]),
		option.Map(F.Flow4(
			A.Map(renderSkill),
			A.Prepend(skillsIntro),
			S.Join("\n"),
			F.Bind1st(prompt.MakeSection, AvailableSkillsTag),
		)),
	)
}

// MakeSharedDeps is wiring: what skills give every mode, see
// [mode.SharedDeps]: their available_skills section (see [AvailableSkills])
// and the use_skill tool over them (see [Tools]). The skills are read twice:
// for the section and for the tool.
func MakeSharedDeps() func([]Skill) mode.SharedDeps {
	return F.Pipe1(
		F.Flow3(AvailableSkills(), A.FromOption[Section], F.Curry2(mode.MakeSharedDeps)),
		reader.Ap[mode.SharedDeps](F.Flow2(MakeSkillsDeps, Tools())),
	)
}
