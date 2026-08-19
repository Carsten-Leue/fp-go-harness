package openai

import (
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/readerioresult"
	"github.com/IBM/fp-go/v2/readeroption"
	"github.com/IBM/fp-go/v2/result"
)

type (
	Effect[C, A any]         = effect.Effect[C, A]
	Thunk[A any]             = effect.Thunk[A]
	IOResult[A any]          = ioresult.IOResult[A]
	Option[A any]            = option.Option[A]
	ReaderOption[R, A any]   = readeroption.ReaderOption[R, A]
	Result[A any]            = result.Result[A]
	Reader[R, A any]         = reader.Reader[R, A]
	ReaderIOResult[R, A any] = readerioresult.ReaderIOResult[R, A]
)
