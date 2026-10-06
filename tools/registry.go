package tools

import (
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	Mg "github.com/IBM/fp-go/v2/magma"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	R "github.com/IBM/fp-go/v2/record"
	S "github.com/IBM/fp-go/v2/string"
	"github.com/openai/openai-go/v3"
)

// Tool pairs the definition advertised to the model with its implementation.
type Tool struct {
	Definition openai.FunctionDefinitionParam
	Call       ToolCall
}

// ToolRegistry maps tool names to their [Tool].
type ToolRegistry = map[string]Tool

func MakeTool(definition openai.FunctionDefinitionParam, call ToolCall) Tool {
	return Tool{Definition: definition, Call: call}
}

func toolName(t Tool) string {
	return t.Definition.Name
}

func toolCall(t Tool) ToolCall {
	return t.Call
}

func toolEntry(t Tool) pair.Pair[string, Tool] {
	return pair.MakePair(toolName(t), t)
}

func toToolParam(t Tool) openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(t.Definition)
}

// MakeToolRegistry indexes tools by their definition name. If several tools
// share a name, the last one wins.
func MakeToolRegistry(tools ...Tool) ToolRegistry {
	return R.FromArrayMap[Tool, string](Mg.Second[Tool]())(toolEntry)(tools)
}

// ToToolCaller derives the [ToolCaller] lookup from a registry.
func ToToolCaller(registry ToolRegistry) ToolCaller {
	return F.Flow2(
		F.Bind1st(R.MonadLookup[Tool, string], registry),
		option.Map(toolCall),
	)
}

// ToToolParams renders the registry into the tool definitions of a chat
// completion request, ordered by name so that requests are deterministic.
func ToToolParams(registry ToolRegistry) []openai.ChatCompletionToolUnionParam {
	return F.Pipe2(
		registry,
		R.ValuesOrd[Tool](S.Ord),
		A.Map(toToolParam),
	)
}

// WithTools returns an endomorphism that sets the tool definitions of a chat
// completion request from the registry.
func WithTools(registry ToolRegistry) Endomorphism[openai.ChatCompletionNewParams] {
	toolsLens := oai.MakeChatCompletionNewParamsToolsLens()

	return F.Pipe1(
		ToToolParams(registry),
		toolsLens.Set,
	)
}

// MakeRegistryToolDeps builds [ToolDeps] whose caller resolves tools from the
// registry.
func MakeRegistryToolDeps(registry ToolRegistry) ToolDeps {
	return F.Pipe2(
		registry,
		ToToolCaller,
		MakeToolDeps,
	)
}
