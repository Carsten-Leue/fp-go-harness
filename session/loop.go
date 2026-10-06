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

func MakeLoopDeps(maxIterations int) LoopDeps {
	return &loopDeps{maxIterations}
}

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

func makeMaxIterationsError(maxIterations int) error {
	return &MaxIterationsError{maxIterations}
}

// limitIterations passes a session through unchanged while it has used fewer
// than maxIterations iterations, and fails with a [MaxIterationsError]
// otherwise.
func limitIterations(maxIterations int) result.Kleisli[Session, Session] {
	iterLens := MakeSessioniterationsLens()

	return result.FromPredicate(
		F.Flow2(iterLens.Get, N.LessThan(maxIterations)),
		F.Constant1[Session](makeMaxIterationsError(maxIterations)),
	)
}

// Run drives [Next] until the model returns a terminal response and returns the
// final (session, completion) pair.
//
// The loop is stack-safe ([effect.TailRec]) and checks the context before each
// step. Before each request the session's iteration counter is checked against
// [LoopDeps.GetMaxIterations]; once the limit is reached, Run fails with a
// [MaxIterationsError] instead of sending another request.
func Run() effect.Kleisli[SessionDeps, Session, FinalResult] {
	guard := F.Flow2(
		reader.Sequence(F.Flow2(
			SessionDeps.GetMaxIterations,
			limitIterations,
		)),
		effect.FromReaderResult[SessionDeps, Session],
	)

	return F.Pipe1(
		F.Flow2(
			guard,
			effect.Chain(Next()),
		),
		effect.TailRec,
	)
}
