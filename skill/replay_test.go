package skill

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Carsten-Leue/fp-go-harness/mode"
	"github.com/Carsten-Leue/fp-go-harness/prompt"
	"github.com/Carsten-Leue/fp-go-harness/replay"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/iterator/iterresult"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeModeDeps builds the deps of the built-in modes over a temporary
// workspace with an AGENTS.md, sharing skills with every mode.
func makeModeDeps(t *testing.T, skills []Skill) (string, mode.ModeDeps) {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Use fp-go."), 0o644))

	toolsDeps := tools.MakeDefaultToolsDeps(root, filepath.Join(root, "logs"))()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	env := prompt.MakeEnvInfoDeps(toolsDeps, "linux", "amd64", "sh", io.Of(now))

	return root, mode.MakeModeDeps(toolsDeps, env, mode.Modes(), MakeSharedDeps()(skills))
}

// recordedSystemPrompt is the system prompt of the first request in a log
// under the testdata of the mode package.
func recordedSystemPrompt(t *testing.T, name string) string {
	t.Helper()

	prompts, err := result.Unwrap(F.Pipe3(
		filepath.Join("..", "mode", "testdata", name),
		replay.ReadLogFile(),
		replay.SystemPrompts(),
		iterresult.Collect[string],
	)())
	require.NoError(t, err)
	require.NotEmpty(t, prompts)
	return prompts[0]
}

// TestSystemPromptAgainstRecording compares the assembled Agent prompt, with
// skills, with a recorded one, section by section (D7). The recording is
// synthetic (the real ones are not in the repository, see B5) and has the
// tags of the recorded system prompts in their order. Missing lists the
// sections that later steps add; the available_skills section (H1) and the
// available_modes section (H5), the last one, are there.
func TestSystemPromptAgainstRecording(t *testing.T) {
	_, deps := makeModeDeps(t, testSkills())

	agent, err := result.Unwrap(mode.Lookup()(mode.MakeModeTableDeps(mode.Modes()))(mode.AgentID))
	require.NoError(t, err)
	assembled, err := effect.RunSync(effect.Provide[string](deps)(mode.SystemPrompt()(agent)))(t.Context())
	require.NoError(t, err)

	diff := prompt.CompareSections()(pair.MakePair(recordedSystemPrompt(t, "recorded-system-prompt.log"), assembled))

	assert.Equal(t, []string{
		"investigate_before_answering", "engineering_discipline", "auto_appended_context",
		"user_custom_instructions",
	}, diff.Missing)
	assert.Empty(t, diff.Extra)
	assert.Equal(t,
		A.From("role_definition", "tool_use", "markdown_rules", "base_rules", AvailableSkillsTag, "project_rules", "environment_info", mode.AvailableModesTag),
		diff.Assembled)
	assert.Equal(t, diff.Recorded, diff.Assembled, "the sections both prompts have are in the recorded order")
}
