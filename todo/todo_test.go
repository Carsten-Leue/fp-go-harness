package todo

import (
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/prompt"
	L "github.com/IBM/fp-go/v2/optics/lens"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse_Valid(t *testing.T) {
	checklist := "[x] Read the plan\r\n" +
		"\n" +
		"  - [-] Implement the tool  \n" +
		"* [ ] Update the README\n" +
		"+ [X] Tick the step\n"

	assert.Equal(t, result.Of([]Todo{
		MakeTodo("Read the plan", Completed),
		MakeTodo("Implement the tool", InProgress),
		MakeTodo("Update the README", Pending),
		MakeTodo("Tick the step", Completed),
	}), Parse()(checklist))
}

func TestParse_Empty(t *testing.T) {
	assert.Equal(t, result.Of[[]Todo](nil), Parse()(""))
	assert.Equal(t, result.Of[[]Todo](nil), Parse()(" \n\t\n"))
}

func TestParse_Invalid(t *testing.T) {
	for _, line := range []string{
		"Read the plan",
		"[x]",
		"[?] unknown mark",
		"[] no mark",
		"- read the plan",
		"x] half a box",
	} {
		t.Run(line, func(t *testing.T) {
			_, err := result.Unwrap(Parse()("[ ] fine\n" + line))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid todo item")
			assert.Contains(t, err.Error(), line)
		})
	}
}

func TestRender_RoundTrip(t *testing.T) {
	todos := []Todo{
		MakeTodo("Read the plan", Completed),
		MakeTodo("Implement the tool", InProgress),
		MakeTodo("Update the README", Pending),
	}
	checklist := "[x] Read the plan\n[-] Implement the tool\n[ ] Update the README"

	assert.Equal(t, checklist, Render()(todos))
	assert.Equal(t, result.Of(todos), Parse()(Render()(todos)))
}

func TestRender_UnknownStatusIsPending(t *testing.T) {
	assert.Equal(t, "[ ] edited", Render()([]Todo{MakeTodo("edited", Status("done"))}))
}

func TestSection(t *testing.T) {
	assert.Equal(t,
		[]prompt.Section{prompt.MakeSection(prompt.TodoListTag, "[x] one\n[ ] two")},
		Section()([]Todo{MakeTodo("one", Completed), MakeTodo("two", Pending)}),
	)
	assert.Empty(t, Section()(nil))
	assert.Empty(t, Section()([]Todo{}))
}

// callOn runs the update_todo_list call on a list that is the whole state.
func callOn(t *testing.T, args string) result.Result[ToolResult[[]Todo]] {
	t.Helper()

	return UpdateTodoList[[]Todo]()(L.Id[[]Todo]())(args)(t.Context())()
}

func TestUpdateTodoList_ReplacesTheList(t *testing.T) {
	got, err := result.Unwrap(callOn(t, `{"todos":"[x] one\n[-] two"}`))
	require.NoError(t, err)

	assert.Equal(t, UpdatedText, pair.Tail(got))
	assert.Equal(t,
		[]Todo{MakeTodo("one", Completed), MakeTodo("two", InProgress)},
		pair.Head(got)([]Todo{MakeTodo("old", Pending)}),
	)
}

func TestUpdateTodoList_EmptyListClears(t *testing.T) {
	got, err := result.Unwrap(callOn(t, `{"todos":""}`))
	require.NoError(t, err)

	assert.Empty(t, pair.Head(got)([]Todo{MakeTodo("old", Pending)}))
}

func TestUpdateTodoList_InvalidListFails(t *testing.T) {
	_, err := result.Unwrap(callOn(t, `{"todos":"not a checklist"}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid todo item "not a checklist"`)
}

func TestUpdateTodoList_InvalidArgumentsFail(t *testing.T) {
	_, err := result.Unwrap(callOn(t, `not json`))

	require.Error(t, err)
}

func TestTools_Definition(t *testing.T) {
	registry := Tools[[]Todo]()(L.Id[[]Todo]())

	require.Contains(t, registry, UpdateTodoListName)
	definition := registry[UpdateTodoListName].Definition
	assert.Equal(t, UpdateTodoListName, definition.Name)
	assert.Equal(t, []string{"todos"}, definition.Parameters["required"])
}
