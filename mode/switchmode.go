package mode

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	J "github.com/IBM/fp-go/v2/json"
	"github.com/IBM/fp-go/v2/pair"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	openai "github.com/openai/openai-go/v3"
)

// SwitchModeName is the name under which the model calls the switch_mode
// tool.
const SwitchModeName = "switch_mode"

// SwitchModeArgs are the arguments of a switch_mode call: the ID of the
// target mode, and optionally why the model switches. The reason is for the
// model's own reasoning and the logs; the switch doesn't read it.
//
// fp-go:Lens
type SwitchModeArgs struct {
	ModeID string `json:"mode_id"`
	Reason string `json:"reason,omitempty"`
}

// ModeRequest is the request of a mode paired with its ID: what a switch to
// the mode installs in a state, see [SwitchModeDeps.GetSwitch]. Only the
// system messages and the tool definitions of the request are used.
type ModeRequest = Pair[string, Params]

// SwitchModeDeps are the dependencies of [SwitchMode] for a state of type T,
// a session. The state is a type parameter because package session can't be
// imported here, like the state of [tools.StateCall].
type SwitchModeDeps[T any] interface {
	ModeTableDeps
	// GetAllowedTools returns the names of the tools the session may call:
	// a switch to a mode that allows any other tool is refused. The CLI
	// passes the tools of the mode it started in, whose registry the
	// session's tool caller is built from, see [Tools].
	GetAllowedTools() []string
	// GetModeRequest returns the builder of the request a mode starts with,
	// such as [Request] with the session tools of the caller.
	GetModeRequest() thunk.Kleisli[Mode, Params]
	// GetSwitch returns the change of the state to the request of a mode,
	// such as session.SwitchMode.
	GetSwitch() func(ModeRequest) Endomorphism[T]
}

type switchModeDeps[T any] struct {
	ModeTableDeps
	allowed  []string
	request  thunk.Kleisli[Mode, Params]
	switchTo func(ModeRequest) Endomorphism[T]
}

func (d *switchModeDeps[T]) GetAllowedTools() []string                    { return d.allowed }
func (d *switchModeDeps[T]) GetModeRequest() thunk.Kleisli[Mode, Params]  { return d.request }
func (d *switchModeDeps[T]) GetSwitch() func(ModeRequest) Endomorphism[T] { return d.switchTo }

// MakeSwitchModeDeps is wiring: it builds [SwitchModeDeps] over the table of
// the modes, the allowed tools, the builder of a mode's request and the
// change of the state to it.
func MakeSwitchModeDeps[T any](table ModeTableDeps, allowed []string, request thunk.Kleisli[Mode, Params], switchTo func(ModeRequest) Endomorphism[T]) SwitchModeDeps[T] {
	return &switchModeDeps[T]{table, allowed, request, switchTo}
}

// toolsOutside are the tools a mode allows that are not among the allowed
// ones.
func toolsOutside() func([]string) A.Kleisli[Mode, string] {
	allowedToolsLens := MakeModeAllowedToolsLens()

	return F.Flow4(
		F.Curry2(slices.Contains[[]string]),
		P.Not[string],
		A.Filter[string],
		reader.Compose[[]string](allowedToolsLens.Get),
	)
}

// refusedSwitchError is a leaf: one formatter over the allowed tools and the
// mode whose tools are not all among them.
func refusedSwitchError(allowed []string) func(Mode) error {
	return func(m Mode) error {
		return fmt.Errorf("switch to mode %q refused: it needs the tools %s, which this session does not allow (allowed: %s)",
			m.ID, strings.Join(toolsOutside()(allowed)(m), ", "), strings.Join(allowed, ", "))
	}
}

// allowedIn keeps a mode whose tools are all among the allowed ones, and
// refuses any other. The check and the error both read the allowed tools.
func allowedIn() func([]string) result.Kleisli[Mode, Mode] {
	return F.Pipe2(
		F.Flow2(toolsOutside(), reader.Map[Mode](A.IsEmpty[string])),
		reader.Map[[]string](F.Curry2(result.FromPredicate[Mode])),
		reader.Ap[result.Kleisli[Mode, Mode]](refusedSwitchError),
	)
}

// lookupMode finds the target mode of a switch in the table, see [Lookup].
func lookupMode[T any]() effect.Kleisli[SwitchModeDeps[T], string, Mode] {
	return F.Flow2(
		F.Flip(reader.Local[result.Kleisli[string, Mode]](AsModeTableDeps[SwitchModeDeps[T]])(Lookup())),
		effect.FromReaderResult[SwitchModeDeps[T], Mode],
	)
}

