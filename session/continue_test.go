package session

import (
	"testing"

	"github.com/IBM/fp-go/v2/pair"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestContinue_AppendsUserMessage asserts that Continue appends the user
// message, resets the iteration counter and keeps history and usage.
func TestContinue_AppendsUserMessage(t *testing.T) {
	entry := pair.MakePair(makeLoopRequest(), &openai.ChatCompletion{ID: "chatcmpl-1"})
	usage := openai.CompletionUsage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12}
	s := Session{
		history:    History{entry},
		current:    makeLoopRequest(),
		usage:      usage,
		iterations: 3,
	}

	got := Continue()("and in Paris?")(s)

	assert.Equal(t, 0, got.iterations)
	assert.Equal(t, usage, got.usage)
	assert.Equal(t, History{entry}, got.history)

	require.Len(t, got.current.Messages, 3)
	assert.Equal(t, makeLoopRequest().Messages, got.current.Messages[:2])
	assert.Equal(t, openai.UserMessage("and in Paris?"), got.current.Messages[2])

	// the input session is not changed
	assert.Equal(t, 3, s.iterations)
	assert.Len(t, s.current.Messages, 2)
}

// TestContinue_SecondTurnSendsWholeConversation asserts that a second Run
// after Continue sends the first answer and the new user message, and that
// usage accumulates over both turns while the iterations count per turn.
func TestContinue_SecondTurnSendsWholeConversation(t *testing.T) {
	responses := []openai.ChatCompletion{
		makeStopCompletion("chatcmpl-1", "It is sunny."),
		makeStopCompletion("chatcmpl-2", "It rains in Paris."),
	}

	deps := makeSequencedSessionDeps(t, responses, makeWeatherRegistry(), 10)

	first := runAnswer(t, MakeSession(makeLoopRequest()), deps)
	second := runAnswer(t, Continue()("and in Paris?")(pair.Head(first)), deps)

	got := pair.Head(second)

	assert.Equal(t, 1, got.iterations)
	assert.Equal(t, int64(60), got.usage.PromptTokens)
	assert.Equal(t, int64(10), got.usage.CompletionTokens)

	require.Len(t, got.history, 2)
	assert.Equal(t, "chatcmpl-1", pair.Tail(got.history[0]).ID)
	assert.Equal(t, "chatcmpl-2", pair.Tail(got.history[1]).ID)

	// the second request: system, user, first answer, second user message
	request := pair.Head(got.history[1])
	require.Len(t, request.Messages, 4)
	assert.Equal(t, responses[0].Choices[0].Message.ToParam(), request.Messages[2])
	assert.Equal(t, openai.UserMessage("and in Paris?"), request.Messages[3])

	// the session ends with the second answer
	require.Len(t, got.current.Messages, 5)
	assert.Equal(t, responses[1].Choices[0].Message.ToParam(), got.current.Messages[4])
	assert.Equal(t, "It rains in Paris.", pair.Tail(second).Choices[0].Message.Content)
}
