package todo

import (
	"github.com/Carsten-Leue/fp-go-harness/tools"
	"github.com/IBM/fp-go/v2/endomorphism"
	L "github.com/IBM/fp-go/v2/optics/lens"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
)

type (
	Endomorphism[A any]         = endomorphism.Endomorphism[A]
	Lens[S, A any]              = L.Lens[S, A]
	Option[A any]               = option.Option[A]
	Reader[R, A any]            = reader.Reader[R, A]
	Record[K comparable, V any] = record.Record[K, V]
	Result[A any]               = result.Result[A]

	ToolResult[T any] = tools.ToolResult[T]
	StateCall[T any]  = tools.StateCall[T]
)
