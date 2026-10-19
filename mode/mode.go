package mode

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	Mg "github.com/IBM/fp-go/v2/magma"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
)

// IDs of the built-in modes.
const (
	// AgentID carries out tasks with every tool, including those that change
	// the workspace.
	AgentID = "agent"
	// PlanID investigates the workspace with the read-only tools and answers
	// with a plan.
	PlanID = "plan"
	// AskID answers questions with the read-only tools.
	AskID = "ask"
)

// Mode is a mode of the agent: its role and the tools the model may call.
//
// fp-go:Lens
type Mode struct {
	// ID names the mode on the command line and in its rules directory,
	// see [prompt.ModeRulesDir].
	ID string
	// Name is the display name of the mode.
	Name string
	// Prompt is the role of the mode, the body of its role_definition
	// section.
	Prompt string
	// AllowedTools are the names of the tools the model may call.
	AllowedTools []string
}

//go:embed prompts/agent.txt
var agentPrompt string

//go:embed prompts/plan.txt
var planPrompt string

//go:embed prompts/ask.txt
var askPrompt string

// Modes is the table of the built-in modes, in the order they are offered:
// Agent with every tool, Plan and Ask with the read-only tools.
func Modes() []Mode {
	return A.From(
		Mode{ID: AgentID, Name: "Agent", Prompt: agentPrompt, AllowedTools: tools.ToolNames()},
		Mode{ID: PlanID, Name: "Plan", Prompt: planPrompt, AllowedTools: tools.ReadToolNames()},
		Mode{ID: AskID, Name: "Ask", Prompt: askPrompt, AllowedTools: tools.ReadToolNames()},
	)
}

// IDs are the IDs of the built-in modes, in the order of [Modes].
func IDs() []string {
	idLens := MakeModeIDLens()

	return F.Pipe1(
		Modes(),
		A.Map(idLens.Get),
	)
}

// byID indexes the built-in modes by their ID.
func byID() map[string]Mode {
	idLens := MakeModeIDLens()

	return F.Pipe1(
		Modes(),
		R.FromArrayMap[Mode, string](Mg.Second[Mode]())(F.Flow2(
			pair.Of[Mode],
			pair.MapHead[Mode](idLens.Get),
		)),
	)
}

// unknownModeError is a leaf: one formatter over the ID and the known IDs.
func unknownModeError(id string) func() error {
	return func() error {
		return fmt.Errorf("unknown mode %q, use one of %s", id, strings.Join(IDs(), ", "))
	}
}

// Lookup finds a built-in mode by its ID. The lookup and the error for an
// unknown ID both read the ID.
func Lookup() result.Kleisli[string, Mode] {
	return F.Pipe1(
		F.Flow2(unknownModeError, result.FromOption[Mode]),
		reader.Ap[Result[Mode]](F.Flip(R.Lookup[Mode, string])(byID())),
	)
}
