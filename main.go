package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/Carsten-Leue/fp-go-harness/cli"
	"github.com/Carsten-Leue/fp-go-harness/session"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/predicate"
)

const (
	exitOK = 0
	// exitFailure is the exit code of any error but a cancellation.
	exitFailure = 1
	// exitCancelled is the exit code of a run stopped by Ctrl-C: 128 + SIGINT.
	exitCancelled = 130
)

// exitCode is the exit code of a run, one fold over its error: none is
// success, a cancellation is [exitCancelled], any other error
// [exitFailure].
func exitCode() func(error) int {
	return F.Flow2(
		option.FromNonZero[error](),
		option.Fold(
			F.Constant(exitOK),
			predicate.Fold(F.Constant1[error](exitFailure), F.Constant1[error](exitCancelled))(session.IsCancelled()),
		),
	)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	// The first Ctrl-C cancels ctx and the turn stops. Stopping the
	// notification restores the default handling, so a second Ctrl-C while
	// the turn stops exits at once.
	context.AfterFunc(ctx, stop)

	// the command prints its errors, see [cli.MakePromptCommand]
	os.Exit(exitCode()(cli.MakePromptCommand().Run(ctx, os.Args)))
}
