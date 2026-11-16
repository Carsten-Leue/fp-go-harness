package session

import (
	"github.com/Carsten-Leue/fp-go-harness/todo"
)

// TodoTools is wiring: the session tools of the todo list (H3),
// update_todo_list over the todo list of the [Session]. They only change the
// session, so every mode has them; the CLI merges them into the lifted
// registry of the mode.
func TodoTools() SessionRegistry {
	return todo.Tools[Session]()(MakeSessiontodosLens())
}

// Todos is the todo list of a session, empty until the model calls
// update_todo_list.
func Todos() Reader[Session, []todo.Todo] {
	todosLens := MakeSessiontodosLens()

	return todosLens.Get
}
