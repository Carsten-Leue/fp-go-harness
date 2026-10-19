package cli

import (
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/optics/codec"
	"github.com/IBM/fp-go/v2/optics/lens"
	"github.com/IBM/fp-go/v2/optics/prism"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/readeroption"
	"github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
)

type (
	// ReaderIOResult is a computation over a [context.Context] that may fail,
	// the shape a command's action runs.
	ReaderIOResult[A any] = thunk.ReaderIOResult[A]
	IOResult[A any]       = ioresult.IOResult[A]
	IO[A any]             = io.IO[A]
	Result[A any]         = result.Result[A]
	Option[A any]         = option.Option[A]
	Effect[C, A any]      = effect.Effect[C, A]

	Reader[R, A any]       = reader.Reader[R, A]
	ReaderOption[R, A any] = readeroption.ReaderOption[R, A]

	Record[K comparable, V any] = record.Record[K, V]

	Lens[S, A any]  = lens.Lens[S, A]
	Prism[S, A any] = prism.Prism[S, A]

	// EnvDep looks a configuration key up, the input every [EnvCodec] decodes
	// from.
	EnvDep = ReaderOption[string, string]

	// EnvCodec decodes a T from an [EnvDep] and encodes it back to the
	// variables that produce it.
	EnvCodec[T any] = codec.Type[T, Record[string, string], EnvDep]
)
