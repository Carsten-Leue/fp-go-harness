package cli

import (
	"encoding/json"
	"fmt"
	nethttp "net/http"
	"strings"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/todo"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// todoListBlock is the todo list section of environment_details after the
// first turn of TestRepl_TodoListInNextUserMessage.
const todoListBlock = "<todo_list>\n[x] one\n[ ] two\n</todo_list>\n</environment_details>"

// userTexts are the texts of the user messages of a request.
func userTexts(req fakeRequest) []string {
	var texts []string
	for _, m := range req.Messages {
		var text string
		if m.Role == "user" && json.Unmarshal(m.Content, &text) == nil {
			texts = append(texts, text)
		}
	}
	return texts
}

// hasTool tells whether a request offers the named tool.
func hasTool(req fakeRequest, name string) bool {
	for _, tool := range req.Tools {
		if tool.Function.Name == name {
			return true
		}
	}
	return false
}

// updateTodoListCompletion calls update_todo_list with the checklist.
func updateTodoListCompletion(checklist string) *openai.ChatCompletion {
	arguments, _ := json.Marshal(todo.UpdateTodoListArgs{Todos: checklist})

	return &openai.ChatCompletion{
		Object: "chat.completion",
		Choices: []openai.ChatCompletionChoice{{
			FinishReason: "tool_calls",
			Message: openai.ChatCompletionMessage{
				Role: "assistant",
				ToolCalls: []openai.ChatCompletionMessageToolCallUnion{{
					ID:       "call_todo",
					Type:     "function",
					Function: openai.ChatCompletionMessageFunctionToolCallFunction{Name: todo.UpdateTodoListName, Arguments: string(arguments)},
				}},
			},
		}},
	}
}

// TestRepl_TodoListInNextUserMessage asserts that the ask mode offers
// update_todo_list, that a call of it in the first turn changes the
// session, and that the user message of the next line shows the list in its
// environment_details, while the first one, with an empty list, has none.
// The fake server answers from the request: the tool call for the line a,
// and for every other request what it saw. The line b is sent during the
// last request of the first turn, so it is the line of the next turn, as in
// TestRepl_LateLine, not a steering line.
func TestRepl_TodoListInNextUserMessage(t *testing.T) {
	lines := bufferedLines("a")
	chat := fakeServer(t, func(_ *nethttp.Request, req fakeRequest) *openai.ChatCompletion {
		last := req.Messages[len(req.Messages)-1]
		switch {
		case last.Role == "user" && lastUserLine(req) == "a":
			return updateTodoListCompletion("[x] one\n[ ] two")
		case last.Role == "tool":
			lines <- "b"
		case lastUserLine(req) == "b":
			close(lines)
		}
		texts := userTexts(req)
		return makeAnswerCompletion(fmt.Sprintf("tool=%v last=%s first=%v next=%v",
			hasTool(req, todo.UpdateTodoListName),
			last.Role,
			strings.Contains(texts[0], "<todo_list>"),
			strings.HasSuffix(texts[len(texts)-1], todoListBlock),
		))
	})

	got := runReplWith(t, t.Context(), chat, lines)

	require.NoError(t, got.err)
	assert.Equal(t, "tool=true last=tool first=false next=false\n\n"+statsOf(2)+"\n"+
		"tool=true last=user first=false next=true\n\n"+statsOf(1)+"\n", got.out)
}
