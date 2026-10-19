package cli

import (
	"fmt"
	"path/filepath"
	"strconv"

	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/lazy"
	N "github.com/IBM/fp-go/v2/number"
	"github.com/IBM/fp-go/v2/optics/codec"
	"github.com/IBM/fp-go/v2/optics/codec/validate"
	"github.com/IBM/fp-go/v2/optics/codec/validation"
	P "github.com/IBM/fp-go/v2/optics/prism"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/readeroption"
	"github.com/IBM/fp-go/v2/readerresult"
	R "github.com/IBM/fp-go/v2/record"
)

// mergeEnv merges two variable maps; the second one wins on a conflict. It
// merges the encodings of the fields of a configuration, and the sources of
// the variables in [loadEnv].
func mergeEnv() R.Monoid[Record[string, string]] {
	return R.UnionLastMonoid[string, string]()
}

// lookupFromMap looks keys up in a variable map, the [EnvDep] a codec decodes
// from.
func lookupFromMap() Reader[Record[string, string], EnvDep] {
	return F.Flip(R.Lookup[string, string])
}

// stringWithDefault is wiring: the codec of the variable key, which decodes
// to def when key is not set.
func stringWithDefault(key, def string) EnvCodec[string] {
	return codec.MakeType(
		fmt.Sprintf("stringWithDefault: %s=%s", key, def),
		codec.Is[string](),
		F.Flow4(
			readeroption.Read[string](key),
			option.GetOrElse(lazy.Of(def)),
			validation.Success[string],
			reader.Of[validate.Context, validation.Validation[string]],
		),
		F.Bind1st(R.Of[string, string], key),
	)
}

// intWithDefault is wiring: the codec of the integer variable key, which
// decodes to def when key is not set and fails when its value is not an
// integer.
func intWithDefault(key string, def int) EnvCodec[int] {
	return stringWithDefault(key, strconv.Itoa(def)).
		Pipe(codec.IntFromString())
}

// positive refines an integer to one greater than zero.
func positive() P.Prism[int, int] {
	return P.MakePrismWithName(
		option.FromPredicate(N.MoreThan(0)),
		F.Identity[int],
		"positive",
	)
}

// absolutePath decodes a path to its absolute form, relative to the working
// directory, and encodes it unchanged.
func absolutePath() codec.Type[string, string, string] {
	return codec.MakeType(
		"absolutePath",
		codec.Is[string](),
		validate.FromReaderResult(readerresult.FromIdiomatic(filepath.Abs)),
		reader.Ask[string](),
	)
}

// bindTo is wiring: it starts the codec of a struct S with the codec of the
// field that p focuses on.
func bindTo[S, T any](p Prism[S, T], t EnvCodec[T]) EnvCodec[S] {
	return codec.BindTo(mergeEnv(), p, t)
}

// apSL is wiring: it adds the codec of the field that l focuses on to the
// codec of a struct S.
func apSL[S, T any](l Lens[S, T], t EnvCodec[T]) func(EnvCodec[S]) EnvCodec[S] {
	return codec.ApSL(mergeEnv(), l, t)
}
