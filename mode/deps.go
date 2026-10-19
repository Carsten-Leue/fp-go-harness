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

// ModeDeps are the dependencies of the request of a mode: the tools, whose
// definitions it lists, and the facts and files the system prompt is read
// from.
type ModeDeps interface {
	tools.ToolsDeps
	prompt.EnvInfoDeps
	prompt.ProjectRulesDeps
}

// modeDeps takes the workspace, the files and the directory listings from
// the tools, so the system prompt reads the same workspace the tools work in.
// The machine facts come from env; the getters forward to it, because
// embedding it would make the workspace getter ambiguous.
type modeDeps struct {
	tools.ToolsDeps
	env prompt.EnvInfoDeps
}

func (d *modeDeps) GetOS() string            { return d.env.GetOS() }
func (d *modeDeps) GetArch() string          { return d.env.GetArch() }
func (d *modeDeps) GetShell() string         { return d.env.GetShell() }
func (d *modeDeps) GetNow() io.IO[time.Time] { return d.env.GetNow() }

// MakeModeDeps is wiring: it builds [ModeDeps] from the tools and the facts
// about the machine.
func MakeModeDeps(t tools.ToolsDeps, env prompt.EnvInfoDeps) ModeDeps {
	return &modeDeps{t, env}
}

// MakeDefaultModeDeps is wiring: it builds [ModeDeps] for the running
// process over the dependencies of the tools. The shell is the program of
// [tools.DefaultShell], which runs the commands.
func MakeDefaultModeDeps(t tools.ToolsDeps) ModeDeps {
	shell := F.Pipe2(
		tools.DefaultShell(),
		A.Head[string],
		option.GetOrElse(F.Constant("")),
	)

	return MakeModeDeps(t, prompt.MakeDefaultEnvInfoDeps(t, shell))
}

// AsModeDeps is wiring: it narrows any wider dependencies to [ModeDeps], for
// use with [effect.Local].
func AsModeDeps[R ModeDeps](r R) ModeDeps {
	return r
}
