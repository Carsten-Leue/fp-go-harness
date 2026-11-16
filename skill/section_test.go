package skill

import (
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/prompt"
	"github.com/IBM/fp-go/v2/option"
	"github.com/stretchr/testify/assert"
)

// testSkills are two skills, sorted by name.
func testSkills() []Skill {
	return []Skill{
		{Name: "build", Description: "Builds.", Location: "/ws/.bob/skills/build/SKILL.md", Content: skillText("build", "Builds.")},
		{Name: "fp-go", Description: "Functional Go.", Location: "/home/.bob/skills/fp-go/SKILL.md", Content: skillText("fp-go", "Functional Go.")},
	}
}

func TestAvailableSkills(t *testing.T) {
	section := AvailableSkills()(testSkills())

	assert.Equal(t, option.Some(prompt.MakeSection(AvailableSkillsTag, skillsIntro+"\n"+
		"<skill>\n"+
		"  <name>build</name>\n"+
		"  <description>Builds.</description>\n"+
		"  <location>/ws/.bob/skills/build/SKILL.md</location>\n"+
		"</skill>\n"+
		"<skill>\n"+
		"  <name>fp-go</name>\n"+
		"  <description>Functional Go.</description>\n"+
		"  <location>/home/.bob/skills/fp-go/SKILL.md</location>\n"+
		"</skill>")), section)
}

func TestAvailableSkillsNone(t *testing.T) {
	assert.Equal(t, option.None[Section](), AvailableSkills()(nil))
	assert.Empty(t, MakeSharedDeps()(nil).GetSharedSections())
}
