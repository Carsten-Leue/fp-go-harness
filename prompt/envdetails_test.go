package prompt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/ioref"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// goldenEnvDetails is the user message of TestEnvironmentDetailsGolden.
var goldenEnvDetails = filepath.Join("testdata", "environment_details.golden")

// gitStatusOutput is a git status of a branch with one changed file.
const gitStatusOutput = "## main...origin/main\r\n M cli/run.go\r\n"

// fakeRunProcess answers the git status in testRoot with out, every other
// process with an error. It is read-only, as the deps of an effect must be.
func fakeRunProcess(out result.Result[tools.ProcessOutput]) tools.RunProcess {
	return func(p tools.Process) Thunk[tools.ProcessOutput] {
		if p != tools.MakeProcess(GitStatusCommand, testRoot) {
			return thunk.Left[tools.ProcessOutput](errors.New("unexpected process"))
		}
		return thunk.FromResult(out)
	}
}

func testEnvDetailsDeps(out result.Result[tools.ProcessOutput]) EnvDetailsDeps {
	processes := tools.MakeProcessDeps(fakeRunProcess(out), nil, ioref.MakeIORef(tools.ProcessTable{})())
	now := time.Date(2026, time.October, 21, 14, 37, 30, 431_000_000, time.FixedZone("CEST", 2*60*60))

	return MakeEnvDetailsDeps(tools.MakeWorkspaceDeps(testRoot), processes, io.Of(now))
}

// wrapWith wraps text with the environment_details of the mode.
func wrapWith(deps EnvDetailsDeps, mode, text string) result.Result[string] {
	return result.Map(func(e Endomorphism[string]) string { return e(text) })(
		effect.Provide[Endomorphism[string]](deps)(EnvironmentDetails()(mode))(context.Background())(),
	)
}

func TestEnvironmentDetails(t *testing.T) {
	deps := testEnvDetailsDeps(result.Of(tools.ProcessOutput{Stdout: gitStatusOutput}))

	assert.Equal(t,
		result.Of("Fix the bug\n\n"+
			"<environment_details>\n"+
			"<current_datetime>\n2026-10-21T12:37:30.431Z\n</current_datetime>\n"+
			"<current_mode>\nAgent\n</current_mode>\n"+
			"<git_status_snapshot>\n```\n## main...origin/main\n M cli/run.go\n```\n</git_status_snapshot>\n"+
			"</environment_details>"),
		wrapWith(deps, "Agent", "Fix the bug"),
	)
}

func TestEnvironmentDetailsGolden(t *testing.T) {
	deps := testEnvDetailsDeps(result.Of(tools.ProcessOutput{Stdout: gitStatusOutput}))

	golden, err := os.ReadFile(goldenEnvDetails)
	require.NoError(t, err)

	assert.Equal(t,
		result.Of(strings.TrimRight(strings.ReplaceAll(string(golden), "\r\n", "\n"), "\n")),
		wrapWith(deps, "Agent", "Please implement the next step of the plan."),
	)
}

func TestEnvironmentDetailsOutsideRepository(t *testing.T) {
	deps := testEnvDetailsDeps(result.Of(tools.ProcessOutput{Stderr: "fatal: not a git repository", ExitCode: 128}))

	assert.Equal(t,
		result.Of("hi\n\n"+
			"<environment_details>\n"+
			"<current_datetime>\n2026-10-21T12:37:30.431Z\n</current_datetime>\n"+
			"<current_mode>\nAsk\n</current_mode>\n"+
			"</environment_details>"),
		wrapWith(deps, "Ask", "hi"),
	)
}

func TestEnvironmentDetailsCleanTree(t *testing.T) {
	deps := testEnvDetailsDeps(result.Of(tools.ProcessOutput{Stdout: "## main\n"}))

	assert.Regexp(t, "<git_status_snapshot>\n```\n## main\n```\n</git_status_snapshot>\n",
		result.GetOrElse(error.Error)(wrapWith(deps, "Agent", "hi")))
}

func TestEnvironmentDetailsGitMissing(t *testing.T) {
	missing := errors.New("executable file not found")
	deps := testEnvDetailsDeps(result.Left[tools.ProcessOutput](missing))

	assert.Equal(t, result.Left[string](missing), wrapWith(deps, "Agent", "hi"))
}
