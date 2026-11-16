package mode

import (
	"time"

	"github.com/Carsten-Leue/fp-go-harness/prompt"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/option"
)

// ModeTableDeps hold the table of the modes: the built-in modes merged with
// the custom ones, see [ReadModeDirs].
type ModeTableDeps interface {
	// GetModes returns the modes, sorted by [Mode.Order], modes of the same
	// order by ID.
	GetModes() []Mode
}

type modeTableDeps struct {
	modes []Mode
}

func (d *modeTableDeps) GetModes() []Mode { return d.modes }

// MakeModeTableDeps is wiring: it builds [ModeTableDeps] over a table of
// modes, such as [Modes] or the result of [ReadModeDirs].
func MakeModeTableDeps(modes []Mode) ModeTableDeps {
	return &modeTableDeps{modes}
}

// AsModeTableDeps is wiring: it narrows any wider dependencies to
// [ModeTableDeps], for use with [effect.Local].
func AsModeTableDeps[R ModeTableDeps](r R) ModeTableDeps {
	return r
}

// SharedDeps hold what every mode offers besides its own role and tools: the
// sections that follow the static ones in the system prompt, and tools that
// every mode has, whatever tools it allows. The skills of package skill are
// such: the available_skills section and the use_skill tool. They are built
// by the wiring and passed in, because package skill depends on this one
// (for [SplitFrontMatter]) and so can't be imported here.
type SharedDeps interface {
	// GetSharedSections returns the sections that come after the static
	// sections and before project_rules, in order.
	GetSharedSections() []Section
	// GetSharedTools returns the tools of every mode. A shared tool replaces
	// a tool of the mode with the same name.
	GetSharedTools() tools.ToolRegistry
}

type sharedDeps struct {
	sections []Section
	tools    tools.ToolRegistry
}

func (d *sharedDeps) GetSharedSections() []Section       { return d.sections }
func (d *sharedDeps) GetSharedTools() tools.ToolRegistry { return d.tools }

// MakeSharedDeps is wiring: it builds [SharedDeps] over sections and tools.
// MakeSharedDeps(nil, nil) shares nothing.
func MakeSharedDeps(sections []Section, registry tools.ToolRegistry) SharedDeps {
	return &sharedDeps{sections, registry}
}

// ModeDeps are the dependencies of the request of a mode: the tools, whose
// definitions it lists, the facts and files the system prompt is read from,
// the table of the modes, and what every mode shares.
type ModeDeps interface {
	tools.ToolsDeps
	prompt.EnvInfoDeps
	prompt.ProjectRulesDeps
	ModeTableDeps
	SharedDeps
}

// modeDeps takes the workspace, the files and the directory listings from
// the tools, so the system prompt reads the same workspace the tools work in.
// The machine facts come from env; the getters forward to it, because
// embedding it would make the workspace getter ambiguous.
type modeDeps struct {
	tools.ToolsDeps
	ModeTableDeps
	SharedDeps
	env prompt.EnvInfoDeps
}

func (d *modeDeps) GetOS() string            { return d.env.GetOS() }
func (d *modeDeps) GetArch() string          { return d.env.GetArch() }
func (d *modeDeps) GetShell() string         { return d.env.GetShell() }
func (d *modeDeps) GetNow() io.IO[time.Time] { return d.env.GetNow() }

// MakeModeDeps is wiring: it builds [ModeDeps] from the tools, the facts
// about the machine, the table of the modes and what the modes share.
func MakeModeDeps(t tools.ToolsDeps, env prompt.EnvInfoDeps, modes []Mode, shared SharedDeps) ModeDeps {
	return &modeDeps{t, MakeModeTableDeps(modes), shared, env}
}

// MakeDefaultModeDeps is wiring: it builds [ModeDeps] for the running
// process over the dependencies of the tools, the table of the modes and
// what the modes share. The shell is the program of [tools.DefaultShell],
// which runs the commands.
func MakeDefaultModeDeps(t tools.ToolsDeps, modes []Mode, shared SharedDeps) ModeDeps {
	shell := F.Pipe2(
		tools.DefaultShell(),
		A.Head[string],
		option.GetOrElse(F.Constant("")),
	)

	return MakeModeDeps(t, prompt.MakeDefaultEnvInfoDeps(t, shell), modes, shared)
}

// AsModeDeps is wiring: it narrows any wider dependencies to [ModeDeps], for
// use with [effect.Local].
func AsModeDeps[R ModeDeps](r R) ModeDeps {
	return r
}
