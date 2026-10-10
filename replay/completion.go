package replay

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	A "github.com/IBM/fp-go/v2/array"
	B "github.com/IBM/fp-go/v2/bytes"
	F "github.com/IBM/fp-go/v2/function"
	IO "github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/ioref"
	J "github.com/IBM/fp-go/v2/json"
	N "github.com/IBM/fp-go/v2/number"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	openai "github.com/openai/openai-go/v3"
	opt "github.com/openai/openai-go/v3/option"
)

const (
	// replayBaseURL can't resolve, so a request that slips past the replay
	// middleware fails instead of reaching a real endpoint.
	replayBaseURL = "http://replay.invalid/"
	replayAPIKey  = "replay"

	chatCompletionObject = "chat.completion"
	assistantRole        = "assistant"
	functionToolType     = "function"
	emptyArguments       = "{}"
	contentTypeJSON      = "application/json"
)

// ErrReplayExhausted is the error of a request sent after every recorded
// response has been replayed.
var ErrReplayExhausted = errors.New("replay: no recorded response left")

// finishReasonOf maps a recorded finish reason ("tool-calls") to its OpenAI
// spelling ("tool_calls").
var finishReasonOf = strings.NewReplacer("-", "_").Replace

// argumentsOf encodes the recorded input of a tool call as the JSON arguments
// string of an OpenAI tool call. A call without input has no arguments.
func argumentsOf() reader.Reader[json.RawMessage, string] {
	return F.Flow3(
		rawBytes,
		option.FromPredicate(F.Flow2(B.Size, N.MoreThan(0))),
		option.Fold(F.Constant(emptyArguments), B.ToString),
	)
}

// newToolCall is a leaf: a constructor over the fields of a function tool call.
func newToolCall(id, name, arguments string) openai.ChatCompletionMessageToolCallUnion {
	return openai.ChatCompletionMessageToolCallUnion{
		ID:   id,
		Type: functionToolType,
		Function: openai.ChatCompletionMessageFunctionToolCallFunction{
			Name:      name,
			Arguments: arguments,
		},
	}
}

// toolCallOf converts a recorded tool-call part into an OpenAI tool call.
func toolCallOf(lenses ContentPartLenses) func(ContentPart) openai.ChatCompletionMessageToolCallUnion {
	return func(p ContentPart) openai.ChatCompletionMessageToolCallUnion {
		return newToolCall(lenses.ToolCallID.Get(p), lenses.ToolName.Get(p), argumentsOf()(lenses.Input.Get(p)))
	}
}

// usageOf converts recorded token usage into OpenAI usage. Cache-read tokens
// become cached prompt tokens.
func usageOf(u ResponseUsage) openai.CompletionUsage {
	lenses := MakeResponseUsageLenses()
	input := MakeInputTokenDetailsLenses()
	output := MakeOutputTokenDetailsLenses()

	return openai.CompletionUsage{
		PromptTokens:     lenses.InputTokens.Get(u),
		CompletionTokens: lenses.OutputTokens.Get(u),
		TotalTokens:      lenses.TotalTokens.Get(u),
		PromptTokensDetails: openai.CompletionUsagePromptTokensDetails{
			CachedTokens: input.CacheReadTokens.Get(lenses.InputTokenDetails.Get(u)),
		},
		CompletionTokensDetails: openai.CompletionUsageCompletionTokensDetails{
			ReasoningTokens: output.ReasoningTokens.Get(lenses.OutputTokenDetails.Get(u)),
		},
	}
}

// ToChatCompletion converts a recorded assembled response into the OpenAI
// completion it stands for: one choice whose message carries the text and the
// tool calls of the response.
func ToChatCompletion(r ChatResponse) openai.ChatCompletion {
	lenses := MakeChatResponseLenses()

	return openai.ChatCompletion{
		ID:     lenses.ID.Get(r),
		Object: chatCompletionObject,
		Model:  lenses.ModelID.Get(r),
		Choices: A.Of(openai.ChatCompletionChoice{
			FinishReason: finishReasonOf(lenses.FinishReason.Get(r)),
			Message: openai.ChatCompletionMessage{
				Role:      assistantRole,
				Content:   lenses.Text.Get(r),
				ToolCalls: A.Map(toolCallOf(MakeContentPartLenses()))(lenses.ToolCalls.Get(r)),
			},
		}),
		Usage: usageOf(lenses.Usage.Get(r)),
	}
}

