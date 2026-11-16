package skill

import (
	"context"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/mode"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// callUseSkill calls the use_skill tool of the registry of skills with
// arguments.
func callUseSkill(t *testing.T, skills []Skill, args string) Result[string] {
	t.Helper()

	tool, ok := Tools()(MakeSkillsDeps(skills))[UseSkillName]
	require.True(t, ok)
	return tool.Call(args)(context.Background())()
}

func TestUseSkillKnown(t *testing.T) {
	assert.Equal(t, result.Of(skillText("fp-go", "Functional Go.")),
		callUseSkill(t, testSkills(), `{"skill_name":"fp-go"}`))
}

func TestUseSkillUnknown(t *testing.T) {
	_, err := result.Unwrap(callUseSkill(t, testSkills(), `{"skill_name":"nope"}`))
	assert.EqualError(t, err, `unknown skill "nope", the available skills are: build, fp-go`)

	_, err = result.Unwrap(callUseSkill(t, nil, `{"skill_name":"nope"}`))
	assert.EqualError(t, err, `unknown skill "nope", the available skills are: none`)
}

func TestUseSkillInvalidArguments(t *testing.T) {
	_, err := result.Unwrap(callUseSkill(t, testSkills(), `not json`))
	assert.Error(t, err)
}

func TestUseSkillDefinition(t *testing.T) {
	definition := useSkillDefinition()

	assert.Equal(t, UseSkillName, definition.Name)
	assert.Equal(t, []string{"skill_name"}, definition.Parameters["required"])
}

func TestUseSkillInEveryMode(t *testing.T) {
	_, deps := makeModeDeps(t, testSkills())

	for _, m := range mode.Modes() {
		assert.Contains(t, R.Keys(mode.Tools()(m)(deps)), UseSkillName, m.ID)
	}
}
