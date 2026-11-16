package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/todo"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeUpdateTodoListCompletion is a completion that calls update_todo_list
// once with the checklist.
func makeUpdateTodoListCompletion(t *testing.T, id, checklist string) openai.ChatCompletion {
	t.Helper()

	arguments, err := json.Marshal(todo.UpdateTodoListArgs{Todos: checklist})
	require.NoError(t, err)

	completion := makeToolCallsCompletion(id, todo.UpdateTodoListName)
	completion.Choices[0].Message.ToolCalls[0].Function.Arguments = string(arguments)

	return completion
}

// todoTurn runs one turn whose model calls update_todo_list with the
// checklist and then stops, on a session with the todos.
func todoTurn(t *testing.T, todos []todo.Todo, checklist string) Session {
	t.Helper()

	responses := []openai.ChatCompletion{
		makeUpdateTodoListCompletion(t, "chatcmpl-1", checklist),
		makeStopCompletion("chatcmpl-2", "done"),
	}
	deps := makeSequencedSessionToolDeps(t, responses, tools.ToToolCallerOf[SessionCall]()(TodoTools()), 10)

	s := MakeSession(makeLoopRequest())
	s.todos = todos

	return runTurn(t, s, deps)
}

// TestTodoTools_TurnReplacesTodoList asserts that a turn whose model calls
// update_todo_list replaces the todo list of the session and answers the
// short tool text.
func TestTodoTools_TurnReplacesTodoList(t *testing.T) {
	got := todoTurn(t, []todo.Todo{todo.MakeTodo("old", todo.Pending)}, "[x] one\n[-] two\n[ ] three")

	assert.Equal(t, []todo.Todo{
		todo.MakeTodo("one", todo.Completed),
		todo.MakeTodo("two", todo.InProgress),
		todo.MakeTodo("three", todo.Pending),
	}, Todos()(got))
	assert.Equal(t, todo.UpdatedText, toolMessageText(t, pair.Head(got.history[1]), 3))
}

// TestTodoTools_InvalidListKeepsTodoList asserts that an invalid checklist is
// a tool message with the error and leaves the todo list as it was.
func TestTodoTools_InvalidListKeepsTodoList(t *testing.T) {
	old := []todo.Todo{todo.MakeTodo("old", todo.Pending)}
	got := todoTurn(t, old, "[x] one\nnot an item")

	assert.Equal(t, old, Todos()(got))
	assert.Contains(t, toolMessageText(t, pair.Head(got.history[1]), 3), "invalid todo item")
}

// TestTodoTools_Definition asserts that the registry offers update_todo_list.
func TestTodoTools_Definition(t *testing.T) {
	require.Contains(t, TodoTools(), todo.UpdateTodoListName)
	assert.Equal(t, todo.UpdateTodoListName, TodoTools()[todo.UpdateTodoListName].Definition.Name)
}

// TestSaveLoad_RestoresTodoList asserts that a saved task restores its todo
// list.
func TestSaveLoad_RestoresTodoList(t *testing.T) {
	s := todoTurn(t, nil, "[x] one\n[ ] two")
	store := MakeDefaultTaskStoreDeps(t.TempDir())

	require.Equal(t, result.Of(F.VOID), saveTask(t, store, "task-1", s))

	loaded, err := result.Unwrap(loadTask(t, store, "task-1"))
	require.NoError(t, err)
	assert.Equal(t, []todo.Todo{
		todo.MakeTodo("one", todo.Completed),
		todo.MakeTodo("two", todo.Pending),
	}, Todos()(loaded))
}

// TestSave_EmptyTodoListIsLeftOut asserts that a session without todos is
// stored without a todos field, as before H3.
func TestSave_EmptyTodoListIsLeftOut(t *testing.T) {
	assert.NotContains(t, encoded(t, MakeSession(makeLoopRequest())), `"todos"`)
}

// TestLoad_FileWithoutTodos asserts that a task file written before H3,
// without a todos field, loads with an empty todo list.
func TestLoad_FileWithoutTodos(t *testing.T) {
	dir := t.TempDir()
	old := `{"history":[],"current":{"model":"gpt-test","messages":[{"role":"user","content":"hi"}]},"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2},"iterations":1}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "task-1.json"), []byte(old), 0o644))

	loaded, err := result.Unwrap(loadTask(t, MakeDefaultTaskStoreDeps(dir), "task-1"))
	require.NoError(t, err)

	assert.Empty(t, Todos()(loaded))
	assert.Equal(t, 1, loaded.iterations)
	assert.Len(t, loaded.current.Messages, 1)
}
