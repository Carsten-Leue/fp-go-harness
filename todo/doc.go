// Package todo is the todo list of a session (H3): the items the model keeps
// with the update_todo_list tool, and their rendering into the todo_list
// section of the environment_details block of the next user message.
//
// The model sends the whole list as a Markdown checklist, one item per line:
//
//	[x] Read the plan
//	[-] Implement the tool
//	[ ] Update the README
//
// "[ ]" is pending, "[x]" (or "[X]") completed and "[-]" in progress; a list
// marker ("- ", "* ", "+ ") before the box is allowed, blank lines are
// skipped. [Parse] turns the checklist into typed [Todo] values, so the
// rendering and ACP's plan updates (J12, entries with a status) work on the
// same data. [Render] writes a list back in the same checklist form, which is
// what the todo_list section shows: the model reads the list in the format it
// writes it in. No recording has a todo list, so this format was chosen here.
//
// The tool is generic over the state that holds the list, given as a lens
// ([Tools]), because the session imports this package.
package todo

//go:generate go tool gen lens
