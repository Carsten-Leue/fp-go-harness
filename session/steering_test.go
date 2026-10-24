package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	opt "github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSteer_AppendsQueuedMessages asserts that Steer appends one user message
// per queued text, in order, and keeps the iteration counter.
func TestSteer_AppendsQueuedMessages(t *testing.T) {
	deps := MakeSteeringDeps(thunk.Of(A.From("and in Paris?", "in Celsius, please")))
	s := Session{current: makeLoopRequest(), iterations: 2}

	got, err := result.Unwrap(Steer()(s)(deps)(t.Context())())
	require.NoError(t, err)

	assert.Equal(t, 2, got.iterations)
	require.Len(t, got.current.Messages, 4)
	assert.Equal(t, makeLoopRequest().Messages, got.current.Messages[:2])
	assert.Equal(t, openai.UserMessage("and in Paris?"), got.current.Messages[2])
	assert.Equal(t, openai.UserMessage("in Celsius, please"), got.current.Messages[3])

	// the input session is not changed
	assert.Len(t, s.current.Messages, 2)
}

// TestSteer_WithoutMessages asserts that Steer leaves the session unchanged
// when nothing is queued.
func TestSteer_WithoutMessages(t *testing.T) {
	s := Session{current: makeLoopRequest(), iterations: 1}

	assert.Equal(t, result.Of(s), Steer()(s)(MakeSteeringDeps(NoSteering()))(t.Context())())
}

// TestChannelSteering_DrainsQueue asserts that each poll takes the messages
// waiting in the channel without blocking, and that a closed channel has none.
func TestChannelSteering_DrainsQueue(t *testing.T) {
	queue := make(chan string, 3)
	queue <- "first"
	queue <- "second"

	poll := ChannelSteering(queue)

	assert.Equal(t, result.Of(A.From("first", "second")), poll(t.Context())())
	assert.Equal(t, result.Of(A.Empty[string]()), poll(t.Context())())

	queue <- "third"
	close(queue)

	assert.Equal(t, result.Of(A.From("third")), poll(t.Context())())
	assert.Equal(t, result.Of(A.Empty[string]()), poll(t.Context())())
}

// TestChannelSteering_Cancelled asserts that the drain checks the context: a
// cancelled context ends the poll with its error, even with messages waiting.
func TestChannelSteering_Cancelled(t *testing.T) {
	queue := make(chan string, 1)
	queue <- "ignored"

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	assert.ErrorIs(t, result.ToError(ChannelSteering(queue)(ctx)()), context.Canceled)
}

// TestTryReceive asserts the three outcomes of one receive: a waiting
// message, None for an empty or closed queue, and the context's error once it
// is cancelled.
func TestTryReceive(t *testing.T) {
	queue := make(chan string, 1)
	queue <- "hi"

	assert.Equal(t, result.Of(option.Of("hi")), tryReceive(queue)(t.Context())())
	assert.Equal(t, result.Of(option.None[string]()), tryReceive(queue)(t.Context())())

	close(queue)
	assert.Equal(t, result.Of(option.None[string]()), tryReceive(queue)(t.Context())())

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.ErrorIs(t, result.ToError(tryReceive(make(chan string))(ctx)()), context.Canceled)
}

func TestMakeSteeringDeps(t *testing.T) {
	deps := MakeSteeringDeps(thunk.Of(A.From("hi")))

	assert.Equal(t, result.Of(A.From("hi")), deps.GetPoll()(t.Context())())
	assert.Equal(t, deps, AsSteeringDeps(deps))
}

// steeredAnswer is the fake server's answer to one kind of request: the
// response, the text the user types while it is being answered (none when
// empty), the HTTP status of a failed answer (none when zero), and, when
// set, the channel the fake signals on before it blocks until the request is
// cancelled instead of answering.
type steeredAnswer struct {
	response openai.ChatCompletion
	typed    string
	status   int
	arrived  chan<- struct{}
}

// makeSteeredSessionDeps starts a fake server that answers the initial
// request with first and every later request with later, and wires queue as
// the steering poll. While it answers, the fake sends the answer's typed text
// to queue, as the user typing during the turn. It keeps no state: it tells
// the initial request from the later ones by its number of messages. The
// client does not retry, so a request is answered once.
func makeSteeredSessionDeps(t *testing.T, queue chan string, maxIterations int, first, later steeredAnswer) SessionDeps {
	t.Helper()

	initial := len(makeLoopRequest().Messages)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); !assert.NoError(t, err) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		answer := later
		if len(request.Messages) == initial {
			answer = first
		}
		if answer.typed != "" {
			queue <- answer.typed
		}
		if answer.arrived != nil {
			answer.arrived <- struct{}{}
			<-r.Context().Done()
			return
		}
		if answer.status != 0 {
			http.Error(w, http.StatusText(answer.status), answer.status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(answer.response))
	}))
	t.Cleanup(server.Close)

	client := openai.NewClient(opt.WithBaseURL(server.URL+"/"), opt.WithAPIKey("test-key"), opt.WithMaxRetries(0))
	stub := &stubSessionDeps{
		svc:           &client.Chat.Completions,
		toolCall:      F.Bind1st(record.MonadLookup[tools.ToolCall, string], makeWeatherRegistry()),
		maxIterations: maxIterations,
	}

	return MakeSessionDeps(
		stub,
		tools.MakeToolDeps(stub.GetToolCaller()),
		MakeLoopDeps(maxIterations),
		MakeSteeringDeps(ChannelSteering(queue)),
	)
}

