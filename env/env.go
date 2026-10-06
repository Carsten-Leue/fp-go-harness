package env

import (
	"errors"
	"io/fs"
	"os"

	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	ER "github.com/IBM/fp-go/v2/errors"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/iooption"
	"github.com/IBM/fp-go/v2/ioresult"
	M "github.com/IBM/fp-go/v2/monoid"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/joho/godotenv"
)

// DotEnvFile is the name of the file with local settings and secrets. It is
// not committed; .env.example lists the variables it may set.
const DotEnvFile = ".env"

type EnvironmentDeps interface {
	GetLookupEnv() ReaderIOResult[string, string]
}

type environmentDeps struct {
	lookup ReaderIOResult[string, string]
}

func AsEnvironmentDeps[R EnvironmentDeps](r R) EnvironmentDeps {
	return r
}

func (d *environmentDeps) GetLookupEnv() ReaderIOResult[string, string] {
	return d.lookup
}

func newEnvironmentDeps(lookup ReaderIOResult[string, string]) EnvironmentDeps {
	return &environmentDeps{lookup}
}

// notSetError is a leaf: [ER.OnNone] is variadic and needs the key as argument.
func notSetError(key string) func() error {
	return ER.OnNone("environment variable %q is not set", key)
}

// makeLookup looks a key up in the process environment first and in vars
// second, the precedence of [godotenv.Load], which never overrides a variable
// that is already set.
func makeLookup(vars Vars) ReaderIOResult[string, string] {
	lookupVars := F.Flow2(
		F.Bind1st(R.MonadLookup[string, string], vars),
		iooption.FromOption[string],
	)

	lookupOpt := F.Pipe2(
		lookupVars,
		reader.Map[string](iooption.Alt[string]),
		reader.Ap[IOOption[string]](iooption.Optionize1(os.LookupEnv)),
	)

	return F.Pipe2(
		notSetError,
		reader.Map[string](ioresult.FromIOOption[string]),
		reader.Ap[IOResult[string]](lookupOpt),
	)
}

// MakeEnvironmentDeps reads variables from the process environment only.
func MakeEnvironmentDeps() EnvironmentDeps {
	return F.Pipe2(
		R.Empty[string, string](),
		makeLookup,
		newEnvironmentDeps,
	)
}

// readDotEnvFile is a leaf: [godotenv.Read] is variadic. It parses the file
// and, unlike [godotenv.Load], leaves the process environment untouched.
func readDotEnvFile(path string) (Vars, error) {
	return godotenv.Read(path)
}

func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}

// ReadDotEnv parses the dotenv file at path. A missing file has no variables,
// any other read or parse error fails.
func ReadDotEnv() ioresult.Kleisli[string, Vars] {
	return F.Flow2(
		ioresult.Eitherize1(readDotEnvFile),
		ioresult.ChainLeft(P.Fold(
			ioresult.Left[Vars],
			F.Flow2(
				F.Constant1[error](R.Empty[string, string]()),
				ioresult.Of[Vars],
			),
		)(isNotExist)),
	)
}

// ReadDotEnvs parses the dotenv files at paths and merges them. A variable
// keeps the value of the first file that sets it, as with [godotenv.Load].
func ReadDotEnvs() ioresult.Kleisli[[]string, Vars] {
	return F.Flow2(
		ioresult.TraverseArray(ReadDotEnv()),
		ioresult.Map(M.ConcatAll(R.UnionFirstMonoid[string, string]())),
	)
}

// MakeDotEnvEnvironmentDeps builds [EnvironmentDeps] over the process
// environment and the dotenv files at paths, which fill in the variables the
// process environment doesn't set. Missing files are skipped.
//
// Example:
//
//	deps := MakeDotEnvEnvironmentDeps()(A.Of(DotEnvFile))
func MakeDotEnvEnvironmentDeps() ioresult.Kleisli[[]string, EnvironmentDeps] {
	return F.Flow2(
		ReadDotEnvs(),
		ioresult.Map(F.Flow2(makeLookup, newEnvironmentDeps)),
	)
}

func LookupEnvThunk(key string) Effect[EnvironmentDeps, string] {
	return F.Pipe1(
		effect.Asks(EnvironmentDeps.GetLookupEnv),
		effect.ChainThunkK[EnvironmentDeps](F.Flow2(
			reader.Read[IOResult[string]](key),
			thunk.FromIOResult,
		)),
	)
}
