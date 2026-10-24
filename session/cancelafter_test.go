package session

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	opt "github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cancelAtEOF is a response body that cancels the context of the turn once it
// has been read to its end: the client has received the whole completion, so
// the cancellation comes after the response, independent of any timing.
type cancelAtEOF struct {
	io.Reader
	cancel context.CancelFunc
}

func (b cancelAtEOF) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if err == io.EOF {
		b.cancel()
	}
	return n, err
}

func (b cancelAtEOF) Close() error {
	return nil
}

// makeCancelAfterAnswerDeps answers every request with response without a
// network: a client middleware returns it in a [cancelAtEOF] body. The
// middleware keeps no state.
func makeCancelAfterAnswerDeps(t *testing.T, cancel context.CancelFunc, response openai.ChatCompletion) SessionDeps {
	t.Helper()

	body, err := json.Marshal(response)
	require.NoError(t, err)

	answer := func(req *http.Request, _ opt.MiddlewareNext) (*http.Response, error) {
		return &http.Response{
			Status:        http.StatusText(http.StatusOK),
			StatusCode:    http.StatusOK,
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        http.Header{"Content-Type": []string{"application/json"}},
			Body:          cancelAtEOF{bytes.NewReader(body), cancel},
			ContentLength: int64(len(body)),
			Request:       req,
		}, nil
	}

	client := openai.NewClient(
		opt.WithBaseURL("http://cancel-after-answer.invalid/"),
		opt.WithAPIKey("test-key"),
		opt.WithMaxRetries(0),
		opt.WithMiddleware(answer),
	)

	return &stubSessionDeps{
		svc:           &client.Chat.Completions,
		toolCall:      F.Bind1st(record.MonadLookup[tools.ToolCall, string], makeWeatherRegistry()),
		maxIterations: 10,
	}
}

// TestRun_CancelledAfterFinalAnswer asserts that a final answer that arrives
// after the context was cancelled still lands as the answer of the turn.
func TestRun_CancelledAfterFinalAnswer(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	deps := makeCancelAfterAnswerDeps(t, cancel, makeStopCompletion("chatcmpl-1", "It is sunny."))

	turn := runTurnIn(t, ctx, MakeSession(makeLoopRequest()), deps)

	completion, err := result.Unwrap(pair.Tail(turn))
	require.NoError(t, err)
	assert.Equal(t, "chatcmpl-1", completion.ID)

	got := pair.Head(turn)
	assert.Equal(t, 1, got.iterations)
	assert.Len(t, got.history, 1)
	assert.Equal(t, int64(35), got.usage.TotalTokens)
}

// TestRun_CancelledAfterToolCall asserts that a tool call that arrives after
// the context was cancelled stays in the history: the turn ends with the
// cancellation, with the round trip, its usage and its iteration.
func TestRun_CancelledAfterToolCall(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	deps := makeCancelAfterAnswerDeps(t, cancel, makeToolCallCompletion("chatcmpl-1", "call_1"))

	turn := runTurnIn(t, ctx, MakeSession(makeLoopRequest()), deps)

	assert.True(t, IsCancelled()(result.ToError(pair.Tail(turn))))

	got := pair.Head(turn)
	assert.Equal(t, 1, got.iterations)
	require.Len(t, got.history, 1)
	assert.Equal(t, "chatcmpl-1", pair.Tail(got.history[0]).ID)
	assert.Equal(t, int64(12), got.usage.TotalTokens)
}
