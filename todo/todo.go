package todo

import (
	"regexp"
	"strings"

	"github.com/Carsten-Leue/fp-go-harness/prompt"
	A "github.com/IBM/fp-go/v2/array"
	ER "github.com/IBM/fp-go/v2/errors"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/reader"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
)

// Status is the state of a [Todo]. The values are those of the status of an
// entry of an ACP plan, so J12 can pass them on as they are.
type Status string

const (
	Pending    Status = "pending"
	InProgress Status = "in_progress"
	Completed  Status = "completed"
)

// Todo is one item of a todo list: its text and its status.
//
// fp-go:Lens
type Todo struct {
	Text   string `json:"text"`
	Status Status `json:"status"`
}

// itemLine separates the items of a checklist.
const itemLine = "\n"

// checklistItem matches one trimmed line of a checklist: an optional list
// marker, the box with its mark, and the text after it.
var checklistItem = regexp.MustCompile(`^(?:[-*+]\s+)?\[([ xX-])\]\s+(\S.*)$`)

// The submatches of [checklistItem].
const (
	markGroup = 1
	textGroup = 2
)

// MakeTodo is a leaf: the constructor of a [Todo].
func MakeTodo(text string, status Status) Todo {
	return Todo{Text: text, Status: status}
}

// statusOfMark maps the mark in a box to its status.
func statusOfMark() option.Kleisli[string, Status] {
	return F.Flip(R.Lookup[Status, string])(Record[string, Status]{
		" ": Pending,
		"x": Completed,
		"X": Completed,
		"-": InProgress,
	})
}

// markOfStatus is the mark of a status in a rendered box. A status that is
// not one of the constants, e.g. from an edited task file, renders as
// pending.
func markOfStatus() func(Status) string {
	return F.Flow2(
		F.Flip(R.Lookup[string, Status])(Record[Status, string]{
			Pending:    " ",
			Completed:  "x",
			InProgress: "-",
		}),
		option.GetOrElse(F.Constant(" ")),
	)
}

// itemLines are the non-blank lines of a checklist, trimmed.
func itemLines() A.Kleisli[string, string] {
	return F.Flow3(
		F.Bind2nd(strings.Split, itemLine),
		A.Map(strings.TrimSpace),
		A.Filter(S.IsNonEmpty),
	)
}

// todoOfMatch builds a [Todo] from the submatches of [checklistItem]; text
// and status are independent reads of the submatches.
func todoOfMatch() option.Kleisli[[]string, Todo] {
	return F.Pipe2(
		F.Flow2(A.Lookup[string](textGroup), option.Map(F.Curry2(MakeTodo))),
		reader.Map[[]string](F.Flip(option.Ap[Todo, Status])),
		reader.Ap[Option[Todo]](F.Flow2(A.Lookup[string](markGroup), option.Chain(statusOfMark()))),
	)
}

// todoOfLine is the [Todo] of a trimmed line of a checklist, none for a line
// that is not an item.
func todoOfLine() option.Kleisli[string, Todo] {
	return F.Flow3(
		checklistItem.FindStringSubmatch,
		option.FromPredicate(A.IsNonEmpty[string]),
		option.Chain(todoOfMatch()),
	)
}

// parseItem parses a trimmed line of a checklist. A line that is not an item
// fails with an error that quotes it and names the format; the error and the
// item are independent reads of the line.
func parseItem() result.Kleisli[string, Todo] {
	return F.Pipe1(
		F.Flow3(
			ER.OnSome[string]("invalid todo item %q: each line must be a checklist item, \"[ ] text\" (pending), \"[-] text\" (in progress) or \"[x] text\" (completed)"),
			F.Constant[error],
			result.FromOption[Todo],
		),
		reader.Ap[Result[Todo]](todoOfLine()),
	)
}

// Parse parses a Markdown checklist into its items, in order. Blank lines are
// skipped, so an empty text is the empty list; any other line that is not an
// item fails the whole list.
func Parse() result.Kleisli[string, []Todo] {
	return F.Flow2(
		itemLines(),
		result.TraverseArray(parseItem()),
	)
}

// formatItem is a leaf: one formatter over the mark and the text of an item.
func formatItem(mark, text string) string {
	return "[" + mark + "] " + text
}

// renderTodo renders an item as a line of a checklist; mark and text are
// independent reads of the item.
func renderTodo() func(Todo) string {
	textLens := MakeTodoTextLens()
	statusLens := MakeTodoStatusLens()

	return F.Pipe1(
		F.Flow3(statusLens.Get, markOfStatus(), F.Curry2(formatItem)),
		reader.Ap[string](textLens.Get),
	)
}

// Render renders a list as a Markdown checklist, one item per line, in the
// form [Parse] reads.
func Render() func([]Todo) string {
	return F.Flow2(
		A.Map(renderTodo()),
		S.Join(itemLine),
	)
}

// Section is the todo_list section of the environment_details block for a
// list: the checklist of [Render]. An empty list has no section.
func Section() A.Kleisli[[]Todo, prompt.Section] {
	return F.Flow3(
		option.FromPredicate(A.IsNonEmpty[Todo]),
		option.Map(F.Flow2(Render(), F.Bind1st(prompt.MakeSection, prompt.TodoListTag))),
		A.FromOption[prompt.Section],
	)
}
