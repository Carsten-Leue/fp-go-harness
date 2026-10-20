package cli

import (
	"github.com/Carsten-Leue/fp-go-harness/mode"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/optics/codec/validation"
	"github.com/IBM/fp-go/v2/result"
)

// The variables of the configuration. Each one is also a flag, named by
// [flagName]: MAX_ITERATIONS is --max-iterations.
const (
	// KeyMode selects the mode by its ID, one of [mode.IDs].
	KeyMode = "MODE"
	// KeyWorkspace is the directory the tools work in.
	KeyWorkspace = "WORKSPACE"
	// KeyMaxIterations bounds the number of chat completion requests per
	// prompt.
	KeyMaxIterations = "MAX_ITERATIONS"
	// KeyMaxToolOutput bounds the bytes of a single tool result sent to the
	// model.
	KeyMaxToolOutput = "MAX_TOOL_OUTPUT"
)

const (
	// defaultWorkspace is the working directory.
	defaultWorkspace = "."
	// defaultMaxIterations is the default of [KeyMaxIterations].
	defaultMaxIterations = 25
	// defaultMaxToolOutput is the default of [KeyMaxToolOutput]. The recorded
	// sessions contain results of up to 37 KB.
	defaultMaxToolOutput = 50_000
)

// Config is the configuration of a run of the CLI.
//
// fp-go:Lens
type Config struct {
	// Mode is the ID of the mode, one of [mode.IDs].
	Mode string
	// Workspace is the absolute path of the directory the tools work in.
	Workspace string
	// MaxIterations bounds the number of chat completion requests per prompt.
	MaxIterations int
	// MaxToolOutput bounds the bytes of a single tool result sent to the
	// model.
	MaxToolOutput int
}

// configKeys are the variables of [Config], in the order of its fields.
func configKeys() []string {
	return []string{KeyMode, KeyWorkspace, KeyMaxIterations, KeyMaxToolOutput}
}

// GetConfigCodec returns the codec of [Config]: it decodes the configuration
// from the variables and encodes it back to them.
//
//   - MODE            (mode ID, default "ask")
//   - WORKSPACE       (path, default the working directory), made absolute
//   - MAX_ITERATIONS  (int > 0, default 25)
//   - MAX_TOOL_OUTPUT (int > 0, default 50000)
func GetConfigCodec() EnvCodec[Config] {
	return F.Pipe3(
		bindTo(MakeConfigModePrism(), stringWithDefault(KeyMode, mode.AskID)),
		apSL(MakeConfigWorkspaceLens(), stringWithDefault(KeyWorkspace, defaultWorkspace).
			Pipe(absolutePath())),
		apSL(MakeConfigMaxIterationsLens(), intWithDefault(KeyMaxIterations, defaultMaxIterations).
			PipePrism(positive())),
		apSL(MakeConfigMaxToolOutputLens(), intWithDefault(KeyMaxToolOutput, defaultMaxToolOutput).
			PipePrism(positive())),
	)
}

// decodeConfig decodes the [Config] from a variable map with
// [GetConfigCodec].
func decodeConfig() result.Kleisli[Record[string, string], Config] {
	return F.Flow3(
		lookupFromMap(),
		GetConfigCodec().Decode,
		validation.ToResult[Config],
	)
}
