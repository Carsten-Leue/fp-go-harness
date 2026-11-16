package session

import (
	"github.com/Carsten-Leue/fp-go-harness/todo"
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/optics/iso"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	openai "github.com/openai/openai-go/v3"
)

// SessionDTO is the exported form of a [Session], with one field per field of
// the session, for encoding it as JSON. [SessionIso] converts between both.
//
// The todo list (H3) is left out of the JSON when it is empty, so a task file
// written before H3, which has no todos, loads with an empty list.
//
// fp-go:Lens
type SessionDTO struct {
	History    []HistoryEntryDTO              `json:"history"`
	Current    openai.ChatCompletionNewParams `json:"current"`
	Usage      openai.CompletionUsage         `json:"usage"`
	Iterations int                            `json:"iterations"`
	Todos      []todo.Todo                    `json:"todos,omitempty"`
}

// HistoryEntryDTO is the exported form of a [HistoryEntry]: a request and the
// completion the model answered it with.
//
// fp-go:Lens
type HistoryEntryDTO struct {
	Request  openai.ChatCompletionNewParams `json:"request"`
	Response *openai.ChatCompletion         `json:"response"`
}

// The partial applications of the curried constructors of [Session] and
// [SessionDTO] after the history, in order of application. They are named
// because [reader.Ap] cannot infer them.
type (
	fromTodos[T any]      = func([]todo.Todo) T
	fromIterations[T any] = func(int) fromTodos[T]
	fromUsage[T any]      = func(openai.CompletionUsage) fromIterations[T]
	fromCurrent[T any]    = func(openai.ChatCompletionNewParams) fromUsage[T]
)

// makeSessionDTO is a leaf: the constructor of a [SessionDTO].
func makeSessionDTO(history []HistoryEntryDTO, current openai.ChatCompletionNewParams, usage openai.CompletionUsage, iterations int, todos []todo.Todo) SessionDTO {
	return SessionDTO{history, current, usage, iterations, todos}
}

// makeSessionOf is a leaf: the constructor of a [Session] over all of its
// fields.
func makeSessionOf(history History, current openai.ChatCompletionNewParams, usage openai.CompletionUsage, iterations int, todos []todo.Todo) Session {
	return Session{history, current, usage, iterations, todos}
}

// makeHistoryEntryDTO is a leaf: the constructor of a [HistoryEntryDTO].
func makeHistoryEntryDTO(request openai.ChatCompletionNewParams, response *openai.ChatCompletion) HistoryEntryDTO {
	return HistoryEntryDTO{request, response}
}

// historyEntryOf converts a [HistoryEntryDTO] back to its pair; request and
// response are independent reads of the entry.
func historyEntryOf() Reader[HistoryEntryDTO, HistoryEntry] {
	requestLens := MakeHistoryEntryDTORequestLens()
	responseLens := MakeHistoryEntryDTOResponseLens()

	return F.Pipe1(
		F.Flow2(requestLens.Get, F.Curry2(pair.MakePair[openai.ChatCompletionNewParams, *openai.ChatCompletion])),
		reader.Ap[HistoryEntry](responseLens.Get),
	)
}

// HistoryEntryIso converts between a [HistoryEntry] and its
// [HistoryEntryDTO].
func HistoryEntryIso() Iso[HistoryEntry, HistoryEntryDTO] {
	return iso.MakeIso(pair.Paired(makeHistoryEntryDTO), historyEntryOf())
}

// sessionDTOOf converts a [Session] to its [SessionDTO], one independent read
// of the session per field.
func sessionDTOOf() Reader[Session, SessionDTO] {
	historyLens := MakeSessionhistoryLens()
	currentLens := MakeSessioncurrentLens()
	usageLens := MakeSessionusageLens()
	iterLens := MakeSessioniterationsLens()
	todosLens := MakeSessiontodosLens()

	return F.Pipe4(
		F.Flow3(historyLens.Get, A.Map(HistoryEntryIso().Get), F.Curry5(makeSessionDTO)),
		reader.Ap[fromUsage[SessionDTO]](currentLens.Get),
		reader.Ap[fromIterations[SessionDTO]](usageLens.Get),
		reader.Ap[fromTodos[SessionDTO]](iterLens.Get),
		reader.Ap[SessionDTO](todosLens.Get),
	)
}

// sessionOf converts a [SessionDTO] back to its [Session], one independent
// read of the DTO per field.
func sessionOf() Reader[SessionDTO, Session] {
	historyLens := MakeSessionDTOHistoryLens()
	currentLens := MakeSessionDTOCurrentLens()
	usageLens := MakeSessionDTOUsageLens()
	iterLens := MakeSessionDTOIterationsLens()
	todosLens := MakeSessionDTOTodosLens()

	return F.Pipe4(
		F.Flow3(historyLens.Get, A.Map(HistoryEntryIso().ReverseGet), F.Curry5(makeSessionOf)),
		reader.Ap[fromUsage[Session]](currentLens.Get),
		reader.Ap[fromIterations[Session]](usageLens.Get),
		reader.Ap[fromTodos[Session]](iterLens.Get),
		reader.Ap[Session](todosLens.Get),
	)
}

// SessionIso converts between a [Session] and its [SessionDTO]: the DTO
// exports every field of the session, so no state is lost in either
// direction.
func SessionIso() Iso[Session, SessionDTO] {
	return iso.MakeIso(sessionDTOOf(), sessionOf())
}
