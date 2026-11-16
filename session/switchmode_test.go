package session

import (
	"testing"

	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// planRequest is the request of a fake target mode: a system message and
// the tools of the mode, with a user message the switch must not take over.
func planRequest() openai.ChatCompletionNewParams {
	return openai.ChatCompletionNewParams{
		Model: "gpt-other",
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("you plan"),
			openai.UserMessage("not taken over"),
		},
		Tools: offering("read_file"),
	}
}

// TestSwitchMode asserts that the switch replaces the system message and
// the tools of the current request, keeps the conversation and the model,
// and records the mode.
func TestSwitchMode(t *testing.T) {
	s := Continue()("and in Paris?")(MakeSession(makeLoopRequest()))
	assert.Equal(t, option.None[string](), ModeID()(s))

	got := SwitchMode()(pair.MakePair("plan", planRequest()))(s)

	assert.Equal(t, option.Some("plan"), ModeID()(got))
	assert.Equal(t, openai.ChatModel("gpt-test"), got.current.Model)
	assert.Equal(t, planRequest().Tools, got.current.Tools)
	require.Len(t, got.current.Messages, len(s.current.Messages))
	assert.Equal(t, planRequest().Messages[0], got.current.Messages[0])
	assert.Equal(t, s.current.Messages[1:], got.current.Messages[1:])
	assert.Equal(t, s.history, got.history)
}

// TestSwitchMode_RoundTrip asserts that the mode survives the DTO.
func TestSwitchMode_RoundTrip(t *testing.T) {
	s := SwitchMode()(pair.MakePair("plan", planRequest()))(MakeSession(makeLoopRequest()))

	assert.Equal(t, "plan", SessionIso().Get(s).Mode)
	assert.Equal(t, option.Some("plan"), ModeID()(SessionIso().ReverseGet(SessionIso().Get(s))))
}

// TestNext_UnofferedToolIsUnknown asserts that the caller only calls the
// tools the request offered: a tool of the registry the request doesn't
// offer, as after a switch to a mode without it, is not found.
func TestNext_UnofferedToolIsUnknown(t *testing.T) {
	responses := []openai.ChatCompletion{
		makeToolCallsCompletion("chatcmpl-1", "set_model"),
		makeStopCompletion("chatcmpl-2", "done"),
	}
	deps := makeSequencedSessionToolDeps(t, responses, makeSessionCaller(map[string]SessionCall{
		"set_model": setModel("gpt-changed", "model set"),
	}), 10)

	request := makeLoopRequest()
	request.Tools = offering("get_weather")

	next := pair.Head(pair.Head(runAnswer(t, MakeSession(request), deps)).history[1])

	assert.Equal(t, openai.ChatModel("gpt-test"), next.Model)
	assert.Contains(t, toolMessageText(t, next, 3), "Unable to find tool 'set_model'.")
}
