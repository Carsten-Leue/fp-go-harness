package prompt

import (
	"github.com/IBM/fp-go/v2/monoid"
	"github.com/IBM/fp-go/v2/pair"
)

type (
	Monoid[A any]  = monoid.Monoid[A]
	Pair[A, B any] = pair.Pair[A, B]

	// Section is a prompt section: its tag in the head, its body in the tail.
	Section = Pair[string, string]
)
