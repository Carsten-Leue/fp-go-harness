package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRun records the processes it is asked to run and the time left until
// the deadline of the context, and answers with out, or fails with err.
type fakeRun struct {
	got      []Process
	out      ProcessOutput
	err      error
	deadline time.Duration
}

func (f *fakeRun) run(p Process) Thunk[ProcessOutput] {
	return func(ctx context.Context) func() result.Result[ProcessOutput] {
		return func() result.Result[ProcessOutput] {
			f.got = append(f.got, p)
			if d, ok := ctx.Deadline(); ok {
				f.deadline = time.Until(d)
			}
			if f.err != nil {
				return result.Left[ProcessOutput](f.err)
			}
			return result.Of(f.out)
		}
	}
}

func runExecuteCommand(t *testing.T, deps ExecuteCommandDeps, arguments string) result.Result[string] {
	t.Helper()

	return ExecuteCommand()(arguments)(deps)(t.Context())()
}

func jsonString(t *testing.T, s string) string {
	t.Helper()

	b, err := json.Marshal(s)
	require.NoError(t, err)
	return string(b)
}

func TestOutputText(t *testing.T) {
	for name, tc := range map[string]struct {
		out  ProcessOutput
		want string
	}{
		"stdout only":     {ProcessOutput{Stdout: "hello\r\n"}, "hello"},
		"stdout, stderr":  {ProcessOutput{Stdout: "ok\n", Stderr: "warning\n"}, "ok\n\nStderr:\nwarning"},
		"stderr only":     {ProcessOutput{Stderr: "warning"}, "Stderr:\nwarning"},
		"nothing":         {ProcessOutput{}, noOutput},
		"failed, stderr":  {ProcessOutput{Stderr: "GraphQL: exists", ExitCode: 1}, "Exit code: 1\n\nStderr:\nGraphQL: exists"},
		"failed, both":    {ProcessOutput{Stdout: "a\n", Stderr: "b\n", ExitCode: 1}, "Exit code: 1\n\nStdout:\na\n\nStderr:\nb"},
		"failed, nothing": {ProcessOutput{ExitCode: 2}, "Exit code: 2"},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, outputText()(tc.out))
		})
	}
}

func TestRenderOutput(t *testing.T) {
	assert.Equal(t, result.Of("ok"), renderOutput()(ProcessOutput{Stdout: "ok"}))

	_, err := result.Unwrap(renderOutput()(ProcessOutput{Stdout: "a", ExitCode: 1}))
	assert.EqualError(t, err, "Exit code: 1\n\nStdout:\na")
}

func TestTimeoutOf(t *testing.T) {
	assert.Equal(t, 30*time.Second, timeoutOf()(0))
	assert.Equal(t, 30*time.Second, timeoutOf()(-5))
	assert.Equal(t, 120*time.Second, timeoutOf()(120))
	assert.Equal(t, 270*time.Second, timeoutOf()(1000))
}

func TestTimeoutError(t *testing.T) {
	other := errors.New("other")
	assert.Equal(t, other, timeoutError()(time.Second)(other))

	err := timeoutError()(time.Second)(context.DeadlineExceeded)
	assert.ErrorContains(t, err, "command did not finish within 1s")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestExecuteCommand_Fake(t *testing.T) {
	root := t.TempDir()
	fake := &fakeRun{out: ProcessOutput{Stdout: "hello\n"}}
	deps := MakeExecuteCommandDeps(MakeWorkspaceDeps(root), MakeProcessDeps(fake.run))

	assert.Equal(t, result.Of("hello"), runExecuteCommand(t, deps, `{"command":"echo hello"}`))
	assert.Equal(t, result.Of("hello"), runExecuteCommand(t, deps, `{"command":"ls","cwd":"sub","timeout_seconds":120}`))

	// unlike the file tools, a working directory outside the workspace is allowed
	outside := filepath.Join(filepath.Dir(root), "other")
	assert.Equal(t, result.Of("hello"), runExecuteCommand(t, deps, `{"command":"ls","cwd":`+jsonString(t, outside)+`}`))

	assert.Equal(t, []Process{
		{Command: "echo hello", Dir: root},
		{Command: "ls", Dir: filepath.Join(root, "sub")},
		{Command: "ls", Dir: outside},
	}, fake.got)
}

func TestExecuteCommand_TimeoutInContext(t *testing.T) {
	fake := &fakeRun{}
	deps := MakeExecuteCommandDeps(MakeWorkspaceDeps(t.TempDir()), MakeProcessDeps(fake.run))

	runExecuteCommand(t, deps, `{"command":"x","timeout_seconds":120}`)
	assert.InDelta(t, float64(120*time.Second), float64(fake.deadline), float64(time.Second))

	runExecuteCommand(t, deps, `{"command":"x"}`)
	assert.InDelta(t, float64(30*time.Second), float64(fake.deadline), float64(time.Second))
}

func TestExecuteCommand_Failures(t *testing.T) {
	fake := &fakeRun{out: ProcessOutput{Stderr: "boom", ExitCode: 3}}
	deps := MakeExecuteCommandDeps(MakeWorkspaceDeps(t.TempDir()), MakeProcessDeps(fake.run))

	for name, tc := range map[string]struct {
		arguments string
		want      string
	}{
		"invalid json":  {`{`, "unexpected end of JSON input"},
		"empty command": {`{"command":""}`, "command must not be empty"},
		"exit code":     {`{"command":"x"}`, "Exit code: 3\n\nStderr:\nboom"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := result.Unwrap(runExecuteCommand(t, deps, tc.arguments))
			assert.ErrorContains(t, err, tc.want)
		})
	}

	// an infrastructure error, e.g. a missing shell, is passed on unchanged
	failing := &fakeRun{err: errors.New("cannot start")}
	failingDeps := MakeExecuteCommandDeps(MakeWorkspaceDeps(t.TempDir()), MakeProcessDeps(failing.run))
	_, err := result.Unwrap(runExecuteCommand(t, failingDeps, `{"command":"x"}`))
	assert.EqualError(t, err, "cannot start")
}

