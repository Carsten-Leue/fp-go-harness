package mode

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/Carsten-Leue/fp-go-harness/prompt"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/io"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readOnlyToolNames are the tools of Plan and Ask mode.
var readOnlyToolNames = []string{tools.ReadFileName, tools.ListFilesName, tools.GlobName, tools.GrepName}

// allToolNames are the tools of Agent mode.
var allToolNames = []string{
	tools.ReadFileName, tools.ListFilesName, tools.GlobName, tools.GrepName,
	tools.WriteFileName, tools.SearchAndReplaceName, tools.ApplyDiffName, tools.InsertContentName,
	tools.ExecuteCommandName,
}

// makeTestDeps builds [ModeDeps] over a temporary workspace with the files,
// relative path to content, on a fixed machine and date.
func makeTestDeps(t *testing.T, files map[string]string) (string, ModeDeps) {
	t.Helper()

	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	}

	toolsDeps := tools.MakeDefaultToolsDeps(root, filepath.Join(root, "logs"))()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	env := prompt.MakeEnvInfoDeps(toolsDeps, "linux", "amd64", "sh", io.Of(now))

	return root, MakeModeDeps(toolsDeps, env)
}

func lookup(t *testing.T, id string) Mode {
	t.Helper()

	m, err := result.Unwrap(Lookup()(id))
	require.NoError(t, err)
	return m
}

func runRequest(t *testing.T, deps ModeDeps, m Mode) Params {
	t.Helper()

	request, err := effect.RunSync(effect.Provide[Params](deps)(Request()(m)))(t.Context())
	require.NoError(t, err)
	return request
}

// toolNames are the names of the tool definitions of a request.
func toolNames(request Params) []string {
	return A.Map(func(u openai.ChatCompletionToolUnionParam) string { return u.OfFunction.Function.Name })(request.Tools)
}

// tagsOf are the tags of the top-level sections of a rendered system prompt,
// in order.
func tagsOf(systemPrompt string) []string {
	return A.Map(func(m []string) string { return m[1] })(
		regexp.MustCompile(`(?m)^<(\w+)>$`).FindAllStringSubmatch(systemPrompt, -1))
}

func TestIDs(t *testing.T) {
	assert.Equal(t, []string{AgentID, PlanID, AskID}, IDs())
}

func TestLookup(t *testing.T) {
	for _, id := range IDs() {
		assert.Equal(t, id, lookup(t, id).ID)
	}

	_, err := result.Unwrap(Lookup()("code"))
	assert.EqualError(t, err, `unknown mode "code", use one of agent, plan, ask`)
}

func TestModesAllowedTools(t *testing.T) {
	assert.ElementsMatch(t, allToolNames, lookup(t, AgentID).AllowedTools)
	assert.ElementsMatch(t, readOnlyToolNames, lookup(t, PlanID).AllowedTools)
	assert.ElementsMatch(t, readOnlyToolNames, lookup(t, AskID).AllowedTools)
}

func TestTools(t *testing.T) {
	_, deps := makeTestDeps(t, nil)

	assert.ElementsMatch(t, allToolNames, R.Keys(Tools()(lookup(t, AgentID))(deps)))
	assert.ElementsMatch(t, readOnlyToolNames, R.Keys(Tools()(lookup(t, AskID))(deps)))
}

func TestRequestPlan(t *testing.T) {
	root, deps := makeTestDeps(t, map[string]string{
		"AGENTS.md":             "Use fp-go.",
		".bob/rules-plan/a.md":  "Plans list their tests.",
		".bob/rules-agent/a.md": "Agents run the tests.",
	})

	request := runRequest(t, deps, lookup(t, PlanID))

	require.Len(t, request.Messages, 1)
	system := request.Messages[0].OfSystem.Content.OfString.Value

	assert.Equal(t,
		[]string{"role_definition", "tool_use", "markdown_rules", "base_rules", "project_rules", "agents_md", "rule", "rule", "environment_info"},
		tagsOf(system))
	assert.Regexp(t, `^<role_definition>\nYou are a software architect running in Plan mode\.`, system)
	assert.Contains(t, system, "<rule>\nUse fp-go.\n</rule>")
	assert.Contains(t, system, "<rule>\nPlans list their tests.\n</rule>")
	assert.NotContains(t, system, "Agents run the tests.")
	assert.Contains(t, system, "The currently active workspace folder is "+root+"\n")
	assert.Regexp(t, `Current date: 2026-10-19\n</environment_info>$`, system)
	assert.NotContains(t, system, "\r")

	assert.ElementsMatch(t, readOnlyToolNames, toolNames(request))
}

func TestRequestAgentWithoutRules(t *testing.T) {
	_, deps := makeTestDeps(t, nil)

	request := runRequest(t, deps, lookup(t, AgentID))

	system := request.Messages[0].OfSystem.Content.OfString.Value
	assert.Equal(t,
		[]string{"role_definition", "tool_use", "markdown_rules", "base_rules", "environment_info"},
		tagsOf(system))
	assert.Regexp(t, `^<role_definition>\nYou are a coding agent running in Agent mode\.`, system)

	assert.ElementsMatch(t, allToolNames, toolNames(request))
}

func TestRequestAsk(t *testing.T) {
	_, deps := makeTestDeps(t, nil)

	request := runRequest(t, deps, lookup(t, AskID))

	assert.Regexp(t, `^<role_definition>\nYou are a coding assistant running in Ask mode\.`,
		request.Messages[0].OfSystem.Content.OfString.Value)
	assert.ElementsMatch(t, readOnlyToolNames, toolNames(request))
}
