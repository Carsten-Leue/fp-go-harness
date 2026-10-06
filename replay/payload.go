package replay

import (
	"encoding/json"
	"strings"

	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/iterator/iter"
	"github.com/IBM/fp-go/v2/iterator/iterresult"
	J "github.com/IBM/fp-go/v2/json"
	"github.com/IBM/fp-go/v2/optics/prism"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	openai "github.com/openai/openai-go/v3"
)

// Message prefixes of the payload records. The JSON payload follows the prefix
// in the message text, not in the data field.
const (
	RequestPrefix  = "HTTP request body for /chat/completions:\n"
	ResponsePrefix = "Assembled chat response:\n"
)

// ContentPart is a text or tool-call part of an assembled response. The
// tool calls of a response have the same shape.
//
// fp-go:Lens
type ContentPart struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
}

// ResponseMessage is a message the model added to the conversation.
//
// fp-go:Lens
type ResponseMessage struct {
	Role    string        `json:"role"`
	Content []ContentPart `json:"content"`
}

// InputTokenDetails splits the input tokens of a request.
//
// fp-go:Lens
type InputTokenDetails struct {
	NoCacheTokens   int64 `json:"noCacheTokens"`
	CacheReadTokens int64 `json:"cacheReadTokens"`
}

// OutputTokenDetails splits the output tokens of a request.
//
// fp-go:Lens
type OutputTokenDetails struct {
	TextTokens      int64 `json:"textTokens"`
	ReasoningTokens int64 `json:"reasoningTokens"`
}

// ResponseUsage is the token usage of one request.
//
// fp-go:Lens
type ResponseUsage struct {
	InputTokens        int64              `json:"inputTokens"`
	InputTokenDetails  InputTokenDetails  `json:"inputTokenDetails"`
	OutputTokens       int64              `json:"outputTokens"`
	OutputTokenDetails OutputTokenDetails `json:"outputTokenDetails"`
	TotalTokens        int64              `json:"totalTokens"`
}

// ChatResponse is the payload of an "Assembled chat response:" record, the
// response of one streamed request after its chunks have been combined.
// FinishReason is "stop" or "tool-calls".
//
// fp-go:Lens
type ChatResponse struct {
	TaskID           string            `json:"taskId"`
	ID               string            `json:"id"`
	ModelID          string            `json:"modelId"`
	FinishReason     string            `json:"finishReason"`
	Usage            ResponseUsage     `json:"usage"`
	Text             string            `json:"text"`
	ToolCalls        []ContentPart     `json:"toolCalls"`
	ResponseMessages []ResponseMessage `json:"responseMessages"`
}

// prefixPrism focuses on the rest of a string after prefix. Strings without
// the prefix don't match.
func prefixPrism(prefix string) Prism[string, string] {
	return prism.MakePrismWithName(
		F.Bind2nd(option.Optionize2(strings.CutPrefix), prefix),
		S.Prepend(prefix),
		"Prefix("+prefix+")",
	)
}

// payloadPrism focuses on the payload text of the records whose message
// starts with prefix.
func payloadPrism(prefix string) Prism[Record, string] {
	return F.Pipe1(
		MakeRecordMsgPrism(),
		prism.Compose[Record](prefixPrism(prefix)),
	)
}

// RequestPrism focuses on the JSON body of a recorded /chat/completions request.
func RequestPrism() Prism[Record, string] {
	return payloadPrism(RequestPrefix)
}

// ResponsePrism focuses on the JSON of a recorded assembled chat response.
func ResponsePrism() Prism[Record, string] {
	return payloadPrism(ResponsePrefix)
}

// extract keeps the records p matches and decodes their payloads. Records that
// don't match are skipped, a payload that doesn't decode becomes an error
// element.
func extract[A any](p Prism[Record, string], decode result.Kleisli[[]byte, A]) iterresult.Operator[Record, A] {
	return iterresult.Chain(F.Flow2(
		p.GetOption,
		option.Fold(
			iter.Empty[Result[A]],
			F.Flow3(S.ToBytes, decode, iterresult.FromEither[A]),
		),
	))
}

// Requests picks the recorded /chat/completions requests out of a record
// stream and decodes them.
//
// Example:
//
//	requests := Requests()(ReadLogFile()(path))
func Requests() iterresult.Operator[Record, openai.ChatCompletionNewParams] {
	return extract(RequestPrism(), J.Unmarshal[openai.ChatCompletionNewParams])
}

// Responses picks the recorded assembled chat responses out of a record stream
// and decodes them.
func Responses() iterresult.Operator[Record, ChatResponse] {
	return extract(ResponsePrism(), J.Unmarshal[ChatResponse])
}
