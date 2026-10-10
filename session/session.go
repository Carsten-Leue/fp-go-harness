package session

import (
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	N "github.com/IBM/fp-go/v2/number"
	L "github.com/IBM/fp-go/v2/optics/lens"
	OL "github.com/IBM/fp-go/v2/optics/optional/lens"
	P "github.com/IBM/fp-go/v2/optics/prism"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	S "github.com/IBM/fp-go/v2/string"
	"github.com/IBM/fp-go/v2/tailrec"
	openai "github.com/openai/openai-go/v3"
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
	tools.ToolDeps
	LoopDeps
}

type sessionDeps struct {
	oai.ChatCompletionDeps
	tools.ToolDeps
	LoopDeps
}

func MakeSessionDeps(c oai.ChatCompletionDeps, t tools.ToolDeps, l LoopDeps) SessionDeps {
	return &sessionDeps{c, t, l}
}

type FinalResult = Pair[Session, *openai.ChatCompletion]

type NextStep = Trampoline[Session, FinalResult]

func MakeSession(req openai.ChatCompletionNewParams) Session {
	return Session{current: req}
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
		L.ComposePrism[*openai.ChatCompletion](P.Head[openai.ChatCompletionChoice]()),
		OL.Compose[*openai.ChatCompletion](finishReasonLens),
	)

	handleToolCalls := F.Flow2(
		tools.HandleToolCalls(),
		effect.Local[Endomorphism[openai.ChatCompletionNewParams]](tools.AsToolDeps[SessionDeps]),
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

	// the session after one more request, with the usage of its completion
	countCompletion := F.Flow3(
		usageFromCompletionLens.Get,
		addUsage,
		reader.Map[Session](incIterations),
	)

	addHistoryEntry := F.Flow2(
		A.Push[HistoryEntry],
		historyLens.Modify,
	)

	// The session is updated from the completion next to it: the pair is read
	// once for the update and once more to apply it, hence reader.Chain.
	updateSession := reader.Chain(pair.MapHead[*openai.ChatCompletion, Session, Session])

	recordCompletion := F.Pipe1(
		F.Flow2(pair.Tail[Session, *openai.ChatCompletion], countCompletion),
		updateSession,
	)

	addToHistory := F.Pipe1(
		F.Flow2(pair.MapHead[*openai.ChatCompletion](currentLens.Get), addHistoryEntry),
		updateSession,
	)

	// the session with the messages of the tool calls appended to its request;
	// session and tool calls are two independent reads of the same pair
	applyToolCalls := F.Pipe1(
		F.Flow3(
			pair.Head[Session, *openai.ChatCompletion],
			F.Flip(currentLens.Modify),
			effect.Map[SessionDeps, Endomorphism[openai.ChatCompletionNewParams], Session],
		),
		reader.Ap[Effect[SessionDeps, Session]](F.Flow2(pair.Tail[Session, *openai.ChatCompletion], handleToolCalls)),
	)

	bounceToolCall := F.Flow3(
		addToHistory,
		applyToolCalls,
		effect.Map[SessionDeps](tailrec.Bounce[FinalResult, Session]),
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

	// the request and the session it is paired with are independent reads of
	// the session
	return F.Pipe2(
		F.Flow2(pair.FromHead[*openai.ChatCompletion, Session], effect.Map[SessionDeps, *openai.ChatCompletion, FinalResult]),
		reader.Ap[Effect[SessionDeps, FinalResult]](F.Flow2(currentLens.Get, chatCompletion)),
		reader.Map[Session](F.Flow2(
			effect.Map[SessionDeps](recordCompletion),
			effect.Chain(dispatch),
		)),
	)
}
