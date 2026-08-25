package session

import (
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	I "github.com/IBM/fp-go/v2/identity"
	N "github.com/IBM/fp-go/v2/number"
	LP "github.com/IBM/fp-go/v2/optics/lens/prism"
	OL "github.com/IBM/fp-go/v2/optics/optional/lens"
	P "github.com/IBM/fp-go/v2/optics/prism"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	S "github.com/IBM/fp-go/v2/string"
	"github.com/IBM/fp-go/v2/tailrec"
	"github.com/openai/openai-go/v3"
)

const finishReasonToolCalls = "tool_calls"

type HistoryEntry = pair.Pair[openai.ChatCompletionNewParams, *openai.ChatCompletion]

type History = []HistoryEntry

// fp-go:Lens
type Session struct {
	history    History
	current    openai.ChatCompletionNewParams
	usage      openai.CompletionUsage
	iterations int
}

type SessionDeps interface {
	oai.ChatCompletionDeps
	GetToolCaller() ToolCaller
}

type FinalResult = Pair[Session, *openai.ChatCompletion]

type NextStep = Trampoline[Session, FinalResult]

func MakeSession(req openai.ChatCompletionNewParams) Session {
	return Session{current: req}
}

// TODO move to library
func headPrism[T any]() Prism[[]T, T] {
	return P.MakePrism(A.Head[T], A.Of)
}

func isToolCallFinishReason() Predicate[string] {
	return S.Equals(finishReasonToolCalls)
}

// Next executes a single step of the request/response loop that drives a
// [Session] against the chat completion model.
//
// It sends session.current to the model via [SessionDeps], records the
// returned usage and increments the session's iteration counter, then
// inspects the finish reason of the response's first choice:
//
//   - If the model asked to call tools (finish_reason == "tool_calls"),
//     Next resolves and executes them through [SessionDeps.GetToolCaller],
//     appends the resulting assistant/tool messages to session.current,
//     records the (request, response) pair in session.history, and
//     returns a Bounce carrying the updated [Session] so the loop can
//     continue.
//   - Otherwise, Next returns a Land carrying the final (session,
//     completion) pair: the conversation has reached a terminal response.
//
// The returned [NextStep] is a [Trampoline]; callers are expected to invoke
// Next repeatedly on the bounced Session until it lands, which keeps
// multi-round tool-call conversations stack-safe regardless of how many
// round trips they require.
func Next() effect.Kleisli[SessionDeps, Session, NextStep] {

	currentLens := MakeSessioncurrentLens()
	historyLens := MakeSessionhistoryLens()
	iterLens := MakeSessioniterationsLens()
	usageLens := MakeSessionusageLens()
	usageFromCompletionLens := oai.MakeChatCompletionUsageRefLens()
	choicesLens := oai.MakeChatCompletionChoicesRefLens()
	finishReasonLens := oai.MakeChatCompletionChoiceFinishReasonLens()

	firstFinishReason := F.Pipe2(
		choicesLens,
		LP.Compose[*openai.ChatCompletion](headPrism[openai.ChatCompletionChoice]()),
		OL.Compose[*openai.ChatCompletion](finishReasonLens),
	)

	handleToolCalls := F.Flow2(
		tools.HandleToolCalls(),
		effect.Local[Endomorphism[openai.ChatCompletionNewParams]](SessionDeps.GetToolCaller),
	)
	chatCompletion := F.Flow2(
		oai.ChatCompletion(),
		effect.Local[*openai.ChatCompletion](oai.AsChatCompletionDeps[SessionDeps]),
	)

	incIterations := F.Pipe1(
		N.Add(1),
		iterLens.Modify,
	)

	usageMonoid := MakeUsageMonoid()
	addUsage := F.Flow2(
		F.Curry2(usageMonoid.Concat),
		usageLens.Modify,
	)

	addHistoryEntry := F.Flow2(
		A.Push[HistoryEntry],
		historyLens.Modify,
	)

	addToHistory := I.Bind(
		pair.MapHead[*openai.ChatCompletion, Session, Session],
		F.Flow2(
			pair.MapHead[*openai.ChatCompletion](currentLens.Get),
			addHistoryEntry,
		),
	)

	toFinalResult := reader.Sequence(F.Flow2(
		pair.FromHead[*openai.ChatCompletion, Session],
		effect.Map[SessionDeps],
	))

	bounceToolCall := F.Pipe4(
		pair.Tail[Session, *openai.ChatCompletion],
		reader.Map[FinalResult](handleToolCalls),
		reader.ApS(
			F.Flow3(
				reader.Read[Session],
				reader.Local[Session](currentLens.Modify),
				effect.Map[SessionDeps],
			),
			pair.Head[Session, *openai.ChatCompletion],
		),
		reader.Map[FinalResult](F.Pipe1(
			tailrec.Bounce[FinalResult, Session],
			effect.Map[SessionDeps],
		)),
		reader.Local[Effect[SessionDeps, NextStep]](addToHistory),
	)

	landFinalResult := F.Flow2(
		tailrec.Land[Session, FinalResult],
		effect.Of[SessionDeps],
	)

	dispatch := F.Pipe1(
		F.Flow3(
			pair.Tail[Session, *openai.ChatCompletion],
			firstFinishReason.GetOption,
			option.Exists(isToolCallFinishReason()),
		),
		predicate.Fold(
			landFinalResult,
			bounceToolCall,
		),
	)

	return F.Pipe3(
		currentLens.Get,
		reader.Map[Session](chatCompletion),
		reader.Chain(toFinalResult),
		reader.Map[Session](F.Flow2(
			effect.Map[SessionDeps](F.Flow2(
				pair.MapHead[*openai.ChatCompletion](incIterations),
				I.Bind(
					pair.MapHead[*openai.ChatCompletion, Session],
					F.Flow3(
						pair.Tail[Session, *openai.ChatCompletion],
						usageFromCompletionLens.Get,
						addUsage,
					),
				),
			)),
			effect.Chain(dispatch),
		)),
	)
}
