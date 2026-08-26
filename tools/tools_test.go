package tools

import (
	"encoding/json"
	"errors"
	"testing"

	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeToolCallUnion builds a synthetic function-tool-call as it would arrive
// from the OpenAI API.
func makeToolCallUnion(id, name, arguments string) openai.ChatCompletionMessageToolCallUnion {
	return openai.ChatCompletionMessageToolCallUnion{
		ID:   id,
		Type: "function",
		Function: openai.ChatCompletionMessageFunctionToolCallFunction{
			Name:      name,
			Arguments: arguments,
		},
	}
}

// makeToolCaller turns a plain registry of tools into a [ToolCaller], mirroring
// the record based dependency injection pattern used throughout fp-go.
func makeToolCaller(registry map[string]ToolCall) ToolCaller {
	return F.Bind1st(record.MonadLookup[ToolCall, string], registry)
}

// runToolCall executes [MakeToolCall] for the given call/registry pair and
// unwraps the outer [Result], failing the test if the effect itself errors
// (it shouldn't, since MakeToolCall recovers every failure into a message).
func runToolCall(t *testing.T, call openai.ChatCompletionMessageToolCallUnion, deps ToolDeps) openai.ChatCompletionMessageParamUnion {
	t.Helper()

	msg, err := result.Unwrap(MakeToolCall()(call)(deps)(t.Context())())
	require.NoError(t, err)

	return msg
}

// decodeErrorResult extracts the tool message content and decodes it as the
// package-private error envelope produced by makeErrorResult.
func decodeErrorResult(t *testing.T, msg openai.ChatCompletionMessageParamUnion) errorResult {
	t.Helper()

	require.NotNil(t, msg.OfTool)

	var res errorResult
	require.NoError(t, json.Unmarshal([]byte(msg.OfTool.Content.OfString.Value), &res))

	return res
}

func TestMakeToolCall_Success(t *testing.T) {
	var weatherTool ToolCall = func(arguments string) Thunk[string] {
		return thunk.Of("sunny, 21C for " + arguments)
	}

	caller := makeToolCaller(map[string]ToolCall{"get_weather": weatherTool})
	call := makeToolCallUnion("call_1", "get_weather", `{"city":"Berlin"}`)
	deps := MakeToolDeps(caller)

	msg := runToolCall(t, call, deps)

	require.NotNil(t, msg.OfTool)
	assert.Equal(t, call.ID, msg.OfTool.ToolCallID)
	assert.Equal(t, `sunny, 21C for {"city":"Berlin"}`, msg.OfTool.Content.OfString.Value)
}

func TestMakeToolCall_ToolNotFound(t *testing.T) {
	caller := makeToolCaller(map[string]ToolCall{})
	call := makeToolCallUnion("call_2", "unknown_tool", `{}`)
	deps := MakeToolDeps(caller)

	msg := runToolCall(t, call, deps)

	require.NotNil(t, msg.OfTool)
	assert.Equal(t, call.ID, msg.OfTool.ToolCallID)

	errRes := decodeErrorResult(t, msg)
	assert.True(t, errRes.Error)
	assert.Equal(t, "Unable to find tool 'unknown_tool'.", errRes.Message)
}

func TestMakeToolCall_ToolCallError(t *testing.T) {
	var brokenTool ToolCall = effect.Fail[string, string](errors.New("boom"))

	caller := makeToolCaller(map[string]ToolCall{"broken_tool": brokenTool})
	call := makeToolCallUnion("call_3", "broken_tool", `{}`)
	deps := MakeToolDeps(caller)

	msg := runToolCall(t, call, deps)

	require.NotNil(t, msg.OfTool)
	assert.Equal(t, call.ID, msg.OfTool.ToolCallID)

	errRes := decodeErrorResult(t, msg)
	assert.True(t, errRes.Error)
	assert.Equal(t, "boom", errRes.Message)
}

// runHandleToolCalls executes [HandleToolCalls] for the given completion/registry
// pair and unwraps the outer [Result], failing the test if the effect itself
// errors.
func runHandleToolCalls(t *testing.T, completion *openai.ChatCompletion, deps ToolDeps) Endomorphism[openai.ChatCompletionNewParams] {
	t.Helper()

	endo, err := result.Unwrap(HandleToolCalls()(completion)(deps)(t.Context())())
	require.NoError(t, err)

	return endo
}

// TestHandleToolCalls_NoToolCalls asserts that a completion whose choices carry
// no tool calls yields the identity endomorphism: the params passed in come back
// unchanged.
func TestHandleToolCalls_NoToolCalls(t *testing.T) {
	caller := makeToolCaller(map[string]ToolCall{})
	deps := MakeToolDeps(caller)

	completion := &openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{
			{
				Message: openai.ChatCompletionMessage{
					Content: "no tools needed here",
				},
			},
		},
	}

	endo := runHandleToolCalls(t, completion, deps)

	original := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("you are a helpful assistant"),
			openai.UserMessage("what's up?"),
		},
	}

	assert.Equal(t, original, endo(original))
}

// TestHandleToolCalls_WithToolCalls asserts that a completion whose choice
// carries tool calls follows the tool call protocol: the assistant message is
// appended as-is (converted via [openai.ChatCompletionMessage.ToParam]),
// followed by one tool message per tool call, each carrying its result.
func TestHandleToolCalls_WithToolCalls(t *testing.T) {
	weatherCall := makeToolCallUnion("call_1", "get_weather", `{"city":"Berlin"}`)
	timeCall := makeToolCallUnion("call_2", "get_time", `{"city":"Berlin"}`)

	message := openai.ChatCompletionMessage{
		Content:   "let me check that for you",
		ToolCalls: []openai.ChatCompletionMessageToolCallUnion{weatherCall, timeCall},
	}

	completion := &openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{
			{Message: message},
		},
	}

	var weatherTool ToolCall = func(arguments string) Thunk[string] {
		return thunk.Of("sunny, 21C for " + arguments)
	}
	var timeTool ToolCall = func(arguments string) Thunk[string] {
		return thunk.Of("14:00 for " + arguments)
	}

	caller := makeToolCaller(map[string]ToolCall{
		"get_weather": weatherTool,
		"get_time":    timeTool,
	})
	deps := MakeToolDeps(caller)

	endo := runHandleToolCalls(t, completion, deps)

	original := openai.ChatCompletionNewParams{
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("you are a helpful assistant"),
		},
	}

	updated := endo(original)
	require.Len(t, updated.Messages, 4)

	// The original messages are preserved, in order.
	assert.Equal(t, original.Messages[0], updated.Messages[0])

	// The tool call message is appended as-is (as an assistant message).
	assert.Equal(t, message.ToParam(), updated.Messages[1])

	// Each tool result is appended as its own tool message, in call order.
	require.NotNil(t, updated.Messages[2].OfTool)
	assert.Equal(t, weatherCall.ID, updated.Messages[2].OfTool.ToolCallID)
	assert.Equal(t, `sunny, 21C for {"city":"Berlin"}`, updated.Messages[2].OfTool.Content.OfString.Value)

	require.NotNil(t, updated.Messages[3].OfTool)
	assert.Equal(t, timeCall.ID, updated.Messages[3].OfTool.ToolCallID)
	assert.Equal(t, `14:00 for {"city":"Berlin"}`, updated.Messages[3].OfTool.Content.OfString.Value)
}
