package mode

import (
	"strings"

	"github.com/Carsten-Leue/fp-go-harness/prompt"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/option"
	S "github.com/IBM/fp-go/v2/string"
)

// Tags of the available_modes section and the elements nested in it, as in
// the recorded system prompts.
const (
	AvailableModesTag = "available_modes"
	ModeTag           = "mode"
)

// modesIntro introduces the modes in the available_modes section. The
// recordings have a "# Mode Switching" paragraph before the section; the
// rendered sections carry only tagged blocks (see [prompt.RenderSections]),
// so a shorter version of the paragraph opens the body of the section.
const modesIntro = "# Mode Switching\n\n" +
	"You work in one of the modes below. Each mode has its own role and its own tools. When the task " +
	"needs another mode, for example to plan before you change code, call the " + SwitchModeName +
	" tool with the id of that mode and the reason. The conversation is kept; the system prompt and the " +
	"tools become those of the new mode."

// renderMode is a leaf: one formatter over the name, the ID, the description
// and the allowed tools of a mode, as a mode element of the section, in the
// recorded shape. The description says when to use the mode. The tools every
// mode has (use_skill, update_todo_list, switch_mode) are not listed.
func renderMode(m Mode) string {
	return "  <" + ModeTag + ">\n" +
		"    <name>" + m.Name + "</name>\n" +
		"    <id>" + m.ID + "</id>\n" +
		"    <when_to_use>" + m.Description + "</when_to_use>\n" +
		"    <available_tools>" + strings.Join(m.AllowedTools, ", ") + "</available_tools>\n" +
		"  </" + ModeTag + ">"
}

// AvailableModes renders modes as the available_modes section: a short
// introduction to switch_mode, then one mode element per mode with its name,
// ID, description and allowed tools, in order. Without modes there is no
// section.
func AvailableModes() func([]Mode) Option[Section] {
	return F.Flow2(
		option.FromPredicate(A.IsNonEmpty[Mode]),
		option.Map(F.Flow4(
			A.Map(renderMode),
			A.Prepend(modesIntro),
			S.Join("\n"),
			F.Bind1st(prompt.MakeSection, AvailableModesTag),
		)),
	)
}

// availableModes is the available_modes section of the table of the modes,
// the same in every mode, see [AvailableModes].
func availableModes() effect.Kleisli[ModeDeps, Mode, []Section] {
	return F.Constant1[Mode](effect.Asks(F.Flow3(
		ModeDeps.GetModes,
		AvailableModes(),
		A.FromOption[Section],
	)))
}
