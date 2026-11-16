package todo

import (
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	F "github.com/IBM/fp-go/v2/function"
	J "github.com/IBM/fp-go/v2/json"
	L "github.com/IBM/fp-go/v2/optics/lens"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	openai "github.com/openai/openai-go/v3"
)

// UpdateTodoListName is the name under which the model calls the
// update_todo_list tool.
const UpdateTodoListName = "update_todo_list"

// UpdatedText is the text of a successful update_todo_list call. The list
// itself shows in the environment_details of the next user message, so the
// text stays short.
const UpdatedText = "Todo list updated"

// UpdateTodoListArgs are the arguments of an update_todo_list call: the whole
// list as a Markdown checklist, see [Parse].
//
// fp-go:Lens
type UpdateTodoListArgs struct {
	Todos string `json:"todos"`
}

// parseArgs decodes the JSON arguments of a call and parses their checklist.
func parseArgs() result.Kleisli[string, []Todo] {
	todosLens := MakeUpdateTodoListArgsTodosLens()

	return F.Flow3(
		S.ToBytes,
		J.Unmarshal[UpdateTodoListArgs],
		result.Chain(F.Flow2(todosLens.Get, Parse())),
	)
}

// replaceWith is the result of a successful call for a state whose list a
// lens focuses on: the change sets the new list, the text is [UpdatedText].
func replaceWith[T any]() Reader[Lens[T, []Todo], func([]Todo) ToolResult[T]] {
	return F.Flow2(
		F.Flip(L.Set[T, []Todo]),
		reader.Map[[]Todo](pair.FromTail[Endomorphism[T]](UpdatedText)),
	)
}

// UpdateTodoList is the call of update_todo_list for a state whose list a
// lens focuses on: the parsed checklist replaces the list of the state.
//
// Invalid arguments and an invalid checklist fail the call;
// [tools.MakeStateToolCall] turns the failure into a tool message with the
// error and the identity, so the list stays as it was.
func UpdateTodoList[T any]() Reader[Lens[T, []Todo], StateCall[T]] {
	return F.Flow4(
		replaceWith[T](),
		result.Map[[]Todo, ToolResult[T]],
		reader.Local[Result[ToolResult[T]]](parseArgs()),
		reader.Map[string](thunk.FromResult[ToolResult[T]]),
	)
}

// updateTodoListDefinition describes update_todo_list to the model: one
// required string parameter, the whole checklist.
//
// It is wiring: a getter that builds the plain definition value.
func updateTodoListDefinition() openai.FunctionDefinitionParam {
	return openai.FunctionDefinitionParam{
		Name: UpdateTodoListName,
		Description: openai.String("Replace the entire todo list with an updated checklist that reflects the current " +
			"state of the task. Always send the full list; it overwrites the previous one. Use it to plan a " +
			"longer task step by step, mark an item in progress when you start it and completed when it is done, " +
			"and add items you discover. The current list is shown in the environment_details of the next user " +
			"message. An empty list clears it."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"todos": map[string]any{
					"type": "string",
					"description": "The full todo list as a single-level Markdown checklist, one item per line: " +
						"[ ] for pending, [-] for in progress, [x] for completed.",
				},
			},
			"required": []string{"todos"},
		},
	}
}

// MakeUpdateTodoListTool builds the update_todo_list tool for a state whose
// list a lens focuses on.
func MakeUpdateTodoListTool[T any]() Reader[Lens[T, []Todo], tools.StateTool[T]] {
	return F.Flow2(
		UpdateTodoList[T](),
		F.Curry2(tools.MakeToolOf[StateCall[T]])(updateTodoListDefinition()),
	)
}

// Tools builds the registry of the todo tools for a state whose list a lens
// focuses on: update_todo_list. It only changes the state, so every mode has
// it.
func Tools[T any]() Reader[Lens[T, []Todo], tools.StateRegistry[T]] {
	return F.Flow3(
		MakeUpdateTodoListTool[T](),
		A.Of[tools.StateTool[T]],
		tools.ToToolRegistryOf[StateCall[T]](),
	)
}