// popFirst splits a queue into the rest of the queue and its first element.
// The rest and the first element are independent reads of the queue.
func popFirst[T any]() func([]T) Pair[[]T, Option[T]] {
	return F.Pipe1(
		F.Flow2(A.SliceRight[T](1), F.Curry2(pair.MakePair[[]T, Option[T]])),
		reader.Ap[Pair[[]T, Option[T]]](A.Head[T]),
	)
}

// jsonResponse is a leaf: a constructor for the HTTP response that answers req
// with a JSON body.
func jsonResponse(req *http.Request) func([]byte) *http.Response {
	return func(body []byte) *http.Response {
		return &http.Response{
			Status:        http.StatusText(http.StatusOK),
			StatusCode:    http.StatusOK,
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        http.Header{"Content-Type": A.Of(contentTypeJSON)},
			Body:          io.NopCloser(bytes.NewReader(body)),
			ContentLength: int64(len(body)),
			Request:       req,
		}
	}
}

// replayMiddleware answers every request with the next completion of the
// queue, without calling the next handler. Once the queue is empty, requests
// fail with [ErrReplayExhausted].
//
// The SDK fixes the shape of a middleware, so this is the edge where the
// queue's IO runs and the result is unwrapped.
func replayMiddleware(queue IORef[[]openai.ChatCompletion]) opt.Middleware {
	next := F.Pipe2(
		queue,
		ioref.ModifyWithResult(popFirst[openai.ChatCompletion]()),
		IO.Map(result.FromOption[openai.ChatCompletion](F.Constant(ErrReplayExhausted))),
	)

	return func(req *http.Request, _ opt.MiddlewareNext) (*http.Response, error) {
		return result.Unwrap(F.Pipe2(
			next(),
			result.Chain(J.Marshal[openai.ChatCompletion]),
			result.Map(jsonResponse(req)),
		))
	}
}

// newReplayClient is a leaf: an OpenAI client whose requests are all answered
// by m. Retries are disabled, so an exhausted replay fails at once.
func newReplayClient(m opt.Middleware) *openai.Client {
	client := openai.NewClient(
		opt.WithBaseURL(replayBaseURL),
		opt.WithAPIKey(replayAPIKey),
		opt.WithMaxRetries(0),
		opt.WithMiddleware(m),
	)
	return &client
}

// MakeReplayChatCompletionDeps creates [oai.ChatCompletionDeps] that answer the
// n-th chat completion request with the n-th recorded response, converted by
// [ToChatCompletion], without any network access. Requests after the last
// response fail with [ErrReplayExhausted]. The requests themselves are not
// checked against the recording.
//
// Creating the deps allocates the response queue, hence the IO.
func MakeReplayChatCompletionDeps() IO.Kleisli[[]ChatResponse, oai.ChatCompletionDeps] {
	return F.Flow3(
		A.Map(ToChatCompletion),
		ioref.MakeIORef[[]openai.ChatCompletion],
		IO.Map(F.Flow3(
			replayMiddleware,
			newReplayClient,
			oai.MakeChatCompletionDeps,
		)),
	)
}

// TaskResponses lists the recorded responses of a task in order.
func TaskResponses() reader.Reader[Task, []ChatResponse] {
	return F.Flow2(
		MakeTaskTurnsLens().Get,
		A.Map(turnResponse),
	)
}

// TaskRequest returns the first recorded request of a task, the one a replay
// starts from. A task without turns yields None.
func TaskRequest() option.Kleisli[Task, openai.ChatCompletionNewParams] {
	return F.Flow3(
		MakeTaskTurnsLens().Get,
		A.Head[Turn],
		option.Map(turnRequest),
	)
}

// MakeTaskChatCompletionDeps creates replay deps that answer with the recorded
// responses of a task. Together with [TaskRequest] it replays a task through
// session.Run.
//
// Example:
//
//	deps := MakeTaskChatCompletionDeps()(task)()
//	start := TaskRequest()(task)
func MakeTaskChatCompletionDeps() IO.Kleisli[Task, oai.ChatCompletionDeps] {
	return F.Flow2(
		TaskResponses(),
		MakeReplayChatCompletionDeps(),
	)
}
