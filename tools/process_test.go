package tools

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
)

func TestMakeProcess(t *testing.T) {
	assert.Equal(t, Process{Command: "ls", Dir: "/tmp"}, MakeProcess("ls", "/tmp"))
}

func TestMakeProcessDeps(t *testing.T) {
	fake := &fakeRun{out: ProcessOutput{Stdout: "x"}}
	deps := MakeProcessDeps(fake.run)

	assert.Equal(t, result.Of(ProcessOutput{Stdout: "x"}), deps.GetRunProcess()(MakeProcess("c", "d"))(t.Context())())
	assert.Equal(t, []Process{{Command: "c", Dir: "d"}}, fake.got)
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
	deps := MakeExecuteCommandDeps(MakeWorkspaceDeps(root), MakeProcessDeps((&fakeRun{}).run))
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
