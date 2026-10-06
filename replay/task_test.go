package replay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/result"
	openai "github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// taskRecords builds the record slice from the given records.
func taskRecords(recs ...Record) []Record {
	return recs
}

// taskID used in the unit tests.
const testTaskID = "task-1"

// makeRequestRec returns a HarnessProvider.payload record that carries the
// given JSON request body.
func makeRequestRec(taskID, body string) Record {
	return Record{
		TaskID: taskID,
		Module: "HarnessProvider.payload",
		Level:  "debug",
		Msg:    RequestPrefix + body,
	}
}

// makeResponseRec returns a HarnessProvider.payload record that carries the
// given JSON response body.
func makeResponseRec(taskID, body string) Record {
	return Record{
		TaskID: taskID,
		Module: "HarnessProvider.payload",
		Level:  "debug",
		Msg:    ResponsePrefix + body,
	}
}

// makeToolRec returns a "Tool invoked" record with the given JSON data.
func makeToolRec(taskID string, data json.RawMessage) Record {
	return Record{
		TaskID: taskID,
		Module: "ChatManager",
		Level:  "debug",
		Msg:    msgToolInvoked,
		Data:   data,
	}
}

const minimalRequest = `{"model":"deepseek-chat","max_tokens":100,"messages":[]}`

const minimalResponse = `{"taskId":"task-1","id":"cmpl-1","modelId":"deepseek-chat","finishReason":"stop","usage":{"inputTokens":10,"outputTokens":5,"totalTokens":15}}`

const toolCallResponse = `{"taskId":"task-1","id":"cmpl-2","modelId":"deepseek-chat","finishReason":"tool-calls","usage":{"inputTokens":20,"outputTokens":8,"totalTokens":28},"toolCalls":[{"type":"tool-call","toolCallId":"tc-1","toolName":"read_file","input":{"path":"foo.go"}}]}`

const toolInvocationData = `{"toolCallId":"tc-1","toolName":"read_file","input":{"path":"foo.go"}}`