// shellCommands are command lines for the default shell of the operating system.
type shellCommands struct {
	stdoutAndStderr string
	sleep           string
	pwd             string
}

func commandsFor() shellCommands {
	if runtime.GOOS == "windows" {
		return shellCommands{
			stdoutAndStderr: "Write-Output out; [Console]::Error.WriteLine('err')",
			sleep:           "Start-Sleep -Seconds 10",
			pwd:             "(Get-Location).Path",
		}
	}
	return shellCommands{
		stdoutAndStderr: "echo out; echo err 1>&2",
		sleep:           "sleep 10",
		pwd:             "pwd",
	}
}

// TestExecuteCommand_OperatingSystem runs real commands in the default shell.
func TestExecuteCommand_OperatingSystem(t *testing.T) {
	if testing.Short() {
		t.Skip("starts shell processes")
	}

	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "sub"), 0o700))
	deps := MakeExecuteCommandDeps(MakeWorkspaceDeps(root), MakeDefaultProcessDeps())
	cmds := commandsFor()

	t.Run("stdout and stderr", func(t *testing.T) {
		assert.Equal(t, result.Of("out\n\nStderr:\nerr"),
			runExecuteCommand(t, deps, `{"command":`+jsonString(t, cmds.stdoutAndStderr)+`}`))
	})

	t.Run("cwd", func(t *testing.T) {
		out, err := result.Unwrap(runExecuteCommand(t, deps, `{"command":`+jsonString(t, cmds.pwd)+`,"cwd":"sub"}`))
		require.NoError(t, err)
		want, err := filepath.EvalSymlinks(filepath.Join(root, "sub"))
		require.NoError(t, err)
		got, err := filepath.EvalSymlinks(out)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("exit code", func(t *testing.T) {
		_, err := result.Unwrap(runExecuteCommand(t, deps, `{"command":"exit 3"}`))
		assert.EqualError(t, err, "Exit code: 3")
	})

	t.Run("timeout", func(t *testing.T) {
		start := time.Now()
		_, err := result.Unwrap(runExecuteCommand(t, deps, `{"command":`+jsonString(t, cmds.sleep)+`,"timeout_seconds":1}`))
		assert.ErrorIs(t, err, context.DeadlineExceeded)
		assert.ErrorContains(t, err, "command did not finish within 1s")
		assert.Less(t, time.Since(start), 8*time.Second)
	})

	t.Run("missing cwd", func(t *testing.T) {
		_, err := result.Unwrap(runExecuteCommand(t, deps, `{"command":"exit 0","cwd":"missing"}`))
		assert.Error(t, err)
	})
}

func TestRunShell_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := result.Unwrap(thunk.WithContext(RunShell()(DefaultShell())(MakeProcess("exit 0", t.TempDir())))(ctx)())
	assert.ErrorIs(t, err, context.Canceled)
}

func TestDefaultShell(t *testing.T) {
	shell := DefaultShell()
	if runtime.GOOS == "windows" {
		assert.Equal(t, "powershell.exe", shell[0])
	} else {
		assert.Equal(t, []string{"sh", "-c"}, shell)
	}
}

func TestMakeExecuteCommandTool(t *testing.T) {
	root := t.TempDir()
	fake := &fakeRun{out: ProcessOutput{Stdout: "hi"}}
	deps := MakeExecuteCommandDeps(MakeWorkspaceDeps(root), MakeProcessDeps(fake.run))

	registry := MakeToolRegistry(MakeExecuteCommandTool()(deps))

	call, ok := option.Unwrap(ToToolCaller()(registry)(ExecuteCommandName))
	require.True(t, ok)
	assert.Equal(t, "hi", runCall(t, call, `{"command":"echo hi"}`))

	definition := toolDefinition(registry[ExecuteCommandName])
	assert.Equal(t, ExecuteCommandName, definition.Name)
	assert.Equal(t, []string{"command"}, definition.Parameters["required"])

	properties := definition.Parameters["properties"].(map[string]any)
	assert.Len(t, properties, 3)
	assert.Contains(t, properties["cwd"].(map[string]any)["description"], root)
	assert.Equal(t, "integer", properties["timeout_seconds"].(map[string]any)["type"])
}

func TestAsExecuteCommandDeps(t *testing.T) {
	deps := MakeExecuteCommandDeps(MakeWorkspaceDeps(t.TempDir()), MakeDefaultProcessDeps())

	assert.Equal(t, deps, AsExecuteCommandDeps(deps))
	assert.Equal(t, ProcessDeps(deps), AsProcessDeps(deps))
}
