package openai

import (
	_ "embed"

	A "github.com/IBM/fp-go/v2/array"
	"github.com/openai/openai-go/v3"
)

// askPrompt is the system prompt used for Ask mode.
//
//go:embed prompts/ask.txt
var askPrompt string

func ForAskMode() openai.ChatCompletionNewParams {
	return openai.ChatCompletionNewParams{
		Messages: A.Of(openai.SystemMessage(askPrompt)),
	}
}
