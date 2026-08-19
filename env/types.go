package env

import (
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/readerioresult"
)

type (
	Effect[C, A any]         = effect.Effect[C, A]
	ReaderIOResult[R, A any] = readerioresult.ReaderIOResult[R, A]
	IOResult[A any]          = ioresult.IOResult[A]
)
