package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/Carsten-Leue/fp-go-harness/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := cli.MakePromptCommand().Run(ctx, os.Args); err != nil {
		// %+v lists each error of a failed decoding with its value.
		fmt.Fprintf(os.Stderr, "%+v\n", err)
		os.Exit(1)
	}
}
