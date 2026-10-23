package mode

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"testing/fstest"
	"time"

	"github.com/Carsten-Leue/fp-go-harness/prompt"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/pair"
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

// writeToolNames are the tools of Agent mode only.
var writeToolNames = []string{
	tools.WriteFileName, tools.SearchAndReplaceName, tools.ApplyDiffName, tools.InsertContentName,
	tools.ExecuteCommandName,
}

// makeTestDeps builds [ModeDeps] over a temporary workspace with the files,
// relative path to content, on a fixed machine and date.
func makeTestDeps(t *testing.T, files Record[string, string]) (string, ModeDeps) {
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

	return root, MakeModeDeps(toolsDeps, env, Modes())
}

func lookup(t *testing.T, id string) Mode {
	t.Helper()

	m, err := result.Unwrap(Lookup()(MakeModeTableDeps(Modes()))(id))
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
	assert.Equal(t, []string{AgentID, PlanID, AskID}, IDs()(MakeModeTableDeps(Modes())))
}

func TestLookup(t *testing.T) {
	for _, id := range IDs()(MakeModeTableDeps(Modes())) {
		assert.Equal(t, id, lookup(t, id).ID)
	}

	_, err := result.Unwrap(Lookup()(MakeModeTableDeps(Modes()))("code"))
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
	root, deps := makeTestDeps(t, Record[string, string]{
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
	for _, name := range writeToolNames {
		assert.NotContains(t, system, name)
	}

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
	assert.Contains(t, system, "over write_file for existing files.\n")
	assert.Contains(t, system, "run the validation that applies to your changes with execute_command")

	assert.ElementsMatch(t, allToolNames, toolNames(request))
}

func TestRequestAsk(t *testing.T) {
	_, deps := makeTestDeps(t, nil)

	request := runRequest(t, deps, lookup(t, AskID))

	assert.Regexp(t, `^<role_definition>\nYou are a coding assistant running in Ask mode\.`,
		request.Messages[0].OfSystem.Content.OfString.Value)
	assert.ElementsMatch(t, readOnlyToolNames, toolNames(request))
}

func TestModesFromFrontMatter(t *testing.T) {
	assert.Equal(t, []string{"Agent", "Plan", "Ask"}, A.Map(func(m Mode) string { return m.Name })(Modes()))
	assert.Equal(t, "Answers questions with the read-only tools.", lookup(t, AskID).Description)
	assert.Regexp(t, `^You are a coding assistant running in Ask mode\.\r?\n`, lookup(t, AskID).Prompt)
}

func TestParseMode(t *testing.T) {
	file := "---\r\nid: review\r\nname: Review\r\ndescription: Reviews changes.\r\norder: 4\r\ntools: [read_file, grep]\r\n---\r\n\r\nYou review.\r\n"

	assert.Equal(t, result.Of(Mode{
		ID:           "review",
		Name:         "Review",
		Description:  "Reviews changes.",
		Order:        4,
		AllowedTools: []string{tools.ReadFileName, tools.GrepName},
		Prompt:       "You review.",
	}), ParseMode()([]byte(file)))
}

func TestParseModeErrors(t *testing.T) {
	cases := Record[string, string]{
		"no front matter": "You review.\n",
		"unknown key":     "---\nid: review\ntool: [grep]\n---\nYou review.\n",
		"no id":           "---\nname: Review\n---\nYou review.\n",
		"unclosed":        "---\nid: review\nYou review.\n",
		"empty":           "---\n---\nYou review.\n",
		"not yaml":        "---\nid: [review\n---\nYou review.\n",
	}

	for name, file := range cases {
		_, err := result.Unwrap(ParseMode()([]byte(file)))
		assert.Error(t, err, name)
	}

	_, err := result.Unwrap(ParseMode()([]byte("---\nid: review\ntools: [grep, rm, curl]\n---\nYou review.\n")))
	assert.EqualError(t, err, `mode "review" allows unknown tools rm, curl`)
}

func TestReadModes(t *testing.T) {
	fsys := fstest.MapFS{
		"b.md":      {Data: []byte("---\nid: b\norder: 1\n---\nB\n")},
		"a.md":      {Data: []byte("---\nid: a\norder: 2\n---\nA\n")},
		"c.md":      {Data: []byte("---\nid: c\norder: 1\n---\nC\n")},
		"notes.txt": {Data: []byte("not a mode")},
	}

	modes, err := result.Unwrap(ReadModes()(fsys)())
	require.NoError(t, err)
	assert.Equal(t, []string{"b", "c", "a"}, A.Map(func(m Mode) string { return m.ID })(modes))

	fsys["bad.md"] = &fstest.MapFile{Data: []byte("---\nid: bad\ntools: [rm]\n---\n")}
	_, err = result.Unwrap(ReadModes()(fsys)())
	assert.EqualError(t, err, `mode file "bad.md", Caused By: mode "bad" allows unknown tools rm`)
}

func TestSplitFrontMatter(t *testing.T) {
	cases := Record[string, struct{ file, head, body string }]{
		"lf":                 {"---\nid: a\n---\nBody\n", "id: a\n", "Body\n"},
		"crlf":               {"---\r\nid: a\r\n---\r\n\r\nBody\r\n", "id: a\r\n", "\r\nBody\r\n"},
		"empty front matter": {"---\n---\nBody", "", "Body"},
		"no body":            {"---\nid: a\n---", "id: a\n", ""},
		"delimiter in body":  {"---\nid: a\n---\nBody\n---\nMore\n", "id: a\n", "Body\n---\nMore\n"},
		"spaces":             {"--- \nid: a\n  ---\nBody", "id: a\n", "Body"},
	}

	for name, tc := range cases {
		assert.Equal(t, result.Of(pair.MakePair(tc.head, tc.body)), SplitFrontMatter()(tc.file), name)
	}

	broken := Record[string, string]{
		"empty":           "",
		"no front matter": "Body\n---\n",
		"unclosed":        "---\nid: a\nBody\n",
		"only opening":    "---",
		"not first line":  "\n---\nid: a\n---\n",
	}

	for name, file := range broken {
		_, err := result.Unwrap(SplitFrontMatter()(file))
		assert.Error(t, err, name)
	}
}
