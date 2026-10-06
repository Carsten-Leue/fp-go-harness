package env

import (
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/iooption"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/readerioresult"
	"github.com/IBM/fp-go/v2/result"
)

type (
	Effect[C, A any]         = effect.Effect[C, A]
	ReaderIOResult[R, A any] = readerioresult.ReaderIOResult[R, A]
	IOResult[A any]          = ioresult.IOResult[A]
	IOOption[A any]          = iooption.IOOption[A]
	Result[A any]            = result.Result[A]

	// Vars maps variable names to values.
	Vars = map[string]string
)
