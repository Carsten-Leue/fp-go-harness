package prompt

import (
	"runtime"
	"time"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/reader"
)

// EnvironmentInfoTag is the tag of the environment_info section.
const EnvironmentInfoTag = "environment_info"

// dateLayout formats the current date of the environment_info section.
const dateLayout = "2006-01-02"

// ClockDeps gives a section the current time.
type ClockDeps interface {
	// GetNow reads the current time.
	GetNow() IO[time.Time]
}

// EnvInfoDeps gives the environment_info section the facts about the machine
// the agent runs on.
type EnvInfoDeps interface {
	tools.WorkspaceDeps
	ClockDeps
	// GetOS is the operating system, as in [runtime.GOOS].
	GetOS() string
	// GetArch is the architecture, as in [runtime.GOARCH].
	GetArch() string
	// GetShell is the shell that runs commands.
	GetShell() string
}

type envInfoDeps struct {
	tools.WorkspaceDeps
	os, arch, shell string
	now             IO[time.Time]
}

func (d *envInfoDeps) GetOS() string         { return d.os }
func (d *envInfoDeps) GetArch() string       { return d.arch }
func (d *envInfoDeps) GetShell() string      { return d.shell }
func (d *envInfoDeps) GetNow() IO[time.Time] { return d.now }

// MakeEnvInfoDeps is wiring: it builds [EnvInfoDeps] from its parts.
func MakeEnvInfoDeps(workspace tools.WorkspaceDeps, os, arch, shell string, now IO[time.Time]) EnvInfoDeps {
	return &envInfoDeps{workspace, os, arch, shell, now}
}

// MakeDefaultEnvInfoDeps is wiring: it builds [EnvInfoDeps] for the running
// process, with the operating system and architecture of the Go runtime and
// the system clock.
func MakeDefaultEnvInfoDeps(workspace tools.WorkspaceDeps, shell string) EnvInfoDeps {
	return MakeEnvInfoDeps(workspace, runtime.GOOS, runtime.GOARCH, shell, io.Now)
}

// AsEnvInfoDeps is wiring: it narrows any wider dependencies to
// [EnvInfoDeps], for use with [effect.Local].
func AsEnvInfoDeps[R EnvInfoDeps](r R) EnvInfoDeps {
	return r
}

// renderEnvInfo is a leaf: one formatter over all facts of the section body.
func renderEnvInfo(os, arch, shell, root string, now time.Time) string {
	return "### Workspace Info\n" +
		"The currently active workspace folder is " + root + "\n" +
		"Use file paths relative to this directory.\n" +
		"### Command Execution Environment\n" +
		"Platform: " + os + "\n" +
		"Architecture: " + arch + "\n" +
		"Shell: " + shell + "\n" +
		"Current date: " + now.Format(dateLayout)
}

type (
	// The partial applications of renderEnvInfo, named because [reader.Ap]
	// cannot infer them.
	envInfoFromShell = func(string) func(string) func(time.Time) string
	envInfoFromRoot  = func(string) func(time.Time) string
	envInfoFromNow   = func(time.Time) string
)

// envInfoBody reads every fact of the section body from the dependencies,
// except the current time, which is an effect.
func envInfoBody() Reader[EnvInfoDeps, envInfoFromNow] {
	return F.Pipe4(
		EnvInfoDeps.GetOS,
		reader.Map[EnvInfoDeps](F.Curry5(renderEnvInfo)),
		reader.Ap[envInfoFromShell](EnvInfoDeps.GetArch),
		reader.Ap[envInfoFromRoot](EnvInfoDeps.GetShell),
		reader.Ap[envInfoFromNow](EnvInfoDeps.GetWorkspaceRoot),
	)
}

// now reads the current time through the [ClockDeps] of any dependencies. It
// is a generic I/O leaf: Go cannot infer R through a nullary call.
func now[R ClockDeps]() Effect[R, time.Time] {
	return effect.FromReaderIO(R.GetNow)
}

// EnvironmentInfo builds the environment_info section: the workspace root,
// the operating system, the architecture, the shell and the current date.
func EnvironmentInfo() Effect[EnvInfoDeps, Section] {
	return F.Pipe2(
		effect.Asks(envInfoBody()),
		effect.Ap[string](now[EnvInfoDeps]()),
		effect.Map[EnvInfoDeps](F.Bind1st(MakeSection, EnvironmentInfoTag)),
	)
}
