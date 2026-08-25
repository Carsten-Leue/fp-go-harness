package session

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	opt "github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSessionDeps is a minimal [SessionDeps] backed by a real
// [*openai.ChatCompletionService] pointed at a local test server, and a
// static tool registry.
type stubSessionDeps struct {
	svc      *openai.ChatCompletionService
	toolCall ToolCaller
}

func (d *stubSessionDeps) GetChatCompletionService() *openai.ChatCompletionService {
	return d.svc
}

func (d *stubSessionDeps) GetRequestOptions() Thunk[[]opt.RequestOption] {
	return thunk.FromLazy(A.Empty[opt.RequestOption])
}

func (d *stubSessionDeps) GetToolCaller() ToolCaller {
	return d.toolCall
}

// makeStubSessionDeps starts a test server that answers every chat
// completion request with the given canned response, and wires up registry
// as the tool caller. The server is closed automatically when the test
// completes.
func makeStubSessionDeps(t *testing.T, response openai.ChatCompletion, registry map[string]tools.ToolCall) SessionDeps {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(response))
	}))
	t.Cleanup(server.Close)

	client := openai.NewClient(
		opt.WithBaseURL(server.URL+"/"),
		opt.WithAPIKey("test-key"),
	)

	caller := F.Bind1st(record.MonadLookup[tools.ToolCall, string], registry)

	return &stubSessionDeps{
		svc:      &client.Chat.Completions,
		toolCall: caller,
	}
}

// runNext executes [Next] for the given session/deps pair and unwraps the
// outer [result.Result], failing the test if the effect itself errors.
func runNext(t *testing.T, session Session, deps SessionDeps) NextStep {
	t.Helper()

	step, err := result.Unwrap(Next()(session)(deps)(t.Context())())
	require.NoError(t, err)

	return step
}

// TestNext_LandsOnStop asserts that a completion with a "stop" finish
// reason lands immediately: the iteration counter and usage are updated,
// but history and the current request are left untouched since no tool
// round trip is needed.
func TestNext_LandsOnStop(t *testing.T) {
	req := openai.ChatCompletionNewParams{
		Model: "gpt-test",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("you are a helpful assistant"),
			openai.UserMessage("what's up?"),
		},
	}

	response := openai.ChatCompletion{
		ID:      "chatcmpl-stop",
		Object:  "chat.completion",
		Created: 1,
		Model:   "gpt-test",
		Choices: []openai.ChatCompletionChoice{
			{
				Index:        0,
				FinishReason: "stop",
				Message: openai.ChatCompletionMessage{
					Role:    "assistant",
					Content: "All done.",
				},
			},
		},
		Usage: openai.CompletionUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}

	deps := makeStubSessionDeps(t, response, nil)

	step := runNext(t, MakeSession(req), deps)

	require.True(t, step.Landed)

	gotSession := pair.Head(step.Land)
	gotCompletion := pair.Tail(step.Land)

	assert.Equal(t, 1, gotSession.iterations)
	assert.Equal(t, response.Usage, gotSession.usage)
	assert.Empty(t, gotSession.history)
	assert.Equal(t, req, gotSession.current)

	assert.Equal(t, response.ID, gotCompletion.ID)
	assert.Equal(t, "All done.", gotCompletion.Choices[0].Message.Content)
}

// TestNext_BouncesOnToolCalls asserts that a completion with a "tool_calls"
// finish reason bounces: the requested tool is executed, the
// assistant/tool message round trip is appended to the current request,
// the (request, response) pair is recorded in history, and the loop
// continues with the updated session rather than landing.
func TestNext_BouncesOnToolCalls(t *testing.T) {
	req := openai.ChatCompletionNewParams{
		Model: "gpt-test",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("you are a helpful assistant"),
			openai.UserMessage("what's the weather in Berlin?"),
		},
	}

	toolCall := openai.ChatCompletionMessageToolCallUnion{
		ID:   "call_1",
		Type: "function",
		Function: openai.ChatCompletionMessageFunctionToolCallFunction{
			Name:      "get_weather",
			Arguments: `{"city":"Berlin"}`,
		},
	}
	assistantMessage := openai.ChatCompletionMessage{
		Role:      "assistant",
		Content:   "let me check that for you",
		ToolCalls: []openai.ChatCompletionMessageToolCallUnion{toolCall},
	}

	response := openai.ChatCompletion{
		ID:      "chatcmpl-tools",
		Object:  "chat.completion",
		Created: 2,
		Model:   "gpt-test",
		Choices: []openai.ChatCompletionChoice{
			{
				Index:        0,
				FinishReason: "tool_calls",
				Message:      assistantMessage,
			},
		},
		Usage: openai.CompletionUsage{PromptTokens: 20, CompletionTokens: 8, TotalTokens: 28},
	}

	var weatherTool tools.ToolCall = func(arguments string) tools.Thunk[string] {
		return thunk.Of("sunny, 21C for " + arguments)
	}
	registry := map[string]tools.ToolCall{"get_weather": weatherTool}

	deps := makeStubSessionDeps(t, response, registry)

	step := runNext(t, MakeSession(req), deps)

	require.False(t, step.Landed)

	gotSession := step.Bounce

	assert.Equal(t, 1, gotSession.iterations)
	assert.Equal(t, response.Usage, gotSession.usage)

	require.Len(t, gotSession.history, 1)
	entry := gotSession.history[0]
	assert.Equal(t, req, pair.Head(entry))
	assert.Equal(t, response.ID, pair.Tail(entry).ID)

	require.Len(t, gotSession.current.Messages, 4)
	assert.Equal(t, req.Messages[0], gotSession.current.Messages[0])
	assert.Equal(t, req.Messages[1], gotSession.current.Messages[1])
	assert.Equal(t, assistantMessage.ToParam(), gotSession.current.Messages[2])

	toolMsg := gotSession.current.Messages[3]
	require.NotNil(t, toolMsg.OfTool)
	assert.Equal(t, toolCall.ID, toolMsg.OfTool.ToolCallID)
	assert.Equal(t, `sunny, 21C for {"city":"Berlin"}`, toolMsg.OfTool.Content.OfString.Value)
}
