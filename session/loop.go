package session

import (
	"fmt"

	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	N "github.com/IBM/fp-go/v2/number"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
)

// LoopDeps configures how the agent loop driven by [Run] is bounded.
type LoopDeps interface {
	// GetMaxIterations returns the maximum number of chat completion requests
	// a single [Run] may send before it gives up.
	GetMaxIterations() int
}

type loopDeps struct {
	maxIterations int
}

func (d *loopDeps) GetMaxIterations() int {
	return d.maxIterations
}

// MakeLoopDeps is wiring: it combines the iteration limit into [LoopDeps].
func MakeLoopDeps(maxIterations int) LoopDeps {
	return &loopDeps{maxIterations}
}

// AsLoopDeps is wiring: it narrows any wider dependencies to [LoopDeps], for
// use with [reader.Local] and [effect.Local].
func AsLoopDeps[R LoopDeps](r R) LoopDeps {
	return r
}

// MaxIterationsError reports that [Run] reached [LoopDeps.GetMaxIterations]
// before the model returned a terminal response.
type MaxIterationsError struct {
	MaxIterations int
}

func (e *MaxIterationsError) Error() string {
	return fmt.Sprintf("agent loop stopped after reaching the maximum of %d iterations", e.MaxIterations)
}

// makeMaxIterationsError is a leaf: the constructor of a [MaxIterationsError].
func makeMaxIterationsError(maxIterations int) error {
	return &MaxIterationsError{maxIterations}
}

// belowIterations matches the sessions that have used fewer iterations than
// the limit.
func belowIterations() reader.Reader[int, Predicate[Session]] {
	iterLens := MakeSessioniterationsLens()

	return F.Flow2(N.LessThan[int], reader.Local[bool](iterLens.Get))
}

// iterationsError fails every session with the [MaxIterationsError] of the
// limit.
func iterationsError() reader.Reader[int, func(Session) error] {
	return F.Flow2(makeMaxIterationsError, F.Constant1[Session, error])
}

// limitIterations passes a session through unchanged while it has used fewer
// iterations than the limit, and fails with a [MaxIterationsError]
// otherwise. The limit is read by the predicate and by the error, so both are
// combined with the reader applicative.
func limitIterations() reader.Reader[int, result.Kleisli[Session, Session]] {
	return F.Pipe1(
		F.Flow2(belowIterations(), F.Curry2(result.FromPredicate[Session])),
		reader.Ap[result.Kleisli[Session, Session]](iterationsError()),
	)
}

// Run drives [Next] until the model returns a terminal response and returns the
// final (session, completion) pair.
//
// The loop is stack-safe ([effect.TailRec]) and checks the context before each
// step. Before each request the session's iteration counter is checked against
// [LoopDeps.GetMaxIterations]; once the limit is reached, Run fails with a
// [MaxIterationsError] instead of sending another request. After the check,
// [Steer] appends the user messages queued since the previous step, so the
// request carries them. Messages queued after the last request stay queued
// for the next Run, so after a [Continue] they come after its new prompt.
func Run() effect.Kleisli[SessionDeps, Session, FinalResult] {
	guard := F.Flow2(
		reader.Sequence(F.Flow2(
			SessionDeps.GetMaxIterations,
			limitIterations(),
		)),
		effect.FromReaderResult[SessionDeps, Session],
	)

	steer := F.Flow2(
		Steer(),
		effect.Local[Session](AsSteeringDeps[SessionDeps]),
	)

	return F.Pipe1(
		F.Flow3(
			guard,
			effect.Chain(steer),
			effect.Chain(Next()),
		),
		effect.TailRec,
	)
}
