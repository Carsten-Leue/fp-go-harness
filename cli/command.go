package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/Carsten-Leue/fp-go-harness/mode"
	"github.com/IBM/fp-go/v2/result"
	C "github.com/urfave/cli/v3"
)

// configFlags are the flags of the [Config], one per variable. They carry no
// default: a flag that isn't set leaves the variable of a lower source, or
// the default of [GetConfigCodec], intact.
func configFlags() []C.Flag {
	name := flagName()

	return []C.Flag{
		&C.StringFlag{
			Name:  name(KeyMode),
			Usage: fmt.Sprintf("the mode, one of %s (%s uses all tools and changes the workspace) or a custom mode of ~/%s or <workspace>/%s, defaults to %q. Environment variable [%s].", strings.Join(mode.IDs()(mode.MakeModeTableDeps(mode.Modes())), ", "), mode.AgentID, mode.ModesDir, mode.ModesDir, mode.AskID, KeyMode),
		},
		&C.StringFlag{
			Name:      name(KeyWorkspace),
			Usage:     fmt.Sprintf("the directory the tools work in, defaults to the working directory. Environment variable [%s].", KeyWorkspace),
			TakesFile: true,
		},
		&C.IntFlag{
			Name:        name(KeyMaxIterations),
			HideDefault: true,
			Usage:       fmt.Sprintf("the maximum number of chat completion requests per prompt, defaults to %d. Environment variable [%s].", defaultMaxIterations, KeyMaxIterations),
		},
		&C.IntFlag{
			Name:        name(KeyMaxToolOutput),
			HideDefault: true,
			Usage:       fmt.Sprintf("the maximum number of bytes of a tool result sent to the model, defaults to %d. Environment variable [%s].", defaultMaxToolOutput, KeyMaxToolOutput),
		},
	}
}

// MakePromptCommand creates the command that answers a prompt with DeepSeek
// and the tools of a mode, and prints the answer and the session statistics.
//
// Without a prompt, or with --interactive, it runs a REPL instead: each line
// of the standard input is the next turn of one session, and lines typed
// while a turn runs steer it. The prompt given with --interactive is the
// first line. The REPL ends with the input, with exit code 0, or with a
// cancelled turn.
//
// A turn that does not answer, because it was cancelled, a request failed or
// it reached the iteration limit, prints the error, the last assistant text
// of the session and the statistics on the standard error. After a cancelled
// turn the background processes of execute_command are stopped. The command
// prints every error it returns once; usage errors are printed by
// [C.Command] itself.
//
// Example:
//
//	fp-go-harness --mode agent --max-iterations 10 "add a test for grep"
func MakePromptCommand() *C.Command {
	return &C.Command{
		Name:      "fp-go-harness",
		Usage:     "answers a prompt with DeepSeek and the tools of a mode",
		ArgsUsage: "[<prompt>]",
		Flags: append(configFlags(), &C.BoolFlag{
			Name:  interactiveFlag,
			Usage: "run a REPL over the standard input, the prompt is its first line. Without a prompt the REPL runs anyway.",
		}),
		// Errors are returned to main for the exit code. The default
		// handler exits the process on an error that carries several
		// errors, as a failed decoding does.
		ExitErrHandler: func(context.Context, *C.Command, error) {},
		Action: func(ctx context.Context, cmd *C.Command) error {
			_, err := result.Unwrap(runCommand()(cmd)(ctx)())
			return err
		},
	}
}
