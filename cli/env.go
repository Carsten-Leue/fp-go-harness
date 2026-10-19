package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/Carsten-Leue/fp-go-harness/env"
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/magma"
	M "github.com/IBM/fp-go/v2/monoid"
	O "github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	R "github.com/IBM/fp-go/v2/record"
	S "github.com/IBM/fp-go/v2/string"
	C "github.com/urfave/cli/v3"
)

// flagName converts a variable to the name of its flag: MAX_ITERATIONS is
// max-iterations.
func flagName() func(string) string {
	return F.Flow2(
		strings.ToLower,
		F.Bind23of3(strings.ReplaceAll)("_", "-"),
	)
}

// isSet reads whether the flag was set on the command line, so that a flag
// that wasn't set leaves the value of a lower source intact.
func isSet() Reader[string, func(*C.Command) bool] {
	return reader.Curry((*C.Command).IsSet)
}

// flagValue is a leaf: it renders the value of a flag of any type as the
// string a variable holds.
func flagValue(cmd *C.Command, name string) string {
	return fmt.Sprint(cmd.Value(name))
}

// flagFromCommand reads a flag from the command: its value when it was set,
// none otherwise.
func flagFromCommand() Reader[string, ReaderOption[*C.Command, string]] {
	return F.Pipe2(
		F.Flow2(isSet(), O.FromPredicate[*C.Command]),
		reader.Map[string](reader.Compose[Option[string], *C.Command, Option[*C.Command]]),
		reader.Ap[func(*C.Command) Option[string]](F.Flow2(
			reader.Curry(flagValue),
			O.Map[*C.Command, string],
		)),
	)
}

// varFromCommand pairs a variable with the reader of its flag.
func varFromCommand() func(string) pair.Pair[string, ReaderOption[*C.Command, string]] {
	return F.Flow2(
		pair.Of[string],
		pair.Map[string](F.Flow2(flagName(), flagFromCommand())),
	)
}

// envFromCommand reads the variables whose flags were set on the command.
func envFromCommand() func([]string) Reader[*C.Command, Record[string, string]] {
	return F.Flow3(
		R.FromArrayMap[string, string](magma.Second[ReaderOption[*C.Command, string]]())(varFromCommand()),
		reader.SequenceRecord[string, *C.Command, Option[string]],
		reader.Map[*C.Command](O.CompactRecord[string, string]),
	)
}

// entryOf is a leaf: it splits a KEY=VALUE entry of [os.Environ].
func entryOf(kv string) Option[pair.Pair[string, string]] {
	key, value, found := strings.Cut(kv, "=")
	return O.FromPredicate(F.Constant1[pair.Pair[string, string]](found))(pair.MakePair(key, value))
}

// processEnv reads the variables of the process environment.
func processEnv() IO[Record[string, string]] {
	return F.Pipe1(
		os.Environ,
		io.Map(F.Flow2(
			A.FilterMap(entryOf),
			R.FromEntries[string, string],
		)),
	)
}

// dotEnv reads the variables of the .env file in the working directory. A
// missing file has none.
func dotEnv() IOResult[Record[string, string]] {
	return F.Pipe1(
		A.Of(env.DotEnvFile),
		env.ReadDotEnvs(),
	)
}

// loadEnv reads the variables of a run of the command: the .env file, the
// process environment over it, and the flags of the command that are set
// over both. The sources are read concurrently, [mergeEnv] orders them. An
// empty variable counts as unset, so that `MODE=` in .env keeps the default.
func loadEnv() Reader[*C.Command, IOResult[Record[string, string]]] {
	envMonoid := ioresult.ApplicativeMonoid(mergeEnv())

	return F.Flow4(
		envFromCommand()(configKeys()),
		ioresult.Of[Record[string, string]],
		F.Bind1st(envMonoid.Concat, M.ConcatAll(envMonoid)(A.From(
			dotEnv(),
			ioresult.FromIO(processEnv()),
		))),
		ioresult.Map(R.FilterMap[string](O.FromPredicate(S.IsNonEmpty))),
	)
}
