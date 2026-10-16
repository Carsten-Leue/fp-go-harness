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

// agentPrompt is the system prompt used for Agent mode.
//
//go:embed prompts/agent.txt
var agentPrompt string

// ForAskMode is the request of Ask mode: the model answers questions and
// does not change the workspace.
func ForAskMode() openai.ChatCompletionNewParams {
	return openai.ChatCompletionNewParams{
		Messages: A.Of(openai.SystemMessage(askPrompt)),
	}
}

// ForAgentMode is the request of Agent mode: the model carries out a task
// and may change files and run commands.
func ForAgentMode() openai.ChatCompletionNewParams {
	return openai.ChatCompletionNewParams{
		Messages: A.Of(openai.SystemMessage(agentPrompt)),
	}
}
