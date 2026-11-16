package tools

import (
	"errors"
	"strings"
	"testing"

	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	F "github.com/IBM/fp-go/v2/function"
	N "github.com/IBM/fp-go/v2/number"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeIntCaller turns a registry of calls that change an int into a
// [StateToolCaller].
func makeIntCaller(registry map[string]StateCall[int]) StateToolCaller[int] {
	return F.Bind1st(record.MonadLookup[StateCall[int], string], registry)
}

// changing is a fake call that answers text and applies change.
func changing(change Endomorphism[int], text string) StateCall[int] {
	return F.Constant1[string](thunk.Of(pair.MakePair(change, text)))
}

// runStateToolCalls executes [HandleStateToolCalls] on a completion.
func runStateToolCalls(t *testing.T, completion *openai.ChatCompletion, deps StateToolDeps[int]) ToolUpdate[int] {
	t.Helper()

	update, err := result.Unwrap(HandleStateToolCalls[int]()(completion)(deps)(t.Context())())
	require.NoError(t, err)

	return update
}

// TestHandleStateToolCalls_AppliesInCallOrder asserts that the changes of the
// tool calls compose in the order of the calls: add 1, then double.
func TestHandleStateToolCalls_AppliesInCallOrder(t *testing.T) {
	deps := MakeStateToolDeps(makeIntCaller(map[string]StateCall[int]{
		"inc":    changing(N.Add(1), "inc"),
		"double": changing(N.Mul(2), "double"),
	}))
	completion := &openai.ChatCompletion{Choices: []openai.ChatCompletionChoice{{
		FinishReason: "tool_calls",
		Message: openai.ChatCompletionMessage{Role: "assistant", ToolCalls: []openai.ChatCompletionMessageToolCallUnion{
			makeToolCallUnion("call_1", "inc", `{}`),
			makeToolCallUnion("call_2", "double", `{}`),
		}},
	}}}

	update := runStateToolCalls(t, completion, deps)

	assert.Equal(t, 8, pair.Head(update)(3))
	assert.Len(t, pair.Tail(update)(openai.ChatCompletionNewParams{}).Messages, 3)
}

// TestHandleStateToolCalls_FailureChangesNothing asserts that a failed call
// and an unknown tool are tool messages with the identity.
func TestHandleStateToolCalls_FailureChangesNothing(t *testing.T) {
	deps := MakeStateToolDeps(makeIntCaller(map[string]StateCall[int]{
		"failing": F.Constant1[string](thunk.Left[ToolResult[int]](errors.New("broke"))),
	}))
	completion := &openai.ChatCompletion{Choices: []openai.ChatCompletionChoice{{
		FinishReason: "tool_calls",
		Message: openai.ChatCompletionMessage{Role: "assistant", ToolCalls: []openai.ChatCompletionMessageToolCallUnion{
			makeToolCallUnion("call_1", "failing", `{}`),
			makeToolCallUnion("call_2", "unknown", `{}`),
		}},
	}}}

	update := runStateToolCalls(t, completion, deps)

	assert.Equal(t, 3, pair.Head(update)(3))
	messages := pair.Tail(update)(openai.ChatCompletionNewParams{}).Messages
	require.Len(t, messages, 3)
	assert.Equal(t, "broke", decodeErrorResult(t, messages[1]).Message)
	assert.Equal(t, "Unable to find tool 'unknown'.", decodeErrorResult(t, messages[2]).Message)
}

// TestHandleStateToolCalls_NoToolCalls asserts that a completion without
// tool calls changes neither the state nor the request.
func TestHandleStateToolCalls_NoToolCalls(t *testing.T) {
	update := runStateToolCalls(t, &openai.ChatCompletion{}, MakeStateToolDeps(makeIntCaller(nil)))

	assert.Equal(t, 3, pair.Head(update)(3))
	assert.Empty(t, pair.Tail(update)(openai.ChatCompletionNewParams{}).Messages)
}

// TestTruncateCall_KeepsChange asserts that truncating a state call shortens
// its text and keeps its change.
func TestTruncateCall_KeepsChange(t *testing.T) {
	call := TruncateCall[int]()(4)(changing(N.Add(1), strings.Repeat("x", 10)))

	got, err := result.Unwrap(call(`{}`)(t.Context())())
	require.NoError(t, err)

	assert.Equal(t, 2, pair.Head(got)(1))
	assert.True(t, strings.HasPrefix(pair.Tail(got), "xxxx\n\n[Output truncated"))
}

// TestTruncateStateTools_KeepsDefinition asserts that truncating a registry
// of state tools keeps the definitions and truncates the calls.
func TestTruncateStateTools_KeepsDefinition(t *testing.T) {
	registry := ToToolRegistryOf[StateCall[int]]()([]StateTool[int]{
		MakeToolOf(openai.FunctionDefinitionParam{Name: "inc"}, changing(N.Add(1), strings.Repeat("x", 10))),
	})

	truncated := TruncateStateTools[int]()(4)(registry)

	require.Contains(t, truncated, "inc")
	assert.Equal(t, "inc", truncated["inc"].Definition.Name)
	got, err := result.Unwrap(truncated["inc"].Call(`{}`)(t.Context())())
	require.NoError(t, err)
	assert.Equal(t, 2, pair.Head(got)(1))
	assert.Contains(t, pair.Tail(got), "[Output truncated")
}
