package session

import (
	"github.com/IBM/fp-go/v2/monoid"
	"github.com/openai/openai-go/v3"
)

func addUsage(a, b openai.CompletionUsage) openai.CompletionUsage {
	return openai.CompletionUsage{
		CompletionTokens: a.CompletionTokens + b.CompletionTokens,
		PromptTokens:     a.PromptTokens + b.PromptTokens,
		TotalTokens:      a.TotalTokens + b.TotalTokens,
		CompletionTokensDetails: openai.CompletionUsageCompletionTokensDetails{
			ReasoningTokens:          a.CompletionTokensDetails.ReasoningTokens + b.CompletionTokensDetails.ReasoningTokens,
			AudioTokens:              a.CompletionTokensDetails.AudioTokens + b.CompletionTokensDetails.AudioTokens,
			AcceptedPredictionTokens: a.CompletionTokensDetails.AcceptedPredictionTokens + b.CompletionTokensDetails.AcceptedPredictionTokens,
			RejectedPredictionTokens: a.CompletionTokensDetails.RejectedPredictionTokens + b.CompletionTokensDetails.RejectedPredictionTokens,
		},
		PromptTokensDetails: openai.CompletionUsagePromptTokensDetails{
			AudioTokens:  a.PromptTokensDetails.AudioTokens + b.PromptTokensDetails.AudioTokens,
			CachedTokens: a.PromptTokensDetails.CachedTokens + b.PromptTokensDetails.CachedTokens,
		},
	}
}

func noUsage() openai.CompletionUsage {
	return openai.CompletionUsage{}
}

func MakeUsageMonoid() monoid.Monoid[openai.CompletionUsage] {
	return monoid.MakeMonoid(addUsage, noUsage())
}
