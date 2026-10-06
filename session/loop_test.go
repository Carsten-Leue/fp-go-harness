package session

import (
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeLoopRequest() openai.ChatCompletionNewParams {
	return openai.ChatCompletionNewParams{
		Model: "gpt-test",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("you are a helpful assistant"),
			openai.UserMessage("what's the weather in Berlin?"),
		},
	}
}

func makeToolCallCompletion(id, callID string) openai.ChatCompletion {
	return openai.ChatCompletion{
		ID:     id,
		Object: "chat.completion",
		Model:  "gpt-test",
		Choices: []openai.ChatCompletionChoice{
			{
				FinishReason: "tool_calls",
				Message: openai.ChatCompletionMessage{
					Role: "assistant",
					ToolCalls: []openai.ChatCompletionMessageToolCallUnion{
						{
							ID:   callID,
							Type: "function",
							Function: openai.ChatCompletionMessageFunctionToolCallFunction{
								Name:      "get_weather",
								Arguments: `{"city":"Berlin"}`,
							},
						},
					},
				},
			},
		},
		Usage: openai.CompletionUsage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
	}
}

func makeStopCompletion(id, content string) openai.ChatCompletion {
	return openai.ChatCompletion{
		ID:     id,
		Object: "chat.completion",
		Model:  "gpt-test",
		Choices: []openai.ChatCompletionChoice{
			{
				FinishReason: "stop",
				Message: openai.ChatCompletionMessage{
					Role:    "assistant",
					Content: content,
				},
			},
		},
		Usage: openai.CompletionUsage{PromptTokens: 30, CompletionTokens: 5, TotalTokens: 35},
	}
}

func makeWeatherRegistry() map[string]tools.ToolCall {
	var weatherTool tools.ToolCall = func(arguments string) tools.Thunk[string] {
		return thunk.Of("sunny, 21C for " + arguments)
	}
	return map[string]tools.ToolCall{"get_weather": weatherTool}
}

// TestRun_LandsAfterToolRoundTrips asserts that Run keeps bouncing through
// tool call round trips until the model stops, accumulating history and usage.
func TestRun_LandsAfterToolRoundTrips(t *testing.T) {
	responses := []openai.ChatCompletion{
		makeToolCallCompletion("chatcmpl-1", "call_1"),
		makeToolCallCompletion("chatcmpl-2", "call_2"),
		makeStopCompletion("chatcmpl-3", "It is sunny."),
	}

	deps := makeSequencedSessionDeps(t, responses, makeWeatherRegistry(), 10)

	final, err := result.Unwrap(Run()(MakeSession(makeLoopRequest()))(deps)(t.Context())())
	require.NoError(t, err)

	gotSession := pair.Head(final)
	gotCompletion := pair.Tail(final)

	assert.Equal(t, 3, gotSession.iterations)
	assert.Len(t, gotSession.history, 2)
	assert.Equal(t, int64(50), gotSession.usage.PromptTokens)
	assert.Equal(t, int64(9), gotSession.usage.CompletionTokens)
	assert.Equal(t, int64(59), gotSession.usage.TotalTokens)

	// system + user + 2 x (assistant tool call + tool result)
	assert.Len(t, gotSession.current.Messages, 6)

	assert.Equal(t, "chatcmpl-3", gotCompletion.ID)
	assert.Equal(t, "It is sunny.", gotCompletion.Choices[0].Message.Content)
}

// TestRun_StopsAtMaxIterations asserts that a model which never stops calling
// tools makes Run fail with a MaxIterationsError after the configured limit.
func TestRun_StopsAtMaxIterations(t *testing.T) {
	responses := []openai.ChatCompletion{
		makeToolCallCompletion("chatcmpl-loop", "call_loop"),
	}

	deps := makeSequencedSessionDeps(t, responses, makeWeatherRegistry(), 3)

	_, err := result.Unwrap(Run()(MakeSession(makeLoopRequest()))(deps)(t.Context())())

	var maxErr *MaxIterationsError
	require.ErrorAs(t, err, &maxErr)
	assert.Equal(t, 3, maxErr.MaxIterations)
}
