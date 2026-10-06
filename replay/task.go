package replay

import (
	"encoding/json"

	A "github.com/IBM/fp-go/v2/array"
	ER "github.com/IBM/fp-go/v2/errors"
	F "github.com/IBM/fp-go/v2/function"
	J "github.com/IBM/fp-go/v2/json"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/readerresult"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	openai "github.com/openai/openai-go/v3"
)

// msgToolInvoked is the Msg value of a "Tool invoked" log record.
const msgToolInvoked = "Tool invoked"

// ToolInvocation is the decoded data of a "Tool invoked" log record. It
// contains the tool call identifier, name and input arguments.
//
// fp-go:Lens
type ToolInvocation struct {
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName"`
	Input      json.RawMessage `json:"input,omitempty"`
}

// Turn is one request/response step of a recorded agent session, together
// with the tool invocations the model made after the response.
type Turn struct {
	Request  openai.ChatCompletionNewParams
	Response ChatResponse
	Tools    []ToolInvocation
}

// Task groups the turns of one recorded agent session, identified by its
// task identifier as it appears in the log records.
type Task struct {
	TaskID string
	Turns  []Turn
}

// newTurn is a leaf: a constructor over the three fields of a turn.
func newTurn(request openai.ChatCompletionNewParams, response ChatResponse, tools []ToolInvocation) Turn {
	return Turn{Request: request, Response: response, Tools: tools}
}

// newTask is a leaf: a constructor over the two fields of a task.
func newTask(taskID string, turns []Turn) Task {
	return Task{TaskID: taskID, Turns: turns}
}

// rawBytes is a leaf: Go needs an explicit conversion from the named
// json.RawMessage type to the unnamed []byte the codecs compose over.
func rawBytes(raw json.RawMessage) []byte {
	return raw
}

// completeTurn assembles a turn from the parts decoded out of one chunk of
// records. A chunk whose response was never recorded belongs to an unfinished
// turn and yields None.
func completeTurn(request openai.ChatCompletionNewParams, response Option[ChatResponse], tools []ToolInvocation) Option[Turn] {
	return F.Pipe1(
		response,
		option.Map(F.Bind13of3(newTurn)(request, tools)),
	)
}

// indexWhen keeps the index of the elements that match pred.
func indexWhen[T any](pred Predicate[T]) func(int, T) Option[int] {
	return pair.Unpaired(F.Flow2(
		option.FromPredicate(F.Flow2(pair.Tail[int, T], pred)),
		option.Map(pair.Head[int, T]),
	))
}

// consecutiveRanges pairs every boundary with its successor, turning a list of
// chunk boundaries into the list of chunk ranges.
func consecutiveRanges() reader.Reader[[]int, []Pair[int, int]] {
	return F.Pipe1(
		F.Flow2(A.SliceRight[int](1), A.Zip[int, int]),
		reader.Ap[[]Pair[int, int]](F.Identity[[]int]),
	)
}

// chunkBounds locates the boundaries of the chunks: the index of every element
// that matches pred, terminated by the size of the slice so the last chunk has
// an end. Indices and terminator read the same slice, so they are combined with
// the reader applicative.
func chunkBounds[T any](pred Predicate[T]) reader.Reader[[]T, []int] {
	return F.Pipe1(
		F.Flow2(A.Size[T], A.Push[int]),
		reader.Ap[[]int](A.FilterMapWithIndex(indexWhen(pred))),
	)
}

// chunkAt splits a slice into one chunk per element that matches pred. A chunk
// starts at a matching element and ends before the next one, so elements
// before the first match belong to no chunk and are dropped.
//
// The boundaries and the slicing both read the same slice, so the whole split
// is a reader over it: the ranges are traversed with the reader traversal,
// which hands each range the slice to cut from.
func chunkAt[T any](pred Predicate[T]) reader.Reader[[]T, [][]T] {
	return F.Pipe1(
		chunkBounds(pred),
		reader.Chain(F.Flow2(
			consecutiveRanges(),
			reader.TraverseArray(pair.Paired(A.Slice[T])),
		)),
	)
}

// decodePayload decodes a recorded JSON payload carried in a record message.
func decodePayload[T any]() result.Kleisli[string, T] {
	return F.Flow2(S.ToBytes, J.Unmarshal[T])
}

// startsTurn matches the records that start a turn, the ones carrying a
// recorded request body.
func startsTurn() Predicate[Record] {
	return F.Flow2(RequestPrism().GetOption, option.IsSome[string])
}

// hasTaskID matches the records of one task.
func hasTaskID(taskID string) Predicate[Record] {
	return F.Flow2(MakeRecordTaskIDLens().Get, P.IsStrictEqual[string]()(taskID))
}

