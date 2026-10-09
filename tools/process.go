package tools

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"time"

	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/option"
	R "github.com/IBM/fp-go/v2/record"
)

// waitDelay bounds how long a finished or cancelled command may keep its
// output pipes open, e.g. through a child process it started.
const waitDelay = time.Second

// Process is a command line to run and the directory to run it in.
//
// fp-go:Lens
type Process struct {
	Command string
	Dir     string
}

// ProcessOutput is what a command that ran to its end produced.
//
// fp-go:Lens
type ProcessOutput struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

func MakeProcess(command, dir string) Process {
	return Process{Command: command, Dir: dir}
}

// RunProcess runs a command line until it exits. A non-zero exit code is a
// [ProcessOutput], not an error; a command that cannot be started or whose
// context ends fails.
type RunProcess = thunk.Kleisli[Process, ProcessOutput]

// ProcessDeps gives the tools access to processes.
type ProcessDeps interface {
	GetRunProcess() RunProcess
}

type processDeps struct {
	runProcess RunProcess
}

func (d *processDeps) GetRunProcess() RunProcess {
	return d.runProcess
}

// MakeProcessDeps builds [ProcessDeps] that run commands with runProcess.
func MakeProcessDeps(runProcess RunProcess) ProcessDeps {
	return &processDeps{runProcess}
}

// MakeDefaultProcessDeps builds [ProcessDeps] that run commands in the shell
// of the operating system, see [DefaultShell].
func MakeDefaultProcessDeps() ProcessDeps {
	return MakeProcessDeps(RunShell()(DefaultShell()))
}

func AsProcessDeps[T ProcessDeps](r T) ProcessDeps {
	return r
}

// shells are the program and the leading arguments that run a command line,
// by operating system.
var shells = map[string][]string{
	"windows": {"powershell.exe", "-NoProfile", "-NonInteractive", "-Command"},
}

// posixShell runs command lines on every operating system without an entry
// in shells.
var posixShell = []string{"sh", "-c"}

// DefaultShell is the shell of the operating system: PowerShell on Windows,
// sh everywhere else.
func DefaultShell() []string {
	return F.Pipe2(
		shells,
		R.Lookup[[]string](runtime.GOOS),
		option.GetOrElse(F.Constant(posixShell)),
	)
}

// isExit tests for the errors of a command that ran to its end: a non-zero
// exit code, or output pipes still held open by a child process.
func isExit(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) || errors.Is(err, exec.ErrWaitDelay)
}

// runIn is a leaf: [exec.CommandContext] reports through its own fields. The
// command line is the last argument of shell. When the context ends, the
// process is killed and its error is the context's.
func runIn(shell []string) func(context.Context, Process) (ProcessOutput, error) {
	return func(ctx context.Context, p Process) (ProcessOutput, error) {
		var stdout, stderr bytes.Buffer

		args := append(append([]string(nil), shell[1:]...), p.Command)
		cmd := exec.CommandContext(ctx, shell[0], args...)
		cmd.Dir = p.Dir
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		cmd.WaitDelay = waitDelay

		err := cmd.Run()
		if ctx.Err() != nil {
			return ProcessOutput{}, ctx.Err()
		}
		if err != nil && !isExit(err) {
			return ProcessOutput{}, err
		}
		return ProcessOutput{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: cmd.ProcessState.ExitCode()}, nil
	}
}

// RunShell builds a [RunProcess] that hands the command line to a shell, a
// program followed by its leading arguments, e.g. ["sh", "-c"].
func RunShell() Reader[[]string, RunProcess] {
	return F.Flow2(runIn, thunk.Eitherize1[func(context.Context, Process) (ProcessOutput, error)])
}
