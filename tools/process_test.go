package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/IBM/fp-go/v2/ioref"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMakeProcess(t *testing.T) {
	assert.Equal(t, Process{Command: "ls", Dir: "/tmp"}, MakeProcess("ls", "/tmp"))
}

func TestMakeProcessDeps(t *testing.T) {
	fake := &fakeRun{out: ProcessOutput{Stdout: "x"}}
	start := &fakeStart{}
	table := ioref.MakeIORef(ProcessTable{})()
	deps := MakeProcessDeps(fake.run, start.start, table)

	assert.Equal(t, result.Of(ProcessOutput{Stdout: "x"}), deps.GetRunProcess()(MakeProcess("c", "d"))(t.Context())())
	assert.Equal(t, []Process{{Command: "c", Dir: "d"}}, fake.got)

	assert.Equal(t, result.Of(BackgroundProcess{Pid: 42, Command: "s", LogPath: filepath.Join("d", "command.log")}),
		deps.GetStartProcess()(MakeProcess("s", "d"))(t.Context())())
	assert.Equal(t, []Process{{Command: "s", Dir: "d"}}, start.got)

	assert.Same(t, table, deps.GetProcessTable())
}

func TestShellArgs(t *testing.T) {
	shell := []string{"sh", "-c"}

	assert.Equal(t, []string{"-c", "ls"}, shellArgs(shell, "ls"))
	assert.Equal(t, []string{"sh", "-c"}, shell)
}

func tableOf(table IORef[ProcessTable]) ProcessTable {
	return ioref.Read(table)()
}

func TestStartShell_NotFound(t *testing.T) {
	logDir := t.TempDir()
	table := ioref.MakeIORef(ProcessTable{})()
	start := StartShell([]string{"fp-go-harness-no-such-shell"}, logDir)(table)

	_, err := result.Unwrap(start(MakeProcess("exit 0", t.TempDir()))(t.Context())())
	assert.ErrorIs(t, err, exec.ErrNotFound)

	// the log of a process that did not start is removed
	entries, err := os.ReadDir(logDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
	assert.Empty(t, tableOf(table))
}

func TestStartShell_Cancelled(t *testing.T) {
	logDir := filepath.Join(t.TempDir(), "logs")
	table := ioref.MakeIORef(ProcessTable{})()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := result.Unwrap(StartShell(DefaultShell(), logDir)(table)(MakeProcess("exit 0", t.TempDir()))(ctx)())
	assert.ErrorIs(t, err, context.Canceled)
	assert.NoDirExists(t, logDir)
}

// TestStartShell_OperatingSystem starts real commands in the default shell.
func TestStartShell_OperatingSystem(t *testing.T) {
	if testing.Short() {
		t.Skip("starts shell processes")
	}

	cmds := commandsFor()

	t.Run("output goes to the log", func(t *testing.T) {
		logDir := filepath.Join(t.TempDir(), "logs")
		table := ioref.MakeIORef(ProcessTable{})()
		start := StartShell(DefaultShell(), logDir)(table)

		started, err := result.Unwrap(start(MakeProcess(cmds.stdoutAndStderr, t.TempDir()))(t.Context())())
		require.NoError(t, err)
		assert.Positive(t, started.Pid)
		assert.Equal(t, cmds.stdoutAndStderr, started.Command)
		assert.Equal(t, logDir, filepath.Dir(started.LogPath))

		// the process leaves the table when it exits
		assert.Eventually(t, func() bool { return len(tableOf(table)) == 0 }, 20*time.Second, 50*time.Millisecond)

		log, err := os.ReadFile(started.LogPath)
		require.NoError(t, err)
		assert.Contains(t, string(log), "out")
		assert.Contains(t, string(log), "err")
	})

	t.Run("running process is in the table", func(t *testing.T) {
		table := ioref.MakeIORef(ProcessTable{})()
		start := StartShell(DefaultShell(), t.TempDir())(table)

		started, err := result.Unwrap(start(MakeProcess(cmds.sleep, t.TempDir()))(t.Context())())
		require.NoError(t, err)
		assert.Equal(t, ProcessTable{started.Pid: started}, tableOf(table))

		// the process outlives the context of the call; stop it here
		p, err := os.FindProcess(started.Pid)
		require.NoError(t, err)
		require.NoError(t, p.Kill())
		assert.Eventually(t, func() bool { return len(tableOf(table)) == 0 }, 20*time.Second, 50*time.Millisecond)
	})
}

func TestMakeDefaultProcessDeps(t *testing.T) {
	deps := MakeDefaultProcessDeps(t.TempDir())()

	assert.Empty(t, tableOf(deps.GetProcessTable()))
	assert.NotSame(t, deps.GetProcessTable(), MakeDefaultProcessDeps(t.TempDir())().GetProcessTable())
}

func TestIsExit(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"exit error":         {&exec.ExitError{}, true},
		"wrapped exit error": {fmt.Errorf("run: %w", &exec.ExitError{}), true},
		"wait delay":         {exec.ErrWaitDelay, true},
		"not found":          {exec.ErrNotFound, false},
		"other":              {errors.New("other"), false},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, isExit(tc.err))
		})
	}
}

