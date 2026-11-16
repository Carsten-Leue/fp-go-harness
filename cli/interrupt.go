package cli

import (
	"context"
	"os"
	"os/signal"

	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/endomorphism"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
)

// Interrupts derives from a context a child that the next interrupt (Ctrl-C)
// cancels, paired with the function that releases it. Like the timer of
// [context.WithTimeout], the child registers for the interrupt until it is
// released; [thunk.Local] derives it when the computation runs.
type Interrupts = pair.Kleisli[context.CancelFunc, context.Context, context.Context]

// InterruptDeps is the source of the interrupts of a run, see [Interrupts].
type InterruptDeps interface {
	// GetInterrupts returns the source of the interrupts.
	GetInterrupts() Interrupts
}

type interruptDeps struct {
	interrupts Interrupts
}

func (d interruptDeps) GetInterrupts() Interrupts {
	return d.interrupts
}

// MakeInterruptDeps is wiring: the [InterruptDeps] of a source of interrupts.
func MakeInterruptDeps(interrupts Interrupts) InterruptDeps {
	return interruptDeps{interrupts}
}

// SignalInterrupts is a leaf, a raw I/O source: the interrupts of the
// process, [os.Interrupt], through [signal.NotifyContext], which has no fp-go
// lift. Once a child is cancelled it stops its notification, which restores
// the default handling: a second Ctrl-C, while the interrupted work stops,
// exits the process at once. Outside of a child Ctrl-C has the default
// handling too.
func SignalInterrupts() Interrupts {
	return func(parent context.Context) thunk.ContextCancel {
		ctx, stop := signal.NotifyContext(parent, os.Interrupt)
		context.AfterFunc(ctx, stop)
		return pair.MakePair(stop, ctx)
	}
}

// interruptibleThunk runs a computation under a child of its context that
// the next interrupt cancels, see [Interrupts]; the child is released when
// the computation ends. [thunk.Local] does not check the context, so a
// computation under a cancelled context still runs and reports it.
func interruptibleThunk[T any]() Reader[InterruptDeps, endomorphism.Endomorphism[ReaderIOResult[T]]] {
	return F.Flow2(InterruptDeps.GetInterrupts, thunk.Local[T, context.Context])
}

// interruptible runs an effect under any deps D like [interruptibleThunk].
// The context is below the deps, so it maps over them, as effect.WithTimeout
// does: effect has no Local of the context.
func interruptible[D, T any]() Reader[InterruptDeps, endomorphism.Endomorphism[Effect[D, T]]] {
	return F.Flow2(interruptibleThunk[T](), reader.Map[D, ReaderIOResult[T], ReaderIOResult[T]])
}
