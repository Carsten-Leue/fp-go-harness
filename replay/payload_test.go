package replay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/iterator/iter"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/iterator/iterresult"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/result"
	openai "github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func payloadRecord(msg string) Record {
	return Record{Module: "HarnessProvider.payload", Level: "debug", Msg: msg}
}

func records(recs ...Record) SeqResult[Record] {
	return iterresult.FromSeq(iter.From(recs...))
}

const recordedRequest = `{"model":"opus-5.5-medium","max_tokens":20000,"messages":[{"role":"system","content":"<role_definition>"},{"role":"user","content":"hi"}]}`

const recordedResponse = `{
  "taskId": "t-1",
  "id": "chatcmpl-1",
  "modelId": "opus-5.5-medium",
  "finishReason": "tool-calls",
  "usage": {
    "inputTokens": 141522,
    "inputTokenDetails": {"noCacheTokens": 7822, "cacheReadTokens": 133700},
    "outputTokens": 551,
    "outputTokenDetails": {"textTokens": 551, "reasoningTokens": 0},
    "totalTokens": 142073
  },
  "text": "",
  "toolCalls": [
    {"type": "tool-call", "toolCallId": "tooluse_1", "toolName": "execute_command", "input": {"command": "git status"}}
  ],
  "responseMessages": [
    {"role": "assistant", "content": [
      {"type": "text", "text": "Checking."},
      {"type": "tool-call", "toolCallId": "tooluse_1", "toolName": "execute_command", "input": {"command": "git status"}}
    ]}
  ]
}`

func TestPayloadPrism(t *testing.T) {
	p := RequestPrism()

	assert.Equal(t, option.Some(`{"model":"m"}`), p.GetOption(payloadRecord(RequestPrefix+`{"model":"m"}`)))
	assert.Equal(t, option.None[string](), p.GetOption(payloadRecord(ResponsePrefix+`{}`)))
	assert.Equal(t, option.None[string](), p.GetOption(payloadRecord("Starting agent loop")))
	assert.Equal(t, RequestPrefix+"{}", p.ReverseGet("{}").Msg)
}

func TestRequests(t *testing.T) {
	requests, err := result.Unwrap(iterresult.Collect(Requests()(records(
		payloadRecord("Starting agent loop"),
		payloadRecord(RequestPrefix+recordedRequest),
		payloadRecord(ResponsePrefix+recordedResponse),
	)))())
	require.NoError(t, err)
	require.Len(t, requests, 1)

	req := requests[0]
	assert.Equal(t, openai.ChatModel("opus-5.5-medium"), req.Model)
	assert.Equal(t, openai.Int(20000), req.MaxTokens)
	require.Len(t, req.Messages, 2)
	require.NotNil(t, req.Messages[0].OfSystem)
	assert.Equal(t, openai.String("<role_definition>"), req.Messages[0].OfSystem.Content.OfString)
	require.NotNil(t, req.Messages[1].OfUser)
	assert.Equal(t, openai.String("hi"), req.Messages[1].OfUser.Content.OfString)
}

func TestResponses(t *testing.T) {
	toolCall := ContentPart{
		Type:       "tool-call",
		ToolCallID: "tooluse_1",
		ToolName:   "execute_command",
		Input:      json.RawMessage(`{"command": "git status"}`),
	}

	assert.Equal(t, result.Of([]ChatResponse{{
		TaskID:       "t-1",
		ID:           "chatcmpl-1",
		ModelID:      "opus-5.5-medium",
		FinishReason: "tool-calls",
		Usage: ResponseUsage{
			InputTokens:        141522,
			InputTokenDetails:  InputTokenDetails{NoCacheTokens: 7822, CacheReadTokens: 133700},
			OutputTokens:       551,
			OutputTokenDetails: OutputTokenDetails{TextTokens: 551},
			TotalTokens:        142073,
		},
		ToolCalls: []ContentPart{toolCall},
		ResponseMessages: []ResponseMessage{{
			Role:    "assistant",
			Content: []ContentPart{{Type: "text", Text: "Checking."}, toolCall},
		}},
	}}), iterresult.Collect(Responses()(records(
		payloadRecord(RequestPrefix+recordedRequest),
		payloadRecord(ResponsePrefix+recordedResponse),
	)))())
}

func TestResponsesInvalidPayload(t *testing.T) {
	assert.True(t, result.IsLeft(iterresult.Collect(Responses()(records(
		payloadRecord(ResponsePrefix+`{`),
	)))()))
}

// countPayloads counts the payloads op extracts from a log file.
func countPayloads[T any](op iterresult.Operator[Record, T]) ioresult.Kleisli[string, int] {
	return F.Flow4(
		ReadLogFile(),
		op,
		iterresult.Collect[T],
		ioresult.Map(A.Size[T]),
	)
}

// TestPayloadRecordings checks the payload counts of PLAN.md, section 0, over
// all recorded logs.
func TestPayloadRecordings(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(recordingsDir(t), "*.log"))
	require.NoError(t, err)
	if len(files) == 0 {
		t.Skip("recordings not available")
	}

	var requests, responses int
	for _, file := range files {
		n, err := result.Unwrap(countPayloads(Requests())(file)())
		require.NoError(t, err, file)
		requests += n

		m, err := result.Unwrap(countPayloads(Responses())(file)())
		require.NoError(t, err, file)
		responses += m
	}

	assert.Equal(t, 126, requests)
	assert.Equal(t, 71, responses)
}

func TestPayloadRecordingResponsesHaveTasks(t *testing.T) {
	path := filepath.Join(recordingsDir(t), "bob-bob4z-shell-proxy-p7204-20261006T082140.1.log")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("recording not available: %v", err)
	}

	responses, err := result.Unwrap(iterresult.Collect(Responses()(ReadLogFile()(path)))())
	require.NoError(t, err)

	require.Len(t, responses, 5)
	for _, r := range responses {
		assert.NotEmpty(t, r.TaskID)
		assert.Contains(t, []string{"stop", "tool-calls"}, r.FinishReason)
		assert.Positive(t, r.Usage.TotalTokens)
	}
}
