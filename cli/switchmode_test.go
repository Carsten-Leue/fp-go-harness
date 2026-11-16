package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	nethttp "net/http"
	"strings"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/mode"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// switchModeCompletion calls switch_mode with the ID of a mode.
func switchModeCompletion(modeID string) *openai.ChatCompletion {
	arguments, _ := json.Marshal(mode.SwitchModeArgs{ModeID: modeID, Reason: "test"})

	return &openai.ChatCompletion{
		Object: "chat.completion",
		Choices: []openai.ChatCompletionChoice{{
			FinishReason: "tool_calls",
			Message: openai.ChatCompletionMessage{
				Role: "assistant",
				ToolCalls: []openai.ChatCompletionMessageToolCallUnion{{
					ID:       "call_switch",
					Type:     "function",
					Function: openai.ChatCompletionMessageFunctionToolCallFunction{Name: mode.SwitchModeName, Arguments: string(arguments)},
				}},
			},
		}},
	}
}

// textsOf are the texts of the messages of a role of a request.
func textsOf(req fakeRequest, role string) []string {
	var texts []string
	for _, m := range req.Messages {
		var text string
		if m.Role == role && json.Unmarshal(m.Content, &text) == nil {
			texts = append(texts, text)
		}
	}
	return texts
}

// modeSummary describes the mode of a request: the start of its system
// prompt, whether it offers write_file, read_file and switch_mode, the text
// of its last tool message and whether its last user message shows the mode
// name in current_mode.
func modeSummary(req fakeRequest, name string) string {
	system := textsOf(req, "system")
	toolTexts := append([]string{""}, textsOf(req, "tool")...)
	users := userTexts(req)
	role, _, _ := strings.Cut(strings.TrimPrefix(system[0], "<role_definition>\n"), "\n")

	return fmt.Sprintf("role=%s write=%v read=%v switch=%v tool=%s current=%v",
		role,
		hasTool(req, tools.WriteFileName),
		hasTool(req, tools.ReadFileName),
		hasTool(req, mode.SwitchModeName),
		toolTexts[len(toolTexts)-1],
		strings.Contains(users[len(users)-1], "<current_mode>\n"+name+"\n</current_mode>"),
	)
}

// runSwitchRepl runs the REPL in a mode over the lines a and b: the line a
// is answered with a switch to target, after the switch the line b is sent,
// and every request without a call is answered with its [modeSummary] for
// the name.
func runSwitchRepl(t *testing.T, start, target, name string) replOutput {
	t.Helper()

	lines := bufferedLines("a")
	chat := fakeServer(t, func(_ *nethttp.Request, req fakeRequest) *openai.ChatCompletion {
		last := req.Messages[len(req.Messages)-1]
		switch {
		case last.Role == "user" && lastUserLine(req) == "a":
			return switchModeCompletion(target)
		case last.Role == "tool":
			lines <- "b"
		case lastUserLine(req) == "b":
			close(lines)
		}
		return makeAnswerCompletion(modeSummary(req, name))
	})

	var errOut bytes.Buffer
	return runReplIn(t, t.Context(), start, chat, noInterrupts(), lines, &errOut, &errOut)
}

// TestRepl_SwitchModeAgentToPlan asserts that a switch_mode from agent to
// plan makes the next request carry the Plan system prompt and tools, with
// switch_mode still among them, and that the next line of the REPL stays in
// plan mode, with Plan as its current_mode.
func TestRepl_SwitchModeAgentToPlan(t *testing.T) {
	got := runSwitchRepl(t, mode.AgentID, mode.PlanID, "Plan")

	require.NoError(t, got.err)
	assert.Equal(t,
		"role=You are a software architect running in Plan mode. write=false read=true switch=true tool=Switched to mode \"plan\" current=false\n\n"+statsOf(2)+"\n"+
			"role=You are a software architect running in Plan mode. write=false read=true switch=true tool=Switched to mode \"plan\" current=true\n\n"+statsOf(1)+"\n",
		got.out)
}

// TestRepl_SwitchModeAskToAgentRefused asserts that a session that started
// in ask mode, with the read-only tools only, can't switch to agent mode:
// the call is a tool message (an error, JSON encoded) naming the tools agent
// needs, and the session stays in ask mode.
func TestRepl_SwitchModeAskToAgentRefused(t *testing.T) {
	got := runSwitchRepl(t, mode.AskID, mode.AgentID, "Ask")

	require.NoError(t, got.err)
	first, _, _ := strings.Cut(got.out, "\n")
	assert.Contains(t, first, "role=You are a coding assistant running in Ask mode. write=false read=true switch=true")
	assert.Contains(t, first, `switch to mode \"agent\" refused: it needs the tools write_file`)
	assert.Contains(t, got.out, "current=true")
}

// TestRepl_SwitchModeUnknown asserts that a switch to an unknown mode is a
// tool message naming the modes, and the session stays in its mode.
func TestRepl_SwitchModeUnknown(t *testing.T) {
	got := runSwitchRepl(t, mode.AgentID, "code", "Agent")

	require.NoError(t, got.err)
	first, _, _ := strings.Cut(got.out, "\n")
	assert.Contains(t, first, "role=You are a coding agent running in Agent mode. write=true read=true switch=true")
	assert.Contains(t, first, `unknown mode \"code\", use one of agent, plan, ask`)
}
