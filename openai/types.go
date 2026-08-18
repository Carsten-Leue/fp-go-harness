package openai

import (
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/ioresult"
)

type (
	Effect[C, A any] = effect.Effect[C, A]
	Thunk[A any]     = effect.Thunk[A]
	IOResult[A any]  = ioresult.IOResult[A]
)
