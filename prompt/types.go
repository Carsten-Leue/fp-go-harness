package prompt

import (
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/monoid"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
)

type (
	Effect[C, A any] = effect.Effect[C, A]
	IO[A any]        = io.IO[A]
	Monoid[A any]    = monoid.Monoid[A]
	Pair[A, B any]   = pair.Pair[A, B]
	Reader[R, A any] = reader.Reader[R, A]

	// Section is a prompt section: its tag in the head, its body in the tail.
	Section = Pair[string, string]
)
