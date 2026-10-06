package tools

import (
	"encoding/json"
	"testing"

	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeEchoTool(name, prefix string) Tool {
	return MakeTool(
		openai.FunctionDefinitionParam{
			Name:        name,
			Description: openai.String("echoes its arguments"),
			Parameters: openai.FunctionParameters{
				"type":       "object",
				"properties": map[string]any{"text": map[string]any{"type": "string"}},
			},
		},
		func(arguments string) Thunk[string] {
			return thunk.Of(prefix + arguments)
		},
	)
}

func TestToToolCaller_ResolvesRegisteredTools(t *testing.T) {
	registry := MakeToolRegistry(makeEchoTool("echo", "echo: "))
	caller := ToToolCaller(registry)

	call, ok := option.Unwrap(caller("echo"))
	require.True(t, ok)

	out, err := result.Unwrap(call(`{"text":"hi"}`)(t.Context())())
	require.NoError(t, err)
	assert.Equal(t, `echo: {"text":"hi"}`, out)

	assert.True(t, option.IsNone(caller("missing")))
}

func TestMakeToolRegistry_LastToolWins(t *testing.T) {
	registry := MakeToolRegistry(makeEchoTool("echo", "first: "), makeEchoTool("echo", "second: "))
	require.Len(t, registry, 1)

	call, ok := option.Unwrap(ToToolCaller(registry)("echo"))
	require.True(t, ok)

	out, err := result.Unwrap(call("x")(t.Context())())
	require.NoError(t, err)
	assert.Equal(t, "second: x", out)
}

// TestWithTools_SetsSortedDefinitions asserts that the registry is rendered
// into the request as function tools, sorted by name.
func TestWithTools_SetsSortedDefinitions(t *testing.T) {
	registry := MakeToolRegistry(makeEchoTool("zeta", ""), makeEchoTool("alpha", ""))

	params := WithTools(registry)(openai.ChatCompletionNewParams{Model: "gpt-test"})
	require.Len(t, params.Tools, 2)

	raw, err := json.Marshal(params.Tools)
	require.NoError(t, err)

	var decoded []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			Parameters  map[string]any `json:"parameters"`
		} `json:"function"`
	}
	require.NoError(t, json.Unmarshal(raw, &decoded))

	assert.Equal(t, "function", decoded[0].Type)
	assert.Equal(t, "alpha", decoded[0].Function.Name)
	assert.Equal(t, "zeta", decoded[1].Function.Name)
	assert.Equal(t, "echoes its arguments", decoded[0].Function.Description)
	assert.Equal(t, "object", decoded[0].Function.Parameters["type"])
}
