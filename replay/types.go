package replay

import (
	"github.com/IBM/fp-go/v2/endomorphism"
	"github.com/IBM/fp-go/v2/iterator/iterresult"
	"github.com/IBM/fp-go/v2/result"
)

type (
	Result[A any]        = result.Result[A]
	SeqResult[A any]     = iterresult.SeqResult[A]
	SeqKleisli[A, B any] = iterresult.Kleisli[A, B]
	Endomorphism[A any]  = endomorphism.Endomorphism[A]
)
