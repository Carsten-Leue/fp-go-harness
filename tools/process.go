package tools

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"time"

	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/ioref"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/reader"
	R "github.com/IBM/fp-go/v2/record"
)

// logPattern names the log files of background processes, see [os.CreateTemp].
const logPattern = "command-*.log"

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

// BackgroundProcess is a command that was started without waiting for it:
// its process id and the file that receives its standard output and standard
// error.
//
// fp-go:Lens
type BackgroundProcess struct {
	Pid     int
	Command string
	LogPath string
}

// ProcessTable holds the background processes that are still running, by
// process id.
type ProcessTable = map[int]BackgroundProcess

func MakeProcess(command, dir string) Process {
	return Process{Command: command, Dir: dir}
}

// RunProcess runs a command line until it exits. A non-zero exit code is a
// [ProcessOutput], not an error; a command that cannot be started or whose
// context ends fails.
type RunProcess = thunk.Kleisli[Process, ProcessOutput]

// StartProcess starts a command line and returns without waiting for it.
// The process outlives the context of the call; a command that cannot be
// started fails.
type StartProcess = thunk.Kleisli[Process, BackgroundProcess]

// ProcessDeps gives the tools access to processes: commands that run to
// their end, commands started in the background, and the table of the
// background processes that are still running.
type ProcessDeps interface {
	GetRunProcess() RunProcess
	GetStartProcess() StartProcess
	GetProcessTable() IORef[ProcessTable]
}

type processDeps struct {
	runProcess   RunProcess
	startProcess StartProcess
	table        IORef[ProcessTable]
}

func (d *processDeps) GetRunProcess() RunProcess {
	return d.runProcess
}

func (d *processDeps) GetStartProcess() StartProcess {
	return d.startProcess
}

func (d *processDeps) GetProcessTable() IORef[ProcessTable] {
	return d.table
}

// MakeProcessDeps builds [ProcessDeps] that run commands with runProcess and
// start them with startProcess. table is the one startProcess registers its
// processes in.
func MakeProcessDeps(runProcess RunProcess, startProcess StartProcess, table IORef[ProcessTable]) ProcessDeps {
	return &processDeps{runProcess, startProcess, table}
}

// processDepsIn builds [ProcessDeps] for a shell whose background processes
// write their logs to logDir: the table is read twice, by the starter that
// registers processes in it and by the getter.
func processDepsIn(shell []string, logDir string) Reader[IORef[ProcessTable], ProcessDeps] {
	return F.Pipe1(
		F.Flow2(StartShell(shell, logDir), F.Curry3(MakeProcessDeps)(RunShell()(shell))),
		reader.Ap[ProcessDeps](F.Identity[IORef[ProcessTable]]),
	)
}

// MakeDefaultProcessDeps builds [ProcessDeps] that run commands in the shell
// of the operating system, see [DefaultShell]. Background processes write
// their output to a new file in logDir, which is created when needed. The
// model reads these files with read_file, so logDir should lie inside the
// workspace.
//
// Creating the deps allocates the process table, hence the IO.
func MakeDefaultProcessDeps(logDir string) IO[ProcessDeps] {
	return F.Pipe2(
		ProcessTable{},
		ioref.MakeIORef[ProcessTable],
		io.Map(processDepsIn(DefaultShell(), logDir)),
	)
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

// shellArgs are the arguments that hand a command line to shell: the leading
// arguments of the shell followed by the command line.
func shellArgs(shell []string, command string) []string {
	return append(append([]string(nil), shell[1:]...), command)
}

// runIn is a leaf: [exec.CommandContext] reports through its own fields. The
// command line is the last argument of shell. When the context ends, the
// process is killed and its error is the context's.
func runIn(shell []string) func(context.Context, Process) (ProcessOutput, error) {
	return func(ctx context.Context, p Process) (ProcessOutput, error) {
		var stdout, stderr bytes.Buffer

		cmd := exec.CommandContext(ctx, shell[0], shellArgs(shell, p.Command)...)
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

// createLog is a leaf: it creates a new log file in dir, and dir itself when
// it is missing.
func createLog(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.CreateTemp(dir, logPattern)
}

// discardLog closes and removes the log of a process that did not start.
func discardLog(log *os.File) {
	_ = log.Close()
	_ = os.Remove(log.Name())
}

// startIn is a leaf: [exec.Cmd] reports through its own fields, and a
// goroutine has to wait for the process. The process is not bound to the
// context, which only stops a start that has not happened yet. Standard
// output and standard error both go to a new log file in logDir. The process
// is in table from its start until it exits.
func startIn(shell []string, logDir string, table IORef[ProcessTable]) func(context.Context, Process) (BackgroundProcess, error) {
	return func(ctx context.Context, p Process) (BackgroundProcess, error) {
		if ctx.Err() != nil {
			return BackgroundProcess{}, ctx.Err()
		}
		log, err := createLog(logDir)
		if err != nil {
			return BackgroundProcess{}, err
		}

		cmd := exec.Command(shell[0], shellArgs(shell, p.Command)...)
		cmd.Dir = p.Dir
		cmd.Stdout = log
		cmd.Stderr = log

		if err := cmd.Start(); err != nil {
			discardLog(log)
			return BackgroundProcess{}, err
		}

		started := BackgroundProcess{Pid: cmd.Process.Pid, Command: p.Command, LogPath: log.Name()}
		ioref.Modify(R.UpsertAt(started.Pid, started))(table)()

		go func() {
			_ = cmd.Wait()
			_ = log.Close()
			ioref.Modify(R.DeleteAt[int, BackgroundProcess](started.Pid))(table)()
		}()

		return started, nil
	}
}

// StartShell builds a [StartProcess] that hands the command line to a shell,
// a program followed by its leading arguments, e.g. ["sh", "-c"], writes the
// output of each process to a new file in logDir and keeps the running
// processes in the table it reads.
func StartShell(shell []string, logDir string) Reader[IORef[ProcessTable], StartProcess] {
	return F.Flow2(
		F.Bind12of3(startIn)(shell, logDir),
		thunk.Eitherize1[func(context.Context, Process) (BackgroundProcess, error)],
	)
}
