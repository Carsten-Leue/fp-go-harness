package mode

import (
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/prompt"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeModeRequest is a request whose system message is the ID of the mode.
func fakeModeRequest(m Mode) thunk.ReaderIOResult[Params] {
	return thunk.Of(Params{Messages: []openai.ChatCompletionMessageParamUnion{openai.SystemMessage(m.ID)}})
}

// fakeSwitch changes a string state to the ID of the mode and the system
// message of its request.
func fakeSwitch(request ModeRequest) Endomorphism[string] {
	return F.Constant1[string](pair.Head(request) + ":" + pair.Tail(request).Messages[0].OfSystem.Content.OfString.Value)
}

// runSwitch runs a switch_mode call with the arguments and the allowed tools.
func runSwitch(t *testing.T, allowed []string, arguments string) (ToolResult[string], error) {
	t.Helper()

	deps := MakeSwitchModeDeps[string](MakeModeTableDeps(Modes()), allowed, fakeModeRequest, fakeSwitch)
	return effect.RunSync(effect.Provide[ToolResult[string]](deps)(SwitchMode[string]()(arguments)))(t.Context())
}

func TestSwitchMode(t *testing.T) {
	got, err := runSwitch(t, allToolNames, `{"mode_id":"plan","reason":"plan first"}`)
	require.NoError(t, err)

	assert.Equal(t, "plan:plan", pair.Head(got)("start"))
	assert.Equal(t, `Switched to mode "plan"`, pair.Tail(got))
}

func TestSwitchMode_Unknown(t *testing.T) {
	_, err := runSwitch(t, allToolNames, `{"mode_id":"code"}`)

	assert.EqualError(t, err, `unknown mode "code", use one of agent, plan, ask`)
}

func TestSwitchMode_Refused(t *testing.T) {
	_, err := runSwitch(t, readOnlyToolNames, `{"mode_id":"agent"}`)

	require.Error(t, err)
	assert.Contains(t, err.Error(), `switch to mode "agent" refused`)
	assert.Contains(t, err.Error(), "write_file")

	_, err = runSwitch(t, readOnlyToolNames, `{"mode_id":"plan"}`)
	assert.NoError(t, err)
}

func TestSwitchMode_InvalidArguments(t *testing.T) {
	_, err := runSwitch(t, allToolNames, `not json`)

	assert.Error(t, err)
}

func TestSwitchModeTools(t *testing.T) {
	deps := MakeSwitchModeDeps[string](MakeModeTableDeps(Modes()), allToolNames, fakeModeRequest, fakeSwitch)

	registry := SwitchModeTools[string]()(deps)
	require.Contains(t, registry, SwitchModeName)
	assert.Equal(t, SwitchModeName, registry[SwitchModeName].Definition.Name)
}

func TestAvailableModes(t *testing.T) {
	section, ok := option.Unwrap(AvailableModes()(Modes()))
	require.True(t, ok)

	rendered := prompt.RenderSection()(section)
	assert.Regexp(t, "^<available_modes>\n# Mode Switching\n", rendered)
	assert.Contains(t, rendered, "call the switch_mode tool")
	assert.Contains(t, rendered, "  <mode>\n    <name>Plan</name>\n    <id>plan</id>\n    <when_to_use>")
	assert.Contains(t, rendered, "    <available_tools>read_file, list_files, glob, grep</available_tools>\n  </mode>")
	assert.Regexp(t, "</mode>\n</available_modes>$", rendered)
	assert.Equal(t, []string{AvailableModesTag}, prompt.SectionTags()(rendered))

	assert.True(t, option.IsNone(AvailableModes()(nil)))
}
