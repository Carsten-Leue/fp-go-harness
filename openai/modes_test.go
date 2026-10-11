package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModes(t *testing.T) {
	ask := ForAskMode().Messages
	agent := ForAgentMode().Messages

	require.Len(t, ask, 1)
	require.Len(t, agent, 1)

	assert.Contains(t, ask[0].OfSystem.Content.OfString.Value, "Ask mode")
	assert.Contains(t, agent[0].OfSystem.Content.OfString.Value, "Agent mode")
}