func TestRunShell_NotFound(t *testing.T) {
	run := RunShell()([]string{"fp-go-harness-no-such-shell"})

	_, err := result.Unwrap(run(MakeProcess("exit 0", t.TempDir()))(t.Context())())
	assert.ErrorIs(t, err, exec.ErrNotFound)
}

func TestRunShell_ExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("starts shell processes")
	}

	run := RunShell()(DefaultShell())

	assert.Equal(t, result.Of(ProcessOutput{ExitCode: 3}), run(MakeProcess("exit 3", t.TempDir()))(t.Context())())
}

func TestIsDeadlineExceeded(t *testing.T) {
	assert.True(t, isDeadlineExceeded()(context.DeadlineExceeded))
	assert.True(t, isDeadlineExceeded()(fmt.Errorf("wrapped: %w", context.DeadlineExceeded)))
	assert.False(t, isDeadlineExceeded()(context.Canceled))
}

func TestIsSuccess(t *testing.T) {
	assert.True(t, isSuccess(ProcessOutput{Stderr: "warning"}))
	assert.False(t, isSuccess(ProcessOutput{ExitCode: 1}))
}

func TestSection(t *testing.T) {
	assert.Equal(t, option.Some("L:\na\nb"), section("L:\n")("a\nb\r\n\r\n"))
	assert.Equal(t, option.None[string](), section("L:\n")("\r\n"))
	assert.Equal(t, option.None[string](), section("L:\n")(""))
}

func TestOutputSections(t *testing.T) {
	failed := ProcessOutput{Stdout: "o", Stderr: "e", ExitCode: 2}
	ok := ProcessOutput{Stdout: "o"}

	assert.Equal(t, option.Some("Exit code: 2"), exitSection()(failed))
	assert.Equal(t, option.None[string](), exitSection()(ok))

	assert.Equal(t, option.Some("Stdout:\no"), stdoutSection()(failed))
	assert.Equal(t, option.Some("o"), stdoutSection()(ok))

	assert.Equal(t, option.Some("Stderr:\ne"), stderrSection()(failed))
	assert.Equal(t, option.None[string](), stderrSection()(ok))
}

func TestWithTimeLimit(t *testing.T) {
	blocking := func(ctx context.Context) func() result.Result[ProcessOutput] {
		return func() result.Result[ProcessOutput] {
			<-ctx.Done()
			return result.Left[ProcessOutput](ctx.Err())
		}
	}

	_, err := result.Unwrap(withTimeLimit()(1)(blocking)(t.Context())())
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ErrorContains(t, err, "command did not finish within 1s")

	// other errors pass unchanged
	other := errors.New("other")
	failing := func(context.Context) func() result.Result[ProcessOutput] {
		return func() result.Result[ProcessOutput] { return result.Left[ProcessOutput](other) }
	}
	assert.Equal(t, result.Left[ProcessOutput](other), withTimeLimit()(1)(failing)(t.Context())())
}

func TestWorkingDir(t *testing.T) {
	root := t.TempDir()
	deps := MakeExecuteCommandDeps(MakeWorkspaceDeps(root), fakeProcessDeps((&fakeRun{}).run, (&fakeStart{}).start))
	outside := filepath.Join(filepath.Dir(root), "other")

	for name, tc := range map[string]struct {
		cwd  string
		want string
	}{
		"empty":    {"", root},
		"relative": {"sub", filepath.Join(root, "sub")},
		"absolute": {outside, outside},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, result.Of(tc.want), workingDir()(tc.cwd)(deps)(t.Context())())
		})
	}
}

func TestInvokeProcess(t *testing.T) {
	fake := &fakeRun{out: ProcessOutput{Stdout: "done"}}
	args := ExecuteCommandArgs{Command: "build", Cwd: "ignored", TimeoutSeconds: 60, Dir: "/work"}

	assert.Equal(t, result.Of(ProcessOutput{Stdout: "done"}), invokeProcess()(args)(fake.run)(t.Context())())
	assert.Equal(t, []Process{{Command: "build", Dir: "/work"}}, fake.got)
	assert.InDelta(t, float64(60*time.Second), float64(fake.deadline), float64(time.Second))
}
