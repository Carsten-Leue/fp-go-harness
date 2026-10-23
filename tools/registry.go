package tools

import (
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	Mg "github.com/IBM/fp-go/v2/magma"
	L "github.com/IBM/fp-go/v2/optics/lens"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
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

// MakeTool is a leaf: the constructor of a [Tool] from its definition and
// its call.
func MakeTool(definition openai.FunctionDefinitionParam, call ToolCall) Tool {
	return Tool{Definition: definition, Call: call}
}

// toolDefinition and toolCall are leaf accessors: the lens generator cannot
// derive lenses for Tool because its Call field has a function type.
func toolDefinition(t Tool) openai.FunctionDefinitionParam {
	return t.Definition
}

// toolCall is a leaf accessor, see [toolDefinition].
func toolCall(t Tool) ToolCall {
	return t.Call
}

// MakeToolCallLens focuses on the call of a [Tool]. It is built by hand from
// [toolCall] and [MakeTool], because the generator cannot derive it, see
// [toolDefinition]. The setter keeps the definition.
func MakeToolCallLens() L.Lens[Tool, ToolCall] {
	return L.MakeLens(toolCall, F.Uncurry2(F.Flow2(toolDefinition, F.Curry2(MakeTool))))
}

// toolEntry keys a tool by the name of its definition.
func toolEntry() func(Tool) pair.Pair[string, Tool] {
	nameLens := MakeFunctionDefinitionParamNameLens()

	return F.Flow2(
		pair.Of[Tool],
		pair.MapHead[Tool](F.Flow2(toolDefinition, nameLens.Get)),
	)
}

// ToToolRegistry indexes tools by their definition name. If several tools
// share a name, the last one wins.
func ToToolRegistry() func([]Tool) ToolRegistry {
	return R.FromArrayMap[Tool, string](Mg.Second[Tool]())(toolEntry())
}

// MakeToolRegistry is wiring: the variadic form of [ToToolRegistry].
func MakeToolRegistry(tools ...Tool) ToolRegistry {
	return ToToolRegistry()(tools)
}

// selectFrom picks the tools of a registry by name, in a registry of their
// own. A name without a tool is skipped.
func selectFrom() Reader[ToolRegistry, Reader[[]string, ToolRegistry]] {
	return F.Flow3(
		F.Flip(R.Lookup[Tool, string]),
		A.FilterMap[string, Tool],
		reader.Map[[]string](ToToolRegistry()),
	)
}

// SelectTools keeps the tools of a registry whose names are listed, e.g. the
// tools a mode allows. A listed name without a tool is skipped.
func SelectTools() Reader[[]string, Endomorphism[ToolRegistry]] {
	return F.Flip(selectFrom())
}

// ToToolCaller derives the [ToolCaller] lookup from a registry.
func ToToolCaller() func(ToolRegistry) ToolCaller {
	callLens := MakeToolCallLens()

	return F.Flow2(
		F.Flip(R.Lookup[Tool, string]),
		readeroption.Map[string](callLens.Get),
	)
}

// ToToolParams renders the registry into the tool definitions of a chat
// completion request, ordered by name so that requests are deterministic.
func ToToolParams() A.Kleisli[ToolRegistry, openai.ChatCompletionToolUnionParam] {
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