func TestGroupTasksEmpty(t *testing.T) {
	tasks, err := result.Unwrap(GroupTasks()(taskRecords()))
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestGroupTasksSkipsRecordsWithoutTaskID(t *testing.T) {
	tasks, err := result.Unwrap(GroupTasks()(taskRecords(
		Record{Module: "Extension", Msg: "Activating extension"},
		Record{Module: "ChatManager", Msg: "Starting agent loop"},
	)))
	require.NoError(t, err)
	assert.Empty(t, tasks)
}

func TestGroupTasksSingleTurn(t *testing.T) {
	tasks, err := result.Unwrap(GroupTasks()(taskRecords(
		makeRequestRec(testTaskID, minimalRequest),
		makeResponseRec(testTaskID, minimalResponse),
	)))
	require.NoError(t, err)
	require.Len(t, tasks, 1)

	task := tasks[0]
	assert.Equal(t, testTaskID, task.TaskID)
	require.Len(t, task.Turns, 1)

	turn := task.Turns[0]
	assert.Equal(t, openai.ChatModel("deepseek-chat"), turn.Request.Model)
	assert.Equal(t, "cmpl-1", turn.Response.ID)
	assert.Equal(t, "stop", turn.Response.FinishReason)
	assert.Empty(t, turn.Tools)
}

func TestGroupTasksTurnWithToolInvocations(t *testing.T) {
	tasks, err := result.Unwrap(GroupTasks()(taskRecords(
		makeRequestRec(testTaskID, minimalRequest),
		makeResponseRec(testTaskID, toolCallResponse),
		makeToolRec(testTaskID, json.RawMessage(toolInvocationData)),
		makeRequestRec(testTaskID, minimalRequest),
		makeResponseRec(testTaskID, minimalResponse),
	)))
	require.NoError(t, err)
	require.Len(t, tasks, 1)

	task := tasks[0]
	require.Len(t, task.Turns, 2)

	// First turn: tool-calls finish, one tool invocation.
	first := task.Turns[0]
	assert.Equal(t, "tool-calls", first.Response.FinishReason)
	require.Len(t, first.Tools, 1)
	assert.Equal(t, "read_file", first.Tools[0].ToolName)
	assert.Equal(t, "tc-1", first.Tools[0].ToolCallID)

	// Second turn: stop finish, no tool invocations.
	second := task.Turns[1]
	assert.Equal(t, "stop", second.Response.FinishReason)
	assert.Empty(t, second.Tools)
}

func TestGroupTasksMultipleTasks(t *testing.T) {
	tasks, err := result.Unwrap(GroupTasks()(taskRecords(
		makeRequestRec("task-A", minimalRequest),
		makeRequestRec("task-B", minimalRequest),
		makeResponseRec("task-A", minimalResponse),
		makeResponseRec("task-B", minimalResponse),
	)))
	require.NoError(t, err)
	require.Len(t, tasks, 2)

	// Tasks ordered by first occurrence.
	assert.Equal(t, "task-A", tasks[0].TaskID)
	assert.Equal(t, "task-B", tasks[1].TaskID)
}

func TestGroupTasksResponseWithoutRequestIsSkipped(t *testing.T) {
	// A response without a preceding request should be silently dropped.
	tasks, err := result.Unwrap(GroupTasks()(taskRecords(
		makeResponseRec(testTaskID, minimalResponse),
		makeRequestRec(testTaskID, minimalRequest),
		makeResponseRec(testTaskID, minimalResponse),
	)))
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Len(t, tasks[0].Turns, 1)
}

func TestGroupTasksInvalidRequestPayloadFails(t *testing.T) {
	tasks := GroupTasks()(taskRecords(
		makeRequestRec(testTaskID, `{`),
	))
	assert.True(t, result.IsLeft(tasks))
}

func TestGroupTasksInvalidResponsePayloadFails(t *testing.T) {
	tasks := GroupTasks()(taskRecords(
		makeRequestRec(testTaskID, minimalRequest),
		makeResponseRec(testTaskID, `{`),
	))
	assert.True(t, result.IsLeft(tasks))
}

func TestGroupTasksInvalidToolInvocationIsSkipped(t *testing.T) {
	// A "Tool invoked" record whose data cannot be decoded as a ToolInvocation
	// object is silently skipped (the log format may be a plain string).
	tasks, err := result.Unwrap(GroupTasks()(taskRecords(
		makeRequestRec(testTaskID, minimalRequest),
		makeResponseRec(testTaskID, toolCallResponse),
		makeToolRec(testTaskID, json.RawMessage(`"some-tool-name"`)),
	)))
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	// The turn is committed at end-of-stream; tools list is empty because the
	// only tool record was a plain string.
	require.Len(t, tasks[0].Turns, 1)
	assert.Empty(t, tasks[0].Turns[0].Tools)
}

// TestGroupTasksRecording exercises GroupTasks on a full recorded log file and
// checks the expected task/turn counts. It is skipped when the recordings
// directory is not configured.
func TestGroupTasksRecording(t *testing.T) {
	path := filepath.Join(recordingsDir(t), "bob-bob4z-shell-proxy-p7204-20261006T082140.1.log")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("recording not available: %v", err)
	}

	tasks, err := result.Unwrap(F.Pipe2(path, LoadLogFile(), ioresult.ChainResultK(GroupTasks()))())
	require.NoError(t, err)

	require.Len(t, tasks, 1, "the recording contains one task")
	assert.Len(t, tasks[0].Turns, 5, "the task has 5 turns (one per response)")
}

// isZero matches the elements that start a chunk in the chunkAt tests.
func isZero() Predicate[int] {
	return P.IsStrictEqual[int]()(0)
}

func TestChunkAt(t *testing.T) {
	tests := []struct {
		name     string
		input    []int
		expected [][]int
	}{
		{"empty", nil, nil},
		{"without a match", []int{1, 2, 3}, nil},
		{"elements before the first match are dropped", []int{1, 2, 0, 3}, [][]int{{0, 3}}},
		{"one chunk per match", []int{0, 1, 0, 2, 3}, [][]int{{0, 1}, {0, 2, 3}}},
		{"adjacent matches", []int{0, 0, 1}, [][]int{{0}, {0, 1}}},
		{"trailing match", []int{0, 1, 0}, [][]int{{0, 1}, {0}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, chunkAt(isZero())(tt.input))
		})
	}
}