// TestRun_SendsSteeringMessages asserts that a user message queued while the
// model answers the first request is sent with the second one, after the
// tool results, that the loop goes on to its terminal response, and that a
// message queued while the terminal response is answered stays queued.
func TestRun_SendsSteeringMessages(t *testing.T) {
	queue := make(chan string, 1)
	deps := makeSteeredSessionDeps(t, queue, 10,
		steeredAnswer{response: makeToolCallCompletion("chatcmpl-1", "call_1"), typed: "and in Paris?"},
		steeredAnswer{response: makeStopCompletion("chatcmpl-2", "Sunny in Berlin, rain in Paris."), typed: "and in Rome?"},
	)

	final := runAnswer(t, MakeSession(makeLoopRequest()), deps)

	got := pair.Head(final)
	assert.Equal(t, 2, got.iterations)
	require.Len(t, got.history, 2)

	// the first request is the initial one
	assert.Equal(t, makeLoopRequest().Messages, pair.Head(got.history[0]).Messages)

	// the second request: system, user, tool call, tool result, steering message
	second := pair.Head(got.history[1]).Messages
	require.Len(t, second, 5)
	assert.NotNil(t, second[2].OfAssistant)
	require.NotNil(t, second[3].OfTool)
	assert.Equal(t, "call_1", second[3].OfTool.ToolCallID)
	assert.Equal(t, openai.UserMessage("and in Paris?"), second[4])

	assert.Equal(t, "Sunny in Berlin, rain in Paris.", pair.Tail(final).Choices[0].Message.Content)

	// the message typed during the terminal response waits for the next Run
	assert.Equal(t, result.Of(A.From("and in Rome?")), ChannelSteering(queue)(t.Context())())
}

// TestRun_KeepsSteeringMessagesAtMaxIterations asserts that no poll happens
// once the iteration limit is reached: a message queued during the last
// allowed request is still queued when the turn ends with a
// MaxIterationsError.
func TestRun_KeepsSteeringMessagesAtMaxIterations(t *testing.T) {
	toolCall := steeredAnswer{response: makeToolCallCompletion("chatcmpl-loop", "call_loop"), typed: "too late"}

	queue := make(chan string, 1)
	deps := makeSteeredSessionDeps(t, queue, 1, toolCall, toolCall)

	turn := runTurnIn(t, t.Context(), MakeSession(makeLoopRequest()), deps)

	var maxErr *MaxIterationsError
	require.ErrorAs(t, result.ToError(pair.Tail(turn)), &maxErr)
	assert.Equal(t, result.Of(A.From("too late")), ChannelSteering(queue)(t.Context())())
}

// TestRun_RequestErrorKeepsSteeringMessages asserts that a request that fails
// after the poll took the queued messages ends the turn with its error, and
// that the session keeps the messages in its request.
func TestRun_RequestErrorKeepsSteeringMessages(t *testing.T) {
	queue := make(chan string, 1)
	deps := makeSteeredSessionDeps(t, queue, 10,
		steeredAnswer{response: makeToolCallCompletion("chatcmpl-1", "call_1"), typed: "and in Paris?"},
		steeredAnswer{status: http.StatusInternalServerError},
	)

	turn := runTurnIn(t, t.Context(), MakeSession(makeLoopRequest()), deps)

	var apiErr *openai.Error
	require.ErrorAs(t, result.ToError(pair.Tail(turn)), &apiErr)
	assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)

	got := pair.Head(turn)
	assert.Equal(t, 1, got.iterations)
	require.Len(t, got.history, 1)

	// system, user, tool call, tool result, steering message
	require.Len(t, got.current.Messages, 5)
	assert.Equal(t, openai.UserMessage("and in Paris?"), got.current.Messages[4])
	assert.Equal(t, result.Of(A.Empty[string]()), ChannelSteering(queue)(t.Context())())
}

// TestRun_CancelledDuringRequestKeepsSteeringMessages asserts that a turn
// cancelled while request N runs ends with the cancellation, and that the
// session is the input of request N, with the steered message it carries.
func TestRun_CancelledDuringRequestKeepsSteeringMessages(t *testing.T) {
	queue := make(chan string, 1)
	arrived := make(chan struct{}, 1)
	deps := makeSteeredSessionDeps(t, queue, 10,
		steeredAnswer{response: makeToolCallCompletion("chatcmpl-1", "call_1"), typed: "and in Paris?"},
		steeredAnswer{arrived: arrived},
	)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		<-arrived
		cancel()
	}()

	turn := runTurnIn(t, ctx, MakeSession(makeLoopRequest()), deps)

	assert.True(t, IsCancelled()(result.ToError(pair.Tail(turn))))

	got := pair.Head(turn)
	assert.Equal(t, 1, got.iterations)
	require.Len(t, got.history, 1)

	// system, user, tool call, tool result, steering message
	require.Len(t, got.current.Messages, 5)
	assert.Equal(t, openai.UserMessage("and in Paris?"), got.current.Messages[4])
}
