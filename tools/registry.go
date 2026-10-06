package tools

import (
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	Mg "github.com/IBM/fp-go/v2/magma"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/readeroption"
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

// toolDefinition and toolCall are leaf accessors: the lens generator cannot
// derive lenses for Tool because its Call field has a function type.
func toolDefinition(t Tool) openai.FunctionDefinitionParam {
	return t.Definition
}

func toolCall(t Tool) ToolCall {
	return t.Call
}

// toolEntry keys a tool by the name of its definition.
func toolEntry() func(Tool) pair.Pair[string, Tool] {
	nameLens := MakeFunctionDefinitionParamNameLens()

	return F.Flow2(
		pair.Of[Tool],
		pair.MapHead[Tool](F.Flow2(toolDefinition, nameLens.Get)),
	)
}

// MakeToolRegistry indexes tools by their definition name. If several tools
// share a name, the last one wins.
func MakeToolRegistry(tools ...Tool) ToolRegistry {
	return R.FromArrayMap[Tool, string](Mg.Second[Tool]())(toolEntry())(tools)
}

// ToToolCaller derives the [ToolCaller] lookup from a registry.
func ToToolCaller() func(ToolRegistry) ToolCaller {
	return F.Flow2(
		F.Flip(R.Lookup[Tool, string]),
		readeroption.Map[string](toolCall),
	)
}

// ToToolParams renders the registry into the tool definitions of a chat
// completion request, ordered by name so that requests are deterministic.
func ToToolParams() func(ToolRegistry) []openai.ChatCompletionToolUnionParam {
	return F.Flow2(
		R.ValuesOrd[Tool](S.Ord),
		A.Map(F.Flow2(toolDefinition, openai.ChatCompletionFunctionTool)),
	)
}

// WithTools returns the endomorphism that sets the tool definitions of a chat
// completion request from the registry.
func WithTools() func(ToolRegistry) Endomorphism[openai.ChatCompletionNewParams] {
	toolsLens := oai.MakeChatCompletionNewParamsToolsLens()

	return F.Flow2(
		ToToolParams(),
		toolsLens.Set,
	)
}

// MakeRegistryToolDeps builds [ToolDeps] whose caller resolves tools from the
// registry.
func MakeRegistryToolDeps() func(ToolRegistry) ToolDeps {
	return F.Flow2(
		ToToolCaller(),
		MakeToolDeps,
	)
}