// requestOf decodes the request body of the record that starts a chunk. Every
// chunk starts with such a record, so a chunk without one cannot occur.
func requestOf() ReaderResult[[]Record, openai.ChatCompletionNewParams] {
	return F.Flow3(
		A.FindFirstMap(RequestPrism().GetOption),
		result.FromOption[string](ER.OnNone("the turn has no recorded request body")),
		result.Chain(decodePayload[openai.ChatCompletionNewParams]()),
	)
}

// responseOf decodes every response recorded in a chunk and keeps the last of
// them. A chunk without a recorded response yields None.
func responseOf() ReaderResult[[]Record, Option[ChatResponse]] {
	return F.Flow3(
		A.FilterMap(ResponsePrism().GetOption),
		result.TraverseArray(decodePayload[ChatResponse]()),
		result.Map(A.Last[ChatResponse]),
	)
}

// toolInvocationOf decodes the data of a "Tool invoked" record. Records of
// another kind don't match, and neither do records whose data is not a tool
// invocation object: in some log formats the data is a plain string.
func toolInvocationOf() option.Kleisli[Record, ToolInvocation] {
	msgLens := MakeRecordMsgLens()
	dataLens := MakeRecordDataLens()

	return F.Flow3(
		option.FromPredicate(F.Flow2(msgLens.Get, P.IsStrictEqual[string]()(msgToolInvoked))),
		option.Map(F.Flow2(dataLens.Get, rawBytes)),
		option.Chain(F.Flow2(J.Unmarshal[ToolInvocation], result.ToOption[ToolInvocation])),
	)
}

// toolsOf collects the tool invocations recorded in a chunk.
func toolsOf() ReaderResult[[]Record, []ToolInvocation] {
	return F.Flow2(
		A.FilterMap(toolInvocationOf()),
		result.Of[[]ToolInvocation],
	)
}

type (
	// The partial applications of the curried turn constructor. The
	// applicative needs them as explicit type arguments.
	turnFromTools    = func([]ToolInvocation) Option[Turn]
	turnFromResponse = func(Option[ChatResponse]) turnFromTools
)

// turnOf decodes one chunk of records into a turn. Request, response and tool
// invocations are read from the same chunk, so they are applied to the curried
// constructor with the applicative. A payload that doesn't decode fails.
func turnOf() ReaderResult[[]Record, Option[Turn]] {
	return F.Pipe3(
		readerresult.Of[[]Record](F.Curry3(completeTurn)),
		readerresult.Ap[turnFromResponse](requestOf()),
		readerresult.Ap[turnFromTools](responseOf()),
		readerresult.Ap[Option[Turn]](toolsOf()),
	)
}

// turnsOf decodes the turns of one task. The records are split into one chunk
// per recorded request, and every complete chunk becomes a turn.
func turnsOf() ReaderResult[[]Record, []Turn] {
	return F.Flow3(
		chunkAt(startsTurn()),
		result.TraverseArray(turnOf()),
		result.Map(option.CompactArray[Turn]),
	)
}

// taskOf builds the task with a given identifier from the records of a log.
func taskOf(taskID string) ReaderResult[[]Record, Task] {
	return F.Flow4(
		A.Filter(hasTaskID(taskID)),
		turnsOf(),
		result.Map(F.Bind1st(newTask, taskID)),
		result.MapLeft[Task](ER.OnError("task %q", taskID)),
	)
}

// taskIDs lists the task identifiers of a log in order of first appearance.
// Records without a task identifier belong to no task and are skipped.
func taskIDs() reader.Reader[[]Record, []string] {
	return F.Flow3(
		A.Map(MakeRecordTaskIDLens().Get),
		A.Filter(P.Not(S.IsEmpty)),
		A.Uniq(F.Identity[string]),
	)
}

// GroupTasks groups records by taskId and returns the ordered slice of tasks.
// Each task contains the turns built from the request, response and "Tool
// invoked" records that share that taskId.
//
// Records without a taskId are skipped. The tasks are ordered by the first
// occurrence of their taskId. A decode error in any payload fails the result.
//
// Grouping needs every record before it can produce anything, so it is a pure
// operation over a slice. Compose it with [LoadLogFile] or [LoadRecords] to
// read a log, or collect a SeqResult with iterresult.Collect to group an
// already filtered stream. The identifiers and the contents of the tasks are
// both read from the same records, so the reader is flattened.
//
// Example:
//
//	tasks := F.Pipe2(path, LoadLogFile(), ioresult.ChainResultK(GroupTasks()))
func GroupTasks() result.Kleisli[[]Record, []Task] {
	return F.Pipe1(
		F.Flow2(taskIDs(), readerresult.TraverseArray(taskOf)),
		reader.Flatten[[]Record, Result[[]Task]],
	)
}
