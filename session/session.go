package session

import (
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/endomorphism"
	F "github.com/IBM/fp-go/v2/function"
	I "github.com/IBM/fp-go/v2/identity"
	N "github.com/IBM/fp-go/v2/number"
	L "github.com/IBM/fp-go/v2/optics/lens"
	LP "github.com/IBM/fp-go/v2/optics/lens/prism"
	OL "github.com/IBM/fp-go/v2/optics/optional/lens"
	P "github.com/IBM/fp-go/v2/optics/prism"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/readeroption"
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

	incIterations := F.Pipe2(
		N.Add(1),
		L.Modify,
		reader.Read[Endomorphism[Session]](iterLens),
	)

	usageMonoid := MakeUsageMonoid()
	addUsage := F.Pipe1(
		F.Curry2(usageMonoid.Concat),
		reader.Map[openai.CompletionUsage](F.Flow2(
			L.Modify[Session, Endomorphism[openai.CompletionUsage]],
			reader.Read[Endomorphism[Session]](usageLens),
		)),
	)

	addHstoryEntry := F.Pipe1(
		A.Push[HistoryEntry],
		reader.Map[HistoryEntry](F.Flow2(
			L.Modify[Session, Endomorphism[History]],
			reader.Read[Endomorphism[Session]](historyLens),
		)),
	)

	getRequest := F.Flow2(
		pair.Head[Session, *openai.ChatCompletion],
		currentLens.Get,
	)

	addToHistory := F.Pipe2(
		pair.Tail[Session, *openai.ChatCompletion],
		reader.ApS(
			pair.FromHead[*openai.ChatCompletion, openai.ChatCompletionNewParams],
			getRequest,
		),
		reader.ApS(
			F.Pipe2(
				addHstoryEntry,
				reader.Map[HistoryEntry](pair.MapHead[*openai.ChatCompletion, Session, Session]),
				reader.Sequence,
			),
			reader.Ask[FinalResult](),
		),
	)

	toFinalResult := F.Flow2(
		reader.Of[Session, Effect[SessionDeps, *openai.ChatCompletion]],
		reader.ApS(
			F.Flow2(
				pair.FromHead[*openai.ChatCompletion, Session],
				effect.Map[SessionDeps],
			),
			reader.Ask[Session](),
		),
	)

	bounceToolCall := F.Pipe5(
		pair.Tail[Session, *openai.ChatCompletion],
		reader.Map[FinalResult](handleToolCalls),
		reader.ApS(
			F.Flow2(
				endomorphism.Read[openai.ChatCompletionNewParams],
				effect.Map[SessionDeps],
			),
			getRequest,
		),
		reader.Chain(F.Flow2(
			reader.Of[FinalResult],
			reader.ApS(
				F.Flow2(
					reader.Sequence(currentLens.Set),
					effect.Map[SessionDeps],
				),
				pair.Head[Session, *openai.ChatCompletion],
			),
		)),
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

	dispatch := F.Pipe4(
		pair.Tail[Session, *openai.ChatCompletion],
		reader.Map[FinalResult](firstFinishReason.GetOption),
		readeroption.ChainOptionK[FinalResult](option.FromPredicate(isToolCallFinishReason())),
		readeroption.ChainReaderK(reader.Of[string](bounceToolCall)),
		readeroption.GetOrElse(landFinalResult),
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
