package env

import (
	"fmt"
	"os"

	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/reader"
)

type EnvironmentDeps interface {
	GetLookupEnv() ReaderIOResult[string, string]
}

type environmentDeps struct {
	lookup ReaderIOResult[string, string]
}

func (d *environmentDeps) GetLookupEnv() ReaderIOResult[string, string] {
	return d.lookup
}

func MakeEnvironmentDeps() EnvironmentDeps {
	return &environmentDeps{ioresult.Eitherize1(lookupEnv)}
}

func lookupEnv(key string) (string, error) {
	if v, ok := os.LookupEnv(key); ok {
		return v, nil
	}
	return "", fmt.Errorf("environment variable %q is not set", key)
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
