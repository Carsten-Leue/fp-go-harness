package session

import (
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	I "github.com/IBM/fp-go/v2/identity"
	N "github.com/IBM/fp-go/v2/number"
	L "github.com/IBM/fp-go/v2/optics/lens"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/tailrec"
	"github.com/openai/openai-go/v3"
)

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

func Next() effect.Kleisli[SessionDeps, Session, NextStep] {

	currentLens := MakeSessioncurrentLens()
	iterLens := MakeSessioniterationsLens()
	usageLens := MakeSessionusageLens()
	usageFromCompletionLens := oai.MakeChatCompletionUsageRefLens()

	// handleToolCalls := F.Flow2(
	// 	tools.HandleToolCalls(),
	// 	effect.Local[Endomorphism[openai.ChatCompletionNewParams]](SessionDeps.GetToolCaller),
	// )
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

	dispatchToolCall := func(r FinalResult) Effect[SessionDeps, NextStep] {
		// TODO dispatch dependening on state
		return effect.Of[SessionDeps](tailrec.Land[Session](r))
	}

	return F.Pipe3(
		currentLens.Get,
		reader.Map[Session](chatCompletion),
		reader.Chain(F.Flow2(
			reader.Of[Session, Effect[SessionDeps, *openai.ChatCompletion]],
			reader.ApS(F.Flow2(
				pair.FromHead[*openai.ChatCompletion, Session],
				effect.Map[SessionDeps],
			), reader.Ask[Session]()),
		)),
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
			effect.Chain(dispatchToolCall),
		)),
	)
}
