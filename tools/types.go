package tools

import (
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/readeroption"
	"github.com/IBM/fp-go/v2/readerresult"
	"github.com/IBM/fp-go/v2/result"
)

type (
	Effect[C, A any]       = effect.Effect[C, A]
	Thunk[A any]           = effect.Thunk[A]
	ReaderOption[R, A any] = readeroption.ReaderOption[R, A]
	Result[A any]          = result.Result[A]
	Reader[R, A any]       = reader.Reader[R, A]
	ReaderResult[R, A any] = readerresult.ReaderResult[R, A]
	Option[A any]          = option.Option[A]
)
