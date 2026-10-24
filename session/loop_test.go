package session

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
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

// runTurnIn runs one turn of a session under ctx and returns its
// [TurnResult]. Run never fails: the outcome of the turn is the tail.
func runTurnIn(t *testing.T, ctx context.Context, s Session, deps SessionDeps) TurnResult {
	t.Helper()

	turn, err := result.Unwrap(Run()(s)(deps)(ctx)())
	require.NoError(t, err)

	return turn
}

// runAnswer runs one turn that the model answers and returns the session and
// the final completion.
func runAnswer(t *testing.T, s Session, deps SessionDeps) FinalResult {
	t.Helper()

	turn := runTurnIn(t, t.Context(), s, deps)
	completion, err := result.Unwrap(pair.Tail(turn))
	require.NoError(t, err)

	return pair.MakePair(pair.Head(turn), completion)
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

	final := runAnswer(t, MakeSession(makeLoopRequest()), deps)

	gotSession := pair.Head(final)
	gotCompletion := pair.Tail(final)

	assert.Equal(t, 3, gotSession.iterations)
	assert.Len(t, gotSession.history, 3)
	assert.Equal(t, int64(50), gotSession.usage.PromptTokens)
	assert.Equal(t, int64(9), gotSession.usage.CompletionTokens)
	assert.Equal(t, int64(59), gotSession.usage.TotalTokens)

	// system + user + 2 x (assistant tool call + tool result) + answer
	assert.Len(t, gotSession.current.Messages, 7)

	assert.Equal(t, "chatcmpl-3", gotCompletion.ID)
	assert.Equal(t, "It is sunny.", gotCompletion.Choices[0].Message.Content)
}

// TestRun_StopsAtMaxIterations asserts that a model which never stops calling
// tools ends the turn with a MaxIterationsError as its outcome after the
// configured limit, together with the session that reached it.
func TestRun_StopsAtMaxIterations(t *testing.T) {
	responses := []openai.ChatCompletion{
		makeToolCallCompletion("chatcmpl-loop", "call_loop"),
	}

	deps := makeSequencedSessionDeps(t, responses, makeWeatherRegistry(), 3)

	turn := runTurnIn(t, t.Context(), MakeSession(makeLoopRequest()), deps)

	var maxErr *MaxIterationsError
	require.ErrorAs(t, result.ToError(pair.Tail(turn)), &maxErr)
	assert.Equal(t, 3, maxErr.MaxIterations)

	got := pair.Head(turn)
	assert.Equal(t, 3, got.iterations)
	assert.Len(t, got.history, 3)
	assert.Equal(t, int64(36), got.usage.TotalTokens)
	// system + user + 3 x (assistant tool call + tool result)
	assert.Len(t, got.current.Messages, 8)
}

// waitToolName is the tool of [makeCancellingSessionDeps] that blocks until
// its context ends.
const waitToolName = "wait"

