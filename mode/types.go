package mode

import (
	"github.com/Carsten-Leue/fp-go-harness/prompt"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/endomorphism"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
)

type (
	Effect[C, A any]    = effect.Effect[C, A]
	Endomorphism[A any] = endomorphism.Endomorphism[A]
	Option[A any]       = option.Option[A]
	Reader[R, A any]    = reader.Reader[R, A]
	Result[A any]       = result.Result[A]

	Section = prompt.Section
	// Params is the chat completion request a mode starts with.
	Params = openai.ChatCompletionNewParams
)