// checkAllowed refuses a target mode with tools the session doesn't allow,
// see [allowedIn].
func checkAllowed[T any]() effect.Kleisli[SwitchModeDeps[T], Mode, Mode] {
	return F.Flow2(
		F.Flip(F.Flow2(SwitchModeDeps[T].GetAllowedTools, allowedIn())),
		effect.FromReaderResult[SwitchModeDeps[T], Mode],
	)
}

// requestOf builds the request of a mode, paired with its ID. The ID and the
// request are independent reads of the mode.
func requestOf[T any]() effect.Kleisli[SwitchModeDeps[T], Mode, ModeRequest] {
	idLens := MakeModeIDLens()

	return F.Pipe2(
		F.Flow2(idLens.Get, F.Curry2(pair.MakePair[string, Params])),
		reader.Map[Mode](effect.Map[SwitchModeDeps[T], Params, ModeRequest]),
		reader.Ap[Effect[SwitchModeDeps[T], ModeRequest]](F.Flow2(
			F.Flip(SwitchModeDeps[T].GetModeRequest),
			effect.FromReaderThunk[SwitchModeDeps[T], Params],
		)),
	)
}

// switchedText is the text of a successful switch_mode call. There is no
// recording of it; it names the mode the session switched to.
func switchedText() func(string) string {
	return S.Format[string]("Switched to mode %q")
}

// switchTo is the result of a switch to the request of a mode: the change of
// the state, see [SwitchModeDeps.GetSwitch], and [switchedText]. The change
// and the text are independent reads of the request.
func switchTo[T any]() reader.Kleisli[SwitchModeDeps[T], ModeRequest, ToolResult[T]] {
	return F.Pipe2(
		F.Flow3(pair.Head[string, Params], switchedText(), pair.FromTail[Endomorphism[T], string]),
		reader.Map[ModeRequest](reader.Map[SwitchModeDeps[T], Endomorphism[T], ToolResult[T]]),
		reader.Ap[Reader[SwitchModeDeps[T], ToolResult[T]]](F.Flip(SwitchModeDeps[T].GetSwitch)),
	)
}

// SwitchMode runs a switch_mode call (H5): it decodes the JSON arguments,
// looks the target mode up in the table, checks that the session allows its
// tools, builds its request and returns the change of the state to it, with
// the text Switched to mode "<id>".
//
// Invalid arguments, an unknown ID (the error names the modes of the table,
// see [Lookup]), a refused mode and a failure to build its request fail the
// call; [tools.MakeStateToolCall] turns the failure into a tool message with
// the identity, so the session stays in its mode. A refused switch could ask
// for approval once I1 has it.
func SwitchMode[T any]() effect.Kleisli[SwitchModeDeps[T], string, ToolResult[T]] {
	modeIDLens := MakeSwitchModeArgsModeIDLens()

	return F.Flow6(
		F.Flow3(S.ToBytes, J.Unmarshal[SwitchModeArgs], effect.FromResult[SwitchModeDeps[T], SwitchModeArgs]),
		effect.Map[SwitchModeDeps[T]](modeIDLens.Get),
		effect.Chain(lookupMode[T]()),
		effect.Chain(checkAllowed[T]()),
		effect.Chain(requestOf[T]()),
		effect.ChainReaderK(switchTo[T]()),
	)
}

// switchModeDefinition describes switch_mode to the model: the required ID
// of the target mode and an optional reason.
//
// It is wiring: a getter that builds the plain definition value.
func switchModeDefinition() openai.FunctionDefinitionParam {
	return openai.FunctionDefinitionParam{
		Name: SwitchModeName,
		Description: openai.String("Switch to another mode, one of the modes of the available_modes section. " +
			"The conversation is kept; the system prompt and the tools become those of the new mode. Use it " +
			"when the task needs the role or the tools of another mode."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"mode_id": map[string]any{
					"type":        "string",
					"description": "The id of the mode to switch to, for example plan or agent",
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Why the switch is needed",
				},
			},
			"required": []string{"mode_id"},
		},
	}
}

// MakeSwitchModeTool builds the switch_mode tool for a state of type T over
// [SwitchModeDeps].
func MakeSwitchModeTool[T any]() Reader[SwitchModeDeps[T], tools.StateTool[T]] {
	return F.Flow2(
		F.Flip(SwitchMode[T]()),
		F.Curry2(tools.MakeToolOf[tools.StateCall[T]])(switchModeDefinition()),
	)
}

// SwitchModeTools builds the registry of the mode tools for a state of type
// T: switch_mode. It only changes the state, so every mode has it.
func SwitchModeTools[T any]() Reader[SwitchModeDeps[T], tools.StateRegistry[T]] {
	return F.Flow3(
		MakeSwitchModeTool[T](),
		A.Of[tools.StateTool[T]],
		tools.ToToolRegistryOf[tools.StateCall[T]](),
	)
}
