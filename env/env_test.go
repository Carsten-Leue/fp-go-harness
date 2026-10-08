package env

import (
	"os"
	"path/filepath"
	"testing"

	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeDotEnv(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func lookup(deps EnvironmentDeps, key string) Result[string] {
	return deps.GetLookupEnv()(key)()
}

func TestReadDotEnv(t *testing.T) {
	path := writeDotEnv(t, ".env", "# comment\nA=1\nB='two words'\nexport C=\"x\\ny\"\n")

	assert.Equal(t, result.Of(Vars{"A": "1", "B": "two words", "C": "x\ny"}), ReadDotEnv()(path)())
}

func TestReadDotEnvMissing(t *testing.T) {
	assert.Equal(t, result.Of(Vars{}), ReadDotEnv()(filepath.Join(t.TempDir(), ".env"))())
}

func TestReadDotEnvDirectory(t *testing.T) {
	// a path that exists but can't be read as a file is an error, not "missing"
	assert.True(t, result.IsLeft(ReadDotEnv()(t.TempDir())()))
}

func TestReadDotEnvsFirstWins(t *testing.T) {
	first := writeDotEnv(t, "first.env", "A=first\n")
	second := writeDotEnv(t, "second.env", "A=second\nB=second\n")

	assert.Equal(t, result.Of(Vars{"A": "first", "B": "second"}), ReadDotEnvs()(A.From(first, second))())
}

func TestMakeDotEnvEnvironmentDeps(t *testing.T) {
	const (
		fromFile    = "FP_GO_HARNESS_TEST_FROM_FILE"
		fromProcess = "FP_GO_HARNESS_TEST_FROM_PROCESS"
		unset       = "FP_GO_HARNESS_TEST_UNSET"
	)
	t.Setenv(fromProcess, "process")

	path := writeDotEnv(t, ".env", fromFile+"=file\n"+fromProcess+"=file\n")

	deps, err := result.Unwrap(MakeDotEnvEnvironmentDeps()(A.Of(path))())
	require.NoError(t, err)

	assert.Equal(t, result.Of("file"), lookup(deps, fromFile))
	// the process environment wins, as with godotenv.Load
	assert.Equal(t, result.Of("process"), lookup(deps, fromProcess))
	assert.True(t, result.IsLeft(lookup(deps, unset)))

	// the file is not loaded into the process environment
	_, ok := os.LookupEnv(fromFile)
	assert.False(t, ok)
}

func TestMakeDotEnvEnvironmentDepsInvalid(t *testing.T) {
	path := writeDotEnv(t, ".env", "A='unterminated\n")

	assert.True(t, result.IsLeft(MakeDotEnvEnvironmentDeps()(A.Of(path))()))
}

func TestMakeEnvironmentDeps(t *testing.T) {
	const key = "FP_GO_HARNESS_TEST_PROCESS_ONLY"
	t.Setenv(key, "v")

	assert.Equal(t, result.Of("v"), lookup(MakeEnvironmentDeps(), key))
}

func TestAsEnvironmentDeps(t *testing.T) {
	deps := MakeEnvironmentDeps()

	assert.Equal(t, deps, AsEnvironmentDeps(deps))
}

func TestLookupEnvThunk(t *testing.T) {
	const (
		set   = "FP_GO_HARNESS_TEST_THUNK_SET"
		unset = "FP_GO_HARNESS_TEST_THUNK_UNSET"
	)
	t.Setenv(set, "v")
	deps := MakeEnvironmentDeps()

	assert.Equal(t, result.Of("v"), LookupEnvThunk(set)(deps)(t.Context())())

	_, err := result.Unwrap(LookupEnvThunk(unset)(deps)(t.Context())())
	assert.EqualError(t, err, `environment variable "`+unset+`" is not set`)
}
