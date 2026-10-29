package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	N "github.com/IBM/fp-go/v2/number"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	RRIOE "github.com/IBM/fp-go/v2/readerreaderioeither"
	"github.com/IBM/fp-go/v2/result"
	"github.com/IBM/fp-go/v2/tailrec"
	openai "github.com/openai/openai-go/v3"
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

// Outcome is how a turn ended: the final completion of the model, or the
// error that ended the turn: a cancellation (see [IsCancelled]), a failed
// request or poll, or a [MaxIterationsError].
type Outcome = Result[*openai.ChatCompletion]

// TurnResult is what [Run] returns: the session as far as the turn got,
// paired with its [Outcome]. The session holds every round trip whose request
// returned, the usage so far and the steered messages already taken from the
// queue, also when the turn failed.
//
// Its consumers:
//
//   - E5, the REPL, continues the head with [Continue], also after a
//     cancelled turn.
//   - J1, the turn as a value, returns it.
//   - J3, ACP session/prompt, maps a Right by its finish reason, a cancelled
//     Left to "cancelled", a [MaxIterationsError] to "max_turn_requests" and
//     any other Left to a JSON-RPC error.
//   - J5, ACP cancellation, keeps the head in the ACP session table.
//   - K3 and K5, the A2A tasks, map a cancelled Left to "canceled" and any
//     other Left to "failed".
//   - G6, the MCP server, ends a cancelled request with the partial answer.
type TurnResult = Pair[Session, Outcome]

// TurnStep is one step of the loop of [Run]: the session to continue with,
// or the result of the turn.
type TurnStep = Trampoline[Session, TurnResult]

// IsCancelled matches the errors of a cancelled context, also the cause that
// [os/signal.NotifyContext] sets.
func IsCancelled() Predicate[error] {
	return F.Bind2nd(errors.Is, context.Canceled)
}

// failedTurn lands the error of a stage as the outcome of the turn, with the
// session the stage received.
func failedTurn() Reader[Session, effect.Kleisli[SessionDeps, error, TurnStep]] {
	return F.Flow3(
		F.Curry2(pair.MakePair[Session, Outcome]),
		reader.Local[TurnResult](result.Left[*openai.ChatCompletion]),
		reader.Map[error](F.Flow2(tailrec.Land[Session, TurnResult], effect.Of[SessionDeps, TurnStep])),
	)
}

// attempt makes a stage of the loop total: a failure of the stage lands with
// the session it received. The session is read twice, by the stage and by the
// recovery, hence the reader applicative. [effect.ChainLeft] does not check
// the context, so a cancelled stage lands too.
func attempt() Endomorphism[effect.Kleisli[SessionDeps, Session, TurnStep]] {
	recovery := F.Flow2(failedTurn(), effect.ChainLeft[SessionDeps, TurnStep])

	return F.Flow2(
		reader.Ap[Effect[SessionDeps, TurnStep], Session, Effect[SessionDeps, TurnStep]],
		reader.Read[Reader[Session, Effect[SessionDeps, TurnStep]]](recovery),
	)
}

// Run drives [Next] until the model returns a terminal response and returns
// the [TurnResult]: the session paired with the final completion.
//
// Run never fails. Each step runs the iteration guard, [Steer] and [Next],
// and a failure of any of them ends the turn with the error as its [Outcome]
// and the session that stage received: the guard's, so a [MaxIterationsError]
// comes with the session that reached the limit; the session after [Steer]
// for a failed request, so the steered messages it took are in its request.
//
// Before each request the session's iteration counter is checked against
// [LoopDeps.GetMaxIterations]; once the limit is reached, the turn ends with
// a [MaxIterationsError] instead of sending another request. After the
// check, [Steer] appends the user messages queued since the previous step,
// so the request carries them. Messages queued after the last request stay
// queued for the next Run, so after a [Continue] they come after its new
// prompt.
//
// The stages and the loop are joined without a check of the context
// (readerreaderioeither's Chain and TailRec, same type as [Effect]): a
// cancelled context makes the stage that runs fail, through the poll or the
// request, and that stage lands the cancellation with its session. The loop
// is stack-safe.
func Run() effect.Kleisli[SessionDeps, Session, TurnResult] {
	return runWith()(steerStage())
}

// RunPrompt is [Run] for a turn that a prompt started, as [Continue] does: the
// first request of the turn is sent without a poll, so it carries the prompt
// only. User messages queued before it stay queued; the poll before the
// second request takes them, and a turn that lands at once leaves them for
// the next prompt. The first request is the one at iteration zero, which
// [Continue] and [MakeSession] set. Everything else is as in [Run].
func RunPrompt() effect.Kleisli[SessionDeps, Session, TurnResult] {
	iterLens := MakeSessioniterationsLens()

	return F.Pipe2(
		F.Flow2(iterLens.Get, predicate.IsZero[int]()),
		predicate.Fold(steerStage(), effect.Of[SessionDeps, Session]),
		runWith(),
	)
}

// steerStage is the [Steer] stage of the loop, under [SessionDeps].
func steerStage() effect.Kleisli[SessionDeps, Session, Session] {
	return F.Flow2(
		Steer(),
		effect.Local[Session](AsSteeringDeps[SessionDeps]),
	)
}

// runWith is the loop of [Run] with a given steer stage: each step runs the
// iteration guard, the steer stage and [Next], each in an [attempt].
func runWith() Reader[effect.Kleisli[SessionDeps, Session, Session], effect.Kleisli[SessionDeps, Session, TurnResult]] {
	guard := F.Flow2(
		reader.Sequence(F.Flow2(
			SessionDeps.GetMaxIterations,
			limitIterations(),
		)),
		effect.FromReaderResult[SessionDeps, Session],
	)

	try := attempt()
	andThen := RRIOE.Chain[SessionDeps, context.Context, error, Session, TurnStep]

	// the inner stages never fail, so each failure lands in the attempt
	// around the stage that failed: guard >=> try(steer >=> try(next))
	return F.Flow5(
		reader.Map[Session](andThen(try(Next()))),
		try,
		andThen,
		reader.Local[Effect[SessionDeps, TurnStep]](guard),
		F.Flow2(try, RRIOE.TailRec[SessionDeps, context.Context, error, Session, TurnResult]),
	)
}
