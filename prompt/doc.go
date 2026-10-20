// Package prompt assembles the system prompt from tagged sections.
//
// A [Section] pairs a tag with its body and renders as
//
//	<tag>
//	body
//	</tag>
//
// [RenderSections] folds rendered sections into one prompt, separated by a
// blank line, as in the recorded system prompts. [StaticSections] are the
// sections embedded from prompts/ that don't depend on the environment,
// without the rules that name a tool the mode doesn't allow. [RoleDefinition] wraps the role of a mode. [EnvironmentInfo]
// builds the environment_info section from [EnvInfoDeps]. [ProjectRules]
// builds the optional project_rules section of a mode from AGENTS.md,
// .bob/rules and the mode's own rules directory in the workspace.
package prompt
