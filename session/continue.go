package session

import (
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/reader"
	openai "github.com/openai/openai-go/v3"
)

// Continue starts the next turn of a session: it appends a user message with
// the given text to the session's request and resets the iteration counter,
// so the turn gets the full [LoopDeps.GetMaxIterations] budget.
//
// The history and the accumulated usage are kept. Applied to the session of
// the [TurnResult] of [Run], whose request already holds the model's last
// answer, the next [Run] sends the whole conversation. After a failed or
// cancelled turn the session holds what the turn got to, and the new message
// follows it.
func Continue() func(string) Endomorphism[Session] {
	currentLens := MakeSessioncurrentLens()
	iterLens := MakeSessioniterationsLens()
	messagesLens := oai.MakeChatCompletionNewParamsMessagesLens()

	return F.Flow5(
		openai.UserMessage[string],
		A.Push[openai.ChatCompletionMessageParamUnion],
		messagesLens.Modify,
		currentLens.Modify,
		reader.Map[Session](iterLens.Set(0)),
	)
}
