package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/pair"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	C "github.com/urfave/cli/v3"
)

func TestDecodeConfig_Defaults(t *testing.T) {
	cfg, err := result.Unwrap(decodeConfig()(R.Empty[string, string]()))
	require.NoError(t, err)

	wd, err := os.Getwd()
	require.NoError(t, err)

	assert.Equal(t, Config{
		Mode:          askMode,
		Workspace:     wd,
		MaxIterations: 25,
		MaxToolOutput: 50_000,
	}, cfg)
}

func TestDecodeConfig_Values(t *testing.T) {
	root := t.TempDir()

	cfg, err := result.Unwrap(decodeConfig()(R.FromEntries(A.From(
		pair.MakePair(KeyMode, agentMode),
		pair.MakePair(KeyWorkspace, root),
		pair.MakePair(KeyMaxIterations, "3"),
		pair.MakePair(KeyMaxToolOutput, "100"),
		pair.MakePair("UNRELATED", "ignored"),
	))))
	require.NoError(t, err)

	assert.Equal(t, Config{
		Mode:          agentMode,
		Workspace:     root,
		MaxIterations: 3,
		MaxToolOutput: 100,
	}, cfg)
}

func TestDecodeConfig_RelativeWorkspace(t *testing.T) {
	cfg, err := result.Unwrap(decodeConfig()(R.Singleton(KeyWorkspace, "sub")))
	require.NoError(t, err)

	wd, err := os.Getwd()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(wd, "sub"), cfg.Workspace)
}

func TestDecodeConfig_Invalid(t *testing.T) {
	for name, vars := range R.FromEntries(A.From(
		pair.MakePair("not an integer", R.Singleton(KeyMaxIterations, "many")),
		pair.MakePair("zero iterations", R.Singleton(KeyMaxIterations, "0")),
		pair.MakePair("negative tool output", R.Singleton(KeyMaxToolOutput, "-1")),
	)) {
		t.Run(name, func(t *testing.T) {
			_, err := result.Unwrap(decodeConfig()(vars))
			assert.Error(t, err)
		})
	}
}

func TestConfigCodec_Encode(t *testing.T) {
	cfg := Config{Mode: agentMode, Workspace: "/work", MaxIterations: 3, MaxToolOutput: 100}

	assert.Equal(t, R.FromEntries(A.From(
		pair.MakePair(KeyMode, agentMode),
		pair.MakePair(KeyWorkspace, "/work"),
		pair.MakePair(KeyMaxIterations, "3"),
		pair.MakePair(KeyMaxToolOutput, "100"),
	)), GetConfigCodec().Encode(cfg))
}

func TestFlagName(t *testing.T) {
	assert.Equal(t, "max-tool-output", flagName()(KeyMaxToolOutput))
	assert.Equal(t, "mode", flagName()(KeyMode))
}

// readFlags runs a command with the flags of the configuration and returns
// the variables of the flags that are set.
func readFlags(t *testing.T, args ...string) Record[string, string] {
	t.Helper()

	var vars Record[string, string]
	cmd := &C.Command{
		Name:  "test",
		Flags: configFlags(),
		Action: func(_ context.Context, cmd *C.Command) error {
			vars = envFromCommand()(configKeys())(cmd)
			return nil
		},
	}
	require.NoError(t, cmd.Run(t.Context(), append([]string{"test"}, args...)))
	return vars
}

func TestEnvFromCommand_OnlySetFlags(t *testing.T) {
	assert.Equal(t, R.FromEntries(A.From(
		pair.MakePair(KeyMode, agentMode),
		pair.MakePair(KeyMaxIterations, "7"),
	)), readFlags(t, "--mode", agentMode, "--max-iterations", "7", "prompt"))
}

func TestEnvFromCommand_NoFlags(t *testing.T) {
	assert.Empty(t, readFlags(t, "prompt"))
}
