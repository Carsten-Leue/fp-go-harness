package cli

import (
	"github.com/Carsten-Leue/fp-go-harness/mode"
	C "github.com/urfave/cli/v3"
)

// modeEnv is the environment of the wiring of one mode: the configuration of
// the run, its mode and the deps of the modes. The readers of [ask], the
// REPL and their parts read it.
//
// fp-go:Lens
type modeEnv struct {
	// Config is the configuration of the run.
	Config Config
	// Mode is the mode of the configuration, looked up in Deps.
	Mode mode.Mode
	// Deps are the deps of the modes, those of the tools among them.
	Deps mode.ModeDeps
}

// newModeEnv is a leaf: the constructor of [modeEnv] that [F.Curry3] lifts,
// the mode last because it is looked up in the deps.
func newModeEnv(cfg Config, deps mode.ModeDeps, m mode.Mode) modeEnv {
	return modeEnv{cfg, m, deps}
}

// lineQueue is the channel of the lines of the REPL. The lens generator types
// a field of an unnamed channel type as any, so the field is typed by this
// alias, which is the same type as <-chan string.
type lineQueue = <-chan string

// replEnv is the environment of the REPL: the [modeEnv] of its mode, the
// command it prints to and the channel of its lines, see [scanLines].
//
// fp-go:Lens
type replEnv struct {
	// Env is the environment of the mode.
	Env modeEnv
	// Command is the command the REPL prints to.
	Command *C.Command
	// Lines are the lines of the input, also the steering queue.
	Lines lineQueue
}

// newReplEnv is a leaf: the constructor of [replEnv] that [F.Curry3] lifts.
func newReplEnv(cmd *C.Command, lines lineQueue, env modeEnv) replEnv {
	return replEnv{env, cmd, lines}
}
