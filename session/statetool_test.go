package session

import (
	"errors"
	"testing"
	"time"

	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeToolCallsCompletion is a completion that asks for one call of each named
// tool, in that order, with the call IDs call_<name>.
func makeToolCallsCompletion(id string, names ...string) openai.ChatCompletion {
	calls := A.Map(func(name string) openai.ChatCompletionMessageToolCallUnion {
		return openai.ChatCompletionMessageToolCallUnion{
			ID:   "call_" + name,
			Type: "function",
			Function: openai.ChatCompletionMessageFunctionToolCallFunction{
				Name:      name,
				Arguments: `{}`,
			},
		}
	})(names)

	return openai.ChatCompletion{
		ID:     id,
		Object: "chat.completion",
		Model:  "gpt-test",
		Choices: []openai.ChatCompletionChoice{
			{
				FinishReason: "tool_calls",
				Message: openai.ChatCompletionMessage{
					Role:      "assistant",
					ToolCalls: calls,
				},
			},
		},
		Usage: openai.CompletionUsage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
	}
}

// setModel is a fake session-changing tool: it sets the model of the
// session's request through a lens and answers text.
func setModel(model openai.ChatModel, text string) SessionCall {
	modelLens := MakeSessioncurrentLens().Compose(oai.MakeChatCompletionNewParamsModelLens())

	return F.Constant1[string](thunk.Of(pair.MakePair(modelLens.Set(model), text)))
}

// makeSessionCaller looks up session-changing tools in a registry.
func makeSessionCaller(registry map[string]SessionCall) ToolCaller {
	return F.Bind1st(record.MonadLookup[SessionCall, string], registry)
}

// toolMessageText is the text of the tool message at index i of a request.
func toolMessageText(t *testing.T, req openai.ChatCompletionNewParams, i int) string {
	t.Helper()

	require.Greater(t, len(req.Messages), i)
	require.NotNil(t, req.Messages[i].OfTool)

	return req.Messages[i].OfTool.Content.OfString.Value
}

// TestNext_SessionToolChangesNextRequest asserts that a session-changing tool
// changes the session, and the next request is built from the changed
// session: its model is the one the tool set.
func TestNext_SessionToolChangesNextRequest(t *testing.T) {
	responses := []openai.ChatCompletion{
		makeToolCallsCompletion("chatcmpl-1", "set_model"),
		makeStopCompletion("chatcmpl-2", "done"),
	}
	deps := makeSequencedSessionToolDeps(t, responses, makeSessionCaller(map[string]SessionCall{
		"set_model": setModel("gpt-changed", "model set"),
	}), 10)

	got := pair.Head(runAnswer(t, MakeSession(makeLoopRequest()), deps))

	require.Len(t, got.history, 2)
	assert.Equal(t, openai.ChatModel("gpt-test"), pair.Head(got.history[0]).Model)

	next := pair.Head(got.history[1])
	assert.Equal(t, openai.ChatModel("gpt-changed"), next.Model)
	// system, user, assistant, tool: the messages are appended before the change
	assert.Len(t, next.Messages, 4)
	assert.Equal(t, "model set", toolMessageText(t, next, 3))
}

// TestNext_SessionToolsApplyInCallOrder asserts that the changes of the tools
// apply in the order of the tool calls, not the order in which the concurrent
// calls finish: the first call waits until the second one has finished.
func TestNext_SessionToolsApplyInCallOrder(t *testing.T) {
	released := make(chan struct{})

	// first waits for second, so it finishes last; the wait is bounded so a
	// sequential traversal fails the test instead of hanging it
	first := func(arguments string) Thunk[ToolResult] {
		return F.Pipe1(
			thunk.FromIO(func() bool {
				select {
				case <-released:
					return true
				case <-time.After(5 * time.Second):
					return false
				}
			}),
			thunk.Chain(func(ok bool) Thunk[ToolResult] {
				if !ok {
					return thunk.Left[ToolResult](errors.New("second never finished"))
				}
				return setModel("gpt-first", "first done")(arguments)
			}),
		)
	}
	second := func(arguments string) Thunk[ToolResult] {
		return F.Pipe1(
			setModel("gpt-second", "second done")(arguments),
			thunk.ChainFirstIOK[ToolResult](func(ToolResult) func() F.Void {
				return func() F.Void {
					close(released)
					return F.VOID
				}
			}),
		)
	}

	responses := []openai.ChatCompletion{
		makeToolCallsCompletion("chatcmpl-1", "first", "second"),
		makeStopCompletion("chatcmpl-2", "done"),
	}
	deps := makeSequencedSessionToolDeps(t, responses, makeSessionCaller(map[string]SessionCall{
		"first":  first,
		"second": second,
	}), 10)

	next := pair.Head(pair.Head(runAnswer(t, MakeSession(makeLoopRequest()), deps)).history[1])

	assert.Equal(t, "first done", toolMessageText(t, next, 3))
	assert.Equal(t, "second done", toolMessageText(t, next, 4))
	// first is applied first, so the model of second wins
	assert.Equal(t, openai.ChatModel("gpt-second"), next.Model)
}

// TestNext_FailedSessionToolChangesNothing asserts that a failed
// session-changing tool and an unknown tool are tool messages with the error,
// and change nothing, while the other tools of the response still apply.
func TestNext_FailedSessionToolChangesNothing(t *testing.T) {
	failing := func(string) Thunk[ToolResult] {
		return thunk.Left[ToolResult](errors.New("tool broke"))
	}

	responses := []openai.ChatCompletion{
		makeToolCallsCompletion("chatcmpl-1", "set_model", "failing", "unknown"),
		makeStopCompletion("chatcmpl-2", "done"),
	}
	deps := makeSequencedSessionToolDeps(t, responses, makeSessionCaller(map[string]SessionCall{
		"set_model": setModel("gpt-changed", "model set"),
		"failing":   failing,
	}), 10)

	next := pair.Head(pair.Head(runAnswer(t, MakeSession(makeLoopRequest()), deps)).history[1])

	assert.Equal(t, openai.ChatModel("gpt-changed"), next.Model)
	assert.Equal(t, "model set", toolMessageText(t, next, 3))
	assert.Contains(t, toolMessageText(t, next, 4), "tool broke")
	assert.Contains(t, toolMessageText(t, next, 5), "Unable to find tool 'unknown'.")
}

// TestNext_FailedSessionToolAloneKeepsSession asserts that a response whose
// only tool fails leaves the model of the session as it was.
func TestNext_FailedSessionToolAloneKeepsSession(t *testing.T) {
	failing := func(string) Thunk[ToolResult] {
		return thunk.Left[ToolResult](errors.New("tool broke"))
	}

	responses := []openai.ChatCompletion{
		makeToolCallsCompletion("chatcmpl-1", "failing"),
		makeStopCompletion("chatcmpl-2", "done"),
	}
	deps := makeSequencedSessionToolDeps(t, responses, makeSessionCaller(map[string]SessionCall{
		"failing": failing,
	}), 10)

	next := pair.Head(pair.Head(runAnswer(t, MakeSession(makeLoopRequest()), deps)).history[1])

	assert.Equal(t, openai.ChatModel("gpt-test"), next.Model)
	assert.Contains(t, toolMessageText(t, next, 3), "tool broke")
}

// TestLiftRegistry_PlainToolChangesNothing asserts that a plain tool lifted
// into a session tool answers its text with the identity.
func TestLiftRegistry_PlainToolChangesNothing(t *testing.T) {
	lifted := tools.LiftRegistry[Session]()(tools.MakeToolRegistry(
		tools.MakeTool(openai.FunctionDefinitionParam{Name: "get_weather"}, makeWeatherRegistry()["get_weather"]),
	))

	got, err := result.Unwrap(lifted["get_weather"].Call(`x`)(t.Context())())
	require.NoError(t, err)

	s := MakeSession(makeLoopRequest())
	assert.Equal(t, "sunny, 21C for x", pair.Tail(got))
	assert.Equal(t, s, pair.Head(got)(s))
}
