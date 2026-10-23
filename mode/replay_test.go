package mode

import (
	"path/filepath"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/prompt"
	"github.com/Carsten-Leue/fp-go-harness/replay"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/iterator/iterresult"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordedSystemPrompt is the system prompt of the first request in a log
// under testdata.
func recordedSystemPrompt(t *testing.T, name string) string {
	t.Helper()

	prompts, err := result.Unwrap(F.Pipe3(
		filepath.Join("testdata", name),
		replay.ReadLogFile(),
		replay.SystemPrompts(),
		iterresult.Collect[string],
	)())
	require.NoError(t, err)
	require.NotEmpty(t, prompts)
	return prompts[0]
}

// TestSystemPromptAgainstRecording compares the assembled Agent prompt with
// a recorded one, section by section. The recording is synthetic (the real
// ones are not in the repository, see B5) and has the tags of the recorded
// system prompts in their order. Missing lists the sections that later steps
// add: available_skills (H1) and available_modes (H5) among them.
func TestSystemPromptAgainstRecording(t *testing.T) {
	_, deps := makeTestDeps(t, Record[string, string]{"AGENTS.md": "Use fp-go."})

	assembled, err := effect.RunSync(effect.Provide[string](deps)(SystemPrompt()(lookup(t, AgentID))))(t.Context())
	require.NoError(t, err)

	diff := prompt.CompareSections()(pair.MakePair(recordedSystemPrompt(t, "recorded-system-prompt.log"), assembled))

	assert.Equal(t, []string{
		"investigate_before_answering", "engineering_discipline", "auto_appended_context",
		"available_skills", "user_custom_instructions", "available_modes",
	}, diff.Missing)
	assert.Empty(t, diff.Extra)
	assert.Equal(t,
		A.From("role_definition", "tool_use", "markdown_rules", "base_rules", "project_rules", "environment_info"),
		diff.Assembled)
	assert.Equal(t, diff.Recorded, diff.Assembled, "the sections both prompts have are in the recorded order")
}
