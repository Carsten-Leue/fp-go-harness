package replay

import (
	"path/filepath"
	"testing"

	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/session"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	openai "github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syntheticLog is a hand-written log in the recorded format: one task that
// calls a weather tool once and then answers.
var syntheticLog = filepath.Join("testdata", "synthetic-task.log")

func decodeResponse(t *testing.T, body string) ChatResponse {
	t.Helper()

	r, err := result.Unwrap(decodePayload[ChatResponse]()(body))
	require.NoError(t, err)
	return r
}

func loadSyntheticTask(t *testing.T) Task {
	t.Helper()

	tasks, err := result.Unwrap(F.Pipe2(syntheticLog, LoadLogFile(), ioresult.ChainResultK(GroupTasks()))())
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	return tasks[0]
}

// sendRequest sends one chat completion request through deps.
func sendRequest(t *testing.T, deps oai.ChatCompletionDeps) Result[*openai.ChatCompletion] {
	t.Helper()

	return F.Pipe1(
		openai.ChatCompletionNewParams{Model: "deepseek-chat"},
		oai.ChatCompletion(),
	)(deps)(t.Context())()
}

func TestToChatCompletionToolCalls(t *testing.T) {
	got := ToChatCompletion(decodeResponse(t, toolCallResponse))

	assert.Equal(t, "cmpl-2", got.ID)
	assert.Equal(t, "deepseek-chat", got.Model)
	require.Len(t, got.Choices, 1)

	choice := got.Choices[0]
	assert.Equal(t, "tool_calls", choice.FinishReason)
	assert.Equal(t, "assistant", string(choice.Message.Role))
	require.Len(t, choice.Message.ToolCalls, 1)
	assert.Equal(t, "tc-1", choice.Message.ToolCalls[0].ID)
	assert.Equal(t, "function", choice.Message.ToolCalls[0].Type)
	assert.Equal(t, "read_file", choice.Message.ToolCalls[0].Function.Name)
	assert.JSONEq(t, `{"path":"foo.go"}`, choice.Message.ToolCalls[0].Function.Arguments)

	assert.Equal(t, openai.CompletionUsage{PromptTokens: 20, CompletionTokens: 8, TotalTokens: 28}, got.Usage)
}

func TestToChatCompletionStop(t *testing.T) {
	r := decodeResponse(t, `{"id":"cmpl-9","modelId":"m","finishReason":"stop","text":"done",`+
		`"usage":{"inputTokens":7,"inputTokenDetails":{"cacheReadTokens":4},"outputTokens":3,"outputTokenDetails":{"reasoningTokens":1},"totalTokens":10}}`)

	got := ToChatCompletion(r)

	assert.Equal(t, "stop", got.Choices[0].FinishReason)
	assert.Equal(t, "done", got.Choices[0].Message.Content)
	assert.Empty(t, got.Choices[0].Message.ToolCalls)
	assert.Equal(t, int64(4), got.Usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, int64(1), got.Usage.CompletionTokensDetails.ReasoningTokens)
}

func TestArgumentsOfEmptyInput(t *testing.T) {
	assert.Equal(t, "{}", argumentsOf()(nil))
}

func TestPopFirst(t *testing.T) {
	assert.Equal(t, pair.MakePair([]int{2, 3}, option.Some(1)), popFirst[int]()([]int{1, 2, 3}))
	assert.Equal(t, pair.MakePair[[]int](nil, option.None[int]()), popFirst[int]()(nil))
}

// TestReplayAnswersInOrderThenExhausts asserts that the replay deps answer the
// requests with the recorded responses in order, and fail afterwards.
func TestReplayAnswersInOrderThenExhausts(t *testing.T) {
	deps := MakeReplayChatCompletionDeps()([]ChatResponse{
		decodeResponse(t, toolCallResponse),
		decodeResponse(t, minimalResponse),
	})()

	first, err := result.Unwrap(sendRequest(t, deps))
	require.NoError(t, err)
	assert.Equal(t, "cmpl-2", first.ID)

	second, err := result.Unwrap(sendRequest(t, deps))
	require.NoError(t, err)
	assert.Equal(t, "cmpl-1", second.ID)

	_, err = result.Unwrap(sendRequest(t, deps))
	require.ErrorIs(t, err, ErrReplayExhausted)
}

func TestTaskRequestEmptyTask(t *testing.T) {
	assert.True(t, option.IsNone(TaskRequest()(Task{TaskID: "empty"})))
}

// TestReplaySyntheticTask replays the synthetic recorded task end to end
// through session.Run, without network access.
func TestReplaySyntheticTask(t *testing.T) {
	task := loadSyntheticTask(t)
	assert.Equal(t, "task-weather", task.TaskID)

	start, ok := option.Unwrap(TaskRequest()(task))
	require.True(t, ok)

	var weather tools.ToolCall = func(arguments string) tools.Thunk[string] {
		return thunk.Of("sunny, 21C")
	}
	registry := map[string]tools.ToolCall{"get_weather": weather}

	deps := session.MakeSessionDeps(
		MakeTaskChatCompletionDeps()(task)(),
		tools.MakeToolDeps(F.Bind1st(record.MonadLookup[tools.ToolCall, string], registry)),
		session.MakeLoopDeps(10),
	)

	final, err := result.Unwrap(session.Run()(session.MakeSession(start))(deps)(t.Context())())
	require.NoError(t, err)

	got := pair.Head(final)
	assert.Equal(t, 2, session.MakeSessioniterationsLens().Get(got))
	assert.Len(t, session.MakeSessionhistoryLens().Get(got), 1)

	usage := session.MakeSessionusageLens().Get(got)
	assert.Equal(t, int64(110), usage.PromptTokens)
	assert.Equal(t, int64(20), usage.CompletionTokens)
	assert.Equal(t, int64(130), usage.TotalTokens)
	assert.Equal(t, int64(60), usage.PromptTokensDetails.CachedTokens)

	// the replayed run rebuilds the second recorded request: system, user,
	// assistant tool call and tool result
	assert.Len(t, session.MakeSessioncurrentLens().Get(got).Messages, len(task.Turns[1].Request.Messages))

	assert.Equal(t, "It is sunny in Berlin, 21C.", pair.Tail(final).Choices[0].Message.Content)
}
