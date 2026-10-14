package prompt

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
)

// testRoot is a workspace root in the separators of the operating system,
// because [tools.MakeWorkspaceDeps] cleans the root.
var testRoot = filepath.FromSlash("/work/repo")

func fixedNow() time.Time {
	return time.Date(2026, time.October, 14, 23, 59, 0, 0, time.UTC)
}

func testEnvInfoDeps(now IO[time.Time]) EnvInfoDeps {
	return MakeEnvInfoDeps(tools.MakeWorkspaceDeps(testRoot), "linux", "amd64", "sh", now)
}

// renderEnvInfoWith runs [EnvironmentInfo] with deps and renders the section.
func renderEnvInfoWith(deps EnvInfoDeps) result.Result[string] {
	return result.Map(RenderSection())(
		effect.Provide[Section](deps)(EnvironmentInfo())(context.Background())(),
	)
}

func TestEnvironmentInfo(t *testing.T) {
	assert.Equal(t,
		result.Of(MakeSection(EnvironmentInfoTag,
			"### Workspace Info\n"+
				"The currently active workspace folder is "+testRoot+"\n"+
				"Use file paths relative to this directory.\n"+
				"### Command Execution Environment\n"+
				"Platform: linux\n"+
				"Architecture: amd64\n"+
				"Shell: sh\n"+
				"Current date: 2026-10-14")),
		effect.Provide[Section](testEnvInfoDeps(io.Of(fixedNow())))(EnvironmentInfo())(context.Background())(),
	)
}

func TestEnvironmentInfoRender(t *testing.T) {
	assert.Equal(t,
		result.Of("<environment_info>\n"+
			"### Workspace Info\n"+
			"The currently active workspace folder is "+testRoot+"\n"+
			"Use file paths relative to this directory.\n"+
			"### Command Execution Environment\n"+
			"Platform: linux\n"+
			"Architecture: amd64\n"+
			"Shell: sh\n"+
			"Current date: 2026-10-14\n"+
			"</environment_info>"),
		renderEnvInfoWith(testEnvInfoDeps(io.Of(fixedNow()))),
	)
}

func TestEnvironmentInfoReadsClockEachRun(t *testing.T) {
	calls := 0
	clock := func() time.Time {
		calls++
		return fixedNow().AddDate(0, 0, calls)
	}
	deps := testEnvInfoDeps(clock)

	assert.Regexp(t, `Current date: 2026-10-15\n`, result.GetOrElse(error.Error)(renderEnvInfoWith(deps)))
	assert.Regexp(t, `Current date: 2026-10-16\n`, result.GetOrElse(error.Error)(renderEnvInfoWith(deps)))
}

func TestMakeDefaultEnvInfoDeps(t *testing.T) {
	deps := MakeDefaultEnvInfoDeps(tools.MakeWorkspaceDeps("/w"), "bash")

	assert.Equal(t, runtime.GOOS, deps.GetOS())
	assert.Equal(t, runtime.GOARCH, deps.GetArch())
	assert.Equal(t, "bash", deps.GetShell())
	assert.WithinDuration(t, time.Now(), deps.GetNow()(), time.Minute)
}
