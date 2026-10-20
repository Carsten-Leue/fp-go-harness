package mode

import (
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/prompt"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	M "github.com/IBM/fp-go/v2/monoid"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/openai/openai-go/v3"
)

// roleDefinition is the role_definition section of a mode, from its prompt.
func roleDefinition() effect.Kleisli[ModeDeps, Mode, []Section] {
	promptLens := MakeModePromptLens()

	return F.Flow4(
		promptLens.Get,
		prompt.RoleDefinition(),
		A.Of[Section],
		effect.Of[ModeDeps, []Section],
	)
}

// staticSections are the static sections of a mode, without the rules that
// name a tool the mode doesn't allow.
func staticSections() effect.Kleisli[ModeDeps, Mode, []Section] {
	allowedToolsLens := MakeModeAllowedToolsLens()

	return F.Flow3(
		allowedToolsLens.Get,
		prompt.StaticSections(),
		effect.Of[ModeDeps, []Section],
	)
}

// projectRules is the project_rules section of a mode, read with its ID; no
// section without rules.
func projectRules() effect.Kleisli[ModeDeps, Mode, []Section] {
	idLens := MakeModeIDLens()

	return F.Flow4(
		idLens.Get,
		prompt.ProjectRules(),
		effect.Local[Option[Section]](prompt.AsProjectRulesDeps[ModeDeps]),
		effect.Map[ModeDeps](A.FromOption[Section]),
	)
}

// environmentInfo is the environment_info section, the same in every mode.
func environmentInfo() effect.Kleisli[ModeDeps, Mode, []Section] {
	return F.Constant1[Mode](F.Pipe2(
		prompt.EnvironmentInfo(),
		effect.Local[Section](prompt.AsEnvInfoDeps[ModeDeps]),
		effect.Map[ModeDeps](A.Of[Section]),
	))
}

// Sections are the sections of the system prompt of a mode, in the order of
// the recorded system prompts: role_definition, the static sections,
// project_rules and environment_info. The sections are read concurrently;
// the monoid keeps their order.
func Sections() effect.Kleisli[ModeDeps, Mode, []Section] {
	sections := reader.ApplicativeMonoid[Mode](effect.ApplicativeMonoid[ModeDeps](A.Monoid[Section]()))

	return M.ConcatAll(sections)(A.From(
		roleDefinition(),
		staticSections(),
		projectRules(),
		environmentInfo(),
	))
}

// SystemPrompt renders the [Sections] of a mode into its system prompt.
func SystemPrompt() effect.Kleisli[ModeDeps, Mode, string] {
	return F.Flow2(
		Sections(),
		effect.Map[ModeDeps](prompt.RenderSections()),
	)
}

// Tools builds the registry of the tools a mode allows, see
// [tools.SelectTools].
func Tools() reader.Kleisli[tools.ToolsDeps, Mode, tools.ToolRegistry] {
	allowedToolsLens := MakeModeAllowedToolsLens()

	return F.Flow4(
		allowedToolsLens.Get,
		tools.SelectTools(),
		reader.Map[tools.ToolsDeps, tools.ToolRegistry, tools.ToolRegistry],
		reader.Read[Reader[tools.ToolsDeps, tools.ToolRegistry]](tools.MakeTools()),
	)
}

// systemRequest is a request whose only message is the system prompt.
func systemRequest() func(string) Params {
	messagesLens := oai.MakeChatCompletionNewParamsMessagesLens()

	return F.Flow4(
		openai.SystemMessage[string],
		A.Of[openai.ChatCompletionMessageParamUnion],
		messagesLens.Set,
		reader.Read[Params](Params{}),
	)
}

// withTools sets the definitions of the tools of a mode in a request.
func withTools() effect.Kleisli[ModeDeps, Mode, Endomorphism[Params]] {
	return F.Flow4(
		Tools(),
		reader.Local[tools.ToolRegistry](tools.AsToolsDeps[ModeDeps]),
		reader.Map[ModeDeps](tools.WithTools()),
		effect.Asks[ModeDeps, Endomorphism[Params]],
	)
}

// Request is the request a mode starts with: the system prompt of the mode
// as its only message, and the definitions of the tools the mode allows. The
// caller sets the model and appends the user's messages.
func Request() effect.Kleisli[ModeDeps, Mode, Params] {
	return F.Pipe2(
		F.Flow2(SystemPrompt(), effect.Map[ModeDeps](systemRequest())),
		reader.Map[Mode](effect.Ap[Params, ModeDeps, Params]),
		reader.Ap[Effect[ModeDeps, Params]](withTools()),
	)
}
