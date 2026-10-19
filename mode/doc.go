// Package mode defines the modes of the agent and the request each one starts
// with.
//
// A [Mode] names its role and the tools the model may call in it. [Modes] is
// the table of built-in modes: Agent, Plan and Ask. [Request] assembles the
// first request of a mode from [ModeDeps]: the system prompt, rendered from
// the role_definition of the mode, the static sections, the project rules
// (including the rules of the mode in .bob/rules-<id>) and environment_info,
// and the definitions of the mode's tools.
package mode

//go:generate go tool gen lens
