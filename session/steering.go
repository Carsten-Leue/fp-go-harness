package session

import (
	"context"

	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	A "github.com/IBM/fp-go/v2/array"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	"github.com/IBM/fp-go/v2/tailrec"
	openai "github.com/openai/openai-go/v3"
)

// SteeringDeps supplies the user messages that arrive while [Run] is still
// working on a turn: the user steers the agent without waiting for its answer.
type SteeringDeps interface {
	// GetPoll returns the thunk that takes the queued user messages. Each run
	// of it returns the messages queued since the previous run, oldest first,
	// and an empty slice when there are none. It must not block.
	GetPoll() Thunk[[]string]
}

type steeringDeps struct {
	poll Thunk[[]string]
}

func (d *steeringDeps) GetPoll() Thunk[[]string] {
	return d.poll
}

// MakeSteeringDeps is wiring: it combines the poll of the queued user messages
// into [SteeringDeps].
func MakeSteeringDeps(poll Thunk[[]string]) SteeringDeps {
	return &steeringDeps{poll}
}

// AsSteeringDeps is wiring: it narrows any wider dependencies to
// [SteeringDeps], for use with [reader.Local] and [effect.Local].
func AsSteeringDeps[R SteeringDeps](r R) SteeringDeps {
	return r
}

// NoSteering is wiring: the poll of a program that has no way to queue user
// messages during a turn. It always returns an empty slice.
func NoSteering() Thunk[[]string] {
	return thunk.FromLazy(A.Empty[string])
}

// drainState is one step of draining the queue: the messages taken so far are
// its state, the trampoline its outcome.
type drainState = Trampoline[[]string, []string]

// tryReceive is a leaf: the non-blocking receive of one message from the
// queue, None when nothing is waiting or the queue is closed, and the
// context's error once it is cancelled. A select with a default case has no
// fp-go lift. A closed queue is always ready to receive, so ok must be kept,
// or the drain would take empty messages forever.
func tryReceive(queue <-chan string) Thunk[Option[string]] {
	none := result.Of(option.None[string]())
	return func(ctx context.Context) IOResult[Option[string]] {
		return func() Result[Option[string]] {
			select {
			case <-ctx.Done():
				return result.Left[Option[string]](context.Cause(ctx))
			case message, ok := <-queue:
				return result.Of(option.Wrap(message, ok))
			default:
				return none
			}
		}
	}
}

// drainStep decides the next step from the messages taken so far and the
// outcome of one receive: a received message is appended and the drain goes
// on, nothing received lands with the messages taken so far. The messages are
// read by both branches, hence the reader applicative.
func drainStep() Reader[[]string, func(Option[string]) drainState] {
	return F.Pipe2(
		F.Flow2(tailrec.Land[[]string, []string], F.Constant[drainState]),
		reader.Map[[]string](F.Curry2(option.Fold[string, drainState])),
		reader.Ap[func(Option[string]) drainState](F.Flow2(
			F.Flip(A.Push[string]),
			reader.Map[string](tailrec.Bounce[[]string, []string]),
		)),
	)
}

// ChannelSteering is wiring: the poll of the user messages sent to queue by
// another goroutine, such as the reader of the terminal. Each run takes the
// messages that are waiting, oldest first, and returns at once; a closed queue
// has none. The drain is a stack-safe loop of non-blocking receives that
// checks the context before each one, so a cancelled context ends it with the
// context's error even while another goroutine keeps sending.
func ChannelSteering(queue <-chan string) Thunk[[]string] {
	return F.Pipe1(
		A.Empty[string](),
		thunk.TailRec(F.Flow3(
			drainStep(),
			thunk.Map[Option[string], drainState],
			reader.Read[Thunk[drainState]](tryReceive(queue)),
		)),
	)
}

// appendUserMessages appends one user message per text to the request of a
// session. Unlike [Continue] it keeps the iteration counter: the messages
// belong to the turn that is running.
func appendUserMessages() func([]string) Endomorphism[Session] {
	currentLens := MakeSessioncurrentLens()
	messagesLens := oai.MakeChatCompletionNewParamsMessagesLens()

	return F.Flow4(
		A.Map(openai.UserMessage[string]),
		A.Concat[openai.ChatCompletionMessageParamUnion],
		messagesLens.Modify,
		currentLens.Modify,
	)
}

// Steer takes the queued user messages of [SteeringDeps.GetPoll] and appends
// them to the request of the session, so the next request sends them.
// Without queued messages the session is returned unchanged.
func Steer() effect.Kleisli[SteeringDeps, Session, Session] {
	return F.Flow3(
		F.Flip(appendUserMessages()),
		effect.Map[SteeringDeps, []string, Session],
		reader.Read[Effect[SteeringDeps, Session]](effect.FromReaderThunk(SteeringDeps.GetPoll)),
	)
}
