package replay

import (
	"github.com/IBM/fp-go/v2/endomorphism"
	"github.com/IBM/fp-go/v2/iterator/iterresult"
	"github.com/IBM/fp-go/v2/optics/prism"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/readerresult"
	"github.com/IBM/fp-go/v2/result"
)

type (
	Result[A any]          = result.Result[A]
	Option[A any]          = option.Option[A]
	SeqResult[A any]       = iterresult.SeqResult[A]
	SeqKleisli[A, B any]   = iterresult.Kleisli[A, B]
	Endomorphism[A any]    = endomorphism.Endomorphism[A]
	Prism[S, A any]        = prism.Prism[S, A]
	Pair[A, B any]         = pair.Pair[A, B]
	Predicate[A any]       = predicate.Predicate[A]
	ReaderResult[R, A any] = readerresult.ReaderResult[R, A]
)
