package session

import (
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/endomorphism"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	S "github.com/IBM/fp-go/v2/string"
	openai "github.com/openai/openai-go/v3"
)

// ModeRequest is the request a mode starts with, paired with the ID of the
// mode, the same type as mode.ModeRequest: package mode builds it, see
// mode.Request.
type ModeRequest = Pair[string, openai.ChatCompletionNewParams]

// isSystemMessage matches the system messages of a request.
func isSystemMessage() Predicate[openai.ChatCompletionMessageParamUnion] {
	return F.Flow2(
		oai.MakeChatCompletionMessageParamUnionOfSystemPrism().GetOption,
		option.IsSome[*openai.ChatCompletionSystemMessageParam],
	)
}

// withSystemMessages replaces the system messages of a conversation with
// those of a request: the request's system messages come first, then every
// other message of the conversation, in order.
func withSystemMessages() func(openai.ChatCompletionNewParams) Endomorphism[openai.ChatCompletionNewParams] {
	messagesLens := oai.MakeChatCompletionNewParamsMessagesLens()

	return F.Flow4(
		messagesLens.Get,
		A.Filter(isSystemMessage()),
		F.Flow2(
			F.Flip(A.Concat[openai.ChatCompletionMessageParamUnion]),
			reader.Compose[[]openai.ChatCompletionMessageParamUnion](A.Filter(predicate.Not(isSystemMessage()))),
		),
		messagesLens.Modify,
	)
}

// withToolsOf sets the tool definitions of a request to those of another.
func withToolsOf() func(openai.ChatCompletionNewParams) Endomorphism[openai.ChatCompletionNewParams] {
	toolsLens := oai.MakeChatCompletionNewParamsToolsLens()

	return F.Flow2(toolsLens.Get, toolsLens.Set)
}

// SwitchMode is the change of a session to the request of a mode (D5, see
// mode.Request): the system messages of the current request are replaced by
// those of the mode's request, its tool definitions by the mode's, and the
// session records the ID of the mode, see [ModeID]. The conversation, every
// message but the system ones, is kept, and so are the model and the other
// parameters of the current request.
//
// It is the one change of mode: switch_mode (H5, see mode.SwitchMode) returns
// it as the change of its call, and the ACP session/set_mode (J6) is to apply
// it to the session of a client's request. The two changes of the request
// are independent.
func SwitchMode() func(ModeRequest) Endomorphism[Session] {
	modeLens := MakeSessionmodeLens()
	currentLens := MakeSessioncurrentLens()
	requestChanges := reader.ApplicativeMonoid[openai.ChatCompletionNewParams](endomorphism.Monoid[openai.ChatCompletionNewParams]())

	return F.Flow2(
		pair.BiMap(
			modeLens.Set,
			F.Flow2(requestChanges.Concat(withSystemMessages(), withToolsOf()), currentLens.Modify),
		),
		pair.Paired(endomorphism.Monoid[Session]().Concat),
	)
}

// ModeID is the ID of the mode a session switched to, None while it is in
// the mode it started in.
func ModeID() Reader[Session, Option[string]] {
	modeLens := MakeSessionmodeLens()

	return F.Flow2(
		modeLens.Get,
		option.FromPredicate(S.IsNonEmpty),
	)
}
