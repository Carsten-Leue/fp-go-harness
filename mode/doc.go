// Package mode defines the modes of the agent and the request each one starts
// with.
//
// A [Mode] names its role and the tools the model may call in it. Each mode
// is a Markdown file: the YAML front matter holds its id, name, description,
// order and tools, the body is its role prompt. [ParseMode] parses one file,
// [ReadModes] every mode file of a file system. [Modes] is the table of
// built-in modes, embedded from modes/: Agent, Plan and Ask. A new built-in
// mode is a new file there. [Request] assembles the
// first request of a mode from [ModeDeps]: the system prompt, rendered from
// the role_definition of the mode, the static sections (without the rules
// that name a tool the mode doesn't allow), the project rules
// (including the rules of the mode in .bob/rules-<id>) and environment_info,
// and the definitions of the mode's tools.
package mode

//go:generate go tool gen lens
