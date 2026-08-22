package tools

import (
	"encoding/json"
	"errors"
	"testing"

	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
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
func runToolCall(t *testing.T, call openai.ChatCompletionMessageToolCallUnion, caller ToolCaller) openai.ChatCompletionMessageParamUnion {
	t.Helper()

	msg, err := result.Unwrap(MakeToolCall()(call)(caller)(t.Context())())
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

	msg := runToolCall(t, call, caller)

	require.NotNil(t, msg.OfTool)
	assert.Equal(t, call.ID, msg.OfTool.ToolCallID)
	assert.Equal(t, `sunny, 21C for {"city":"Berlin"}`, msg.OfTool.Content.OfString.Value)
}

func TestMakeToolCall_ToolNotFound(t *testing.T) {
	caller := makeToolCaller(map[string]ToolCall{})
	call := makeToolCallUnion("call_2", "unknown_tool", `{}`)

	msg := runToolCall(t, call, caller)

	require.NotNil(t, msg.OfTool)
	assert.Equal(t, call.ID, msg.OfTool.ToolCallID)

	errRes := decodeErrorResult(t, msg)
	assert.True(t, errRes.Error)
	assert.Equal(t, "Unable to find tool 'unknown_tool'.", errRes.Message)
}

func TestMakeToolCall_ToolCallError(t *testing.T) {
	var brokenTool ToolCall = func(_ string) Thunk[string] {
		return thunk.Left[string](errors.New("boom"))
	}

	caller := makeToolCaller(map[string]ToolCall{"broken_tool": brokenTool})
	call := makeToolCallUnion("call_3", "broken_tool", `{}`)

	msg := runToolCall(t, call, caller)

	require.NotNil(t, msg.OfTool)
	assert.Equal(t, call.ID, msg.OfTool.ToolCallID)

	errRes := decodeErrorResult(t, msg)
	assert.True(t, errRes.Error)
	assert.Equal(t, "boom", errRes.Message)
}
