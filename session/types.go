package session

import (
	"github.com/Carsten-Leue/fp-go-harness/tools"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/endomorphism"
	"github.com/IBM/fp-go/v2/optics/prism"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/tailrec"
)

type (
	Effect[C, A any]     = effect.Effect[C, A]
	Trampoline[B, L any] = tailrec.Trampoline[B, L]
	ToolCaller           = tools.ToolCaller
	Thunk[A any]         = effect.Thunk[A]
	Endomorphism[A any]  = endomorphism.Endomorphism[A]
	Reader[R, A any]     = reader.Reader[R, A]
	Pair[L, R any]       = pair.Pair[L, R]
	Prism[S, A any]      = prism.Prism[S, A]
	Predicate[A any]     = predicate.Predicate[A]
)