// makeCancellingSessionDeps starts a fake server that answers the initial
// request with a call of the tool [waitToolName], and blocks every later
// request until its context ends. The tool reports on started that it runs,
// blocks until its context ends and hands the context's error to onDone. The
// fake tells the initial request from the later ones by its number of
// messages, so it keeps no state.
func makeCancellingSessionDeps(t *testing.T, started chan<- struct{}, onDone func(error)) SessionDeps {
	t.Helper()

	initial := len(makeLoopRequest().Messages)
	toolCall := makeToolCallCompletion("chatcmpl-1", "call_1")
	toolCall.Choices[0].Message.ToolCalls[0].Function.Name = waitToolName

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []json.RawMessage `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); !assert.NoError(t, err) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(request.Messages) != initial {
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(toolCall))
	}))
	t.Cleanup(server.Close)

	var wait tools.ToolCall = func(string) tools.Thunk[string] {
		return func(ctx context.Context) func() Result[string] {
			return func() Result[string] {
				started <- struct{}{}
				<-ctx.Done()
				onDone(ctx.Err())
				return result.Left[string](ctx.Err())
			}
		}
	}

	client := openai.NewClient(opt.WithBaseURL(server.URL+"/"), opt.WithAPIKey("test-key"), opt.WithMaxRetries(0))

	return &stubSessionDeps{
		svc:           &client.Chat.Completions,
		toolCall:      F.Bind1st(record.MonadLookup[tools.ToolCall, string], map[string]tools.ToolCall{waitToolName: wait}),
		maxIterations: 10,
	}
}

// TestRun_CancelledDuringTool asserts that a turn cancelled while a tool
// runs ends with the cancellation as its outcome: the tool sees its context
// end, its message carries the error, the next request fails, and the
// session keeps the round trip whose request returned.
func TestRun_CancelledDuringTool(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []error
	)
	recordErr := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, err)
	}

	started := make(chan struct{}, 1)
	deps := makeCancellingSessionDeps(t, started, recordErr)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		<-started
		cancel()
	}()

	turn := runTurnIn(t, ctx, MakeSession(makeLoopRequest()), deps)

	assert.True(t, IsCancelled()(result.ToError(pair.Tail(turn))))

	mu.Lock()
	assert.ElementsMatch(t, []error{context.Canceled}, seen)
	mu.Unlock()

	got := pair.Head(turn)
	assert.Equal(t, 1, got.iterations)
	require.Len(t, got.history, 1)
	assert.Equal(t, "chatcmpl-1", pair.Tail(got.history[0]).ID)

	// system, user, tool call, tool result with the error
	require.Len(t, got.current.Messages, 4)
	require.NotNil(t, got.current.Messages[3].OfTool)
	assert.Contains(t, got.current.Messages[3].OfTool.Content.OfString.Value, context.Canceled.Error())
}

// TestRun_CancelledBeforeStart asserts that a turn whose context is already
// cancelled lands the cancellation with the session it started from.
func TestRun_CancelledBeforeStart(t *testing.T) {
	deps := makeSequencedSessionDeps(t, []openai.ChatCompletion{makeStopCompletion("chatcmpl-1", "done")}, makeWeatherRegistry(), 10)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	s := MakeSession(makeLoopRequest())
	turn := runTurnIn(t, ctx, s, deps)

	assert.True(t, IsCancelled()(result.ToError(pair.Tail(turn))))
	assert.Equal(t, s, pair.Head(turn))
}

func TestIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(fmt.Errorf("interrupt: %w", context.Canceled))

	assert.True(t, IsCancelled()(context.Canceled))
	assert.True(t, IsCancelled()(fmt.Errorf("request: %w", context.Canceled)))
	assert.True(t, IsCancelled()(context.Cause(ctx)))
	assert.False(t, IsCancelled()(context.DeadlineExceeded))
	assert.False(t, IsCancelled()(makeMaxIterationsError(1)))
}

func TestMakeLoopDeps(t *testing.T) {
	deps := MakeLoopDeps(7)

	assert.Equal(t, 7, deps.GetMaxIterations())
	assert.Equal(t, deps, AsLoopDeps(deps))
}

func TestMaxIterationsError_Error(t *testing.T) {
	assert.EqualError(t, makeMaxIterationsError(3), "agent loop stopped after reaching the maximum of 3 iterations")
}

// TestMakeSessionDeps asserts that the composed deps delegate each getter to
// the part that provides it.
func TestMakeSessionDeps(t *testing.T) {
	stub := makeSequencedSessionDeps(t, []openai.ChatCompletion{makeStopCompletion("chatcmpl-1", "done")}, makeWeatherRegistry(), 10)

	deps := MakeSessionDeps(stub, tools.MakeToolDeps(stub.GetToolCaller()), MakeLoopDeps(4), MakeSteeringDeps(thunk.Of(A.From("steer"))))

	assert.Equal(t, 4, deps.GetMaxIterations())
	assert.Equal(t, result.Of(A.From("steer")), deps.GetPoll()(t.Context())())
	assert.Same(t, stub.GetChatCompletionService(), deps.GetChatCompletionService())
	assert.True(t, option.IsSome(deps.GetToolCaller()("get_weather")))

	assert.Equal(t, "done", pair.Tail(runAnswer(t, MakeSession(makeLoopRequest()), deps)).Choices[0].Message.Content)
}
