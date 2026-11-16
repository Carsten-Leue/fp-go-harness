package skill

import (
	"fmt"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	J "github.com/IBM/fp-go/v2/json"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/reader"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	openai "github.com/openai/openai-go/v3"
)

// UseSkillName is the name under which the model calls the use_skill tool.
const UseSkillName = "use_skill"

// UseSkillArgs are the arguments of a use_skill call.
//
// fp-go:Lens
type UseSkillArgs struct {
	SkillName string `json:"skill_name"`
}

// namesOf are the names of skills, in their order.
func namesOf() A.Operator[Skill, string] {
	nameLens := MakeSkillNameLens()

	return A.Map(nameLens.Get)
}

// availableNames lists names for a message; no names are "none".
func availableNames() func([]string) string {
	return F.Flow3(
		S.Join(", "),
		option.FromPredicate(S.IsNonEmpty),
		option.GetOrElse(F.Constant("none")),
	)
}

// unknownSkillError is a leaf: one formatter over the names of the skills
// and the name asked for.
func unknownSkillError(names []string, name string) func() error {
	return func() error {
		return fmt.Errorf("unknown skill %q, the available skills are: %s", name, availableNames()(names))
	}
}

// lookupIn finds a skill by its name. The lookup and the error for an
// unknown name both read the skills and the name.
func lookupIn() Reader[[]Skill, result.Kleisli[string, Skill]] {
	return F.Pipe2(
		F.Flow3(
			namesOf(),
			F.Curry2(unknownSkillError),
			reader.Map[string](result.FromOption[Skill]),
		),
		reader.Map[[]Skill](F.Flip(reader.Ap[Result[Skill], string, Option[Skill]])),
		reader.Ap[result.Kleisli[string, Skill]](F.Flow2(indexByName(), F.Flip(R.Lookup[Skill, string]))),
	)
}

// Lookup finds a skill of [SkillsDeps] by its name. An unknown name is an
// error that names the available skills.
func Lookup() Reader[SkillsDeps, result.Kleisli[string, Skill]] {
	return F.Flow2(
		SkillsDeps.GetSkills,
		lookupIn(),
	)
}

// UseSkill runs a use_skill call: it decodes the JSON arguments and returns
// the whole text of the skill file of the named skill, as recorded.
//
// Invalid arguments and an unknown name fail the effect; [tools.MakeToolCall]
// turns the failure into a tool message for the model, which names the
// available skills.
func UseSkill() effect.Kleisli[SkillsDeps, string, string] {
	skillNameLens := MakeUseSkillArgsSkillNameLens()
	contentLens := MakeSkillContentLens()

	return F.Flow4(
		F.Flow3(S.ToBytes, J.Unmarshal[UseSkillArgs], effect.FromResult[SkillsDeps, UseSkillArgs]),
		effect.Map[SkillsDeps](skillNameLens.Get),
		effect.Chain(F.Flow2(F.Flip(Lookup()), effect.FromReaderResult[SkillsDeps, Skill])),
		effect.Map[SkillsDeps](contentLens.Get),
	)
}

// useSkillDefinition describes use_skill to the model, following the
// recorded definition.
//
// It is wiring: a getter that builds the plain definition value.
func useSkillDefinition() openai.FunctionDefinitionParam {
	return openai.FunctionDefinitionParam{
		Name: UseSkillName,
		Description: openai.String("Activate a skill by name to load its full instructions into context. " +
			"Only call this once per skill per current context window. If context was compacted and you need " +
			"the instructions again, you may re-activate the skill."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"skill_name": map[string]any{
					"type":        "string",
					"description": "The name of the skill to activate",
				},
			},
			"required": []string{"skill_name"},
		},
	}
}

// MakeUseSkillTool builds the use_skill [tools.Tool] over the skills of
// [SkillsDeps]. It only reads, so every mode has it, see [MakeSharedDeps].
func MakeUseSkillTool() Reader[SkillsDeps, tools.Tool] {
	return F.Flow2(
		F.Flip(UseSkill()),
		F.Curry2(tools.MakeTool)(useSkillDefinition()),
	)
}

// Tools builds the registry of the skill tools: use_skill.
func Tools() Reader[SkillsDeps, tools.ToolRegistry] {
	return F.Flow3(
		MakeUseSkillTool(),
		A.Of[tools.Tool],
		tools.ToToolRegistry(),
	)
}
