package openai

import (
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/readeroption"
)

type (
	Effect[C, A any]       = effect.Effect[C, A]
	Thunk[A any]           = effect.Thunk[A]
	IOResult[A any]        = ioresult.IOResult[A]
	Option[A any]          = option.Option[A]
	ReaderOption[R, A any] = readeroption.ReaderOption[R, A]
)
