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

// ToolOf pairs the definition advertised to the model with its
// implementation, a call of type C. It is the one tool type: a plain [Tool]
// has a [ToolCall], a [StateTool] has a [StateCall] that can also change a
// state such as the session, and a plain tool lifts into a StateTool with
// [LiftTool]. The registry functions ([ToToolRegistryOf], [SelectToolsOf],
// [ToToolCallerOf], [ToToolParamsOf], [WithToolsOf]) work on every ToolOf;
// the wrappers of a call work on the [StateCall], see there.
type ToolOf[C any] struct {
	Definition openai.FunctionDefinitionParam
	Call       C
}

// Tool is a plain tool: its call returns the text for the model and changes
// nothing else. Every built-in tool, use_skill and the later bridges (G3,
// K7) are plain tools.
type Tool = ToolOf[ToolCall]

// RegistryOf maps tool names to their [ToolOf].
type RegistryOf[C any] = map[string]ToolOf[C]

// ToolRegistry maps tool names to their [Tool].
type ToolRegistry = RegistryOf[ToolCall]

// MakeToolOf is a leaf: the constructor of a [ToolOf] from its definition
// and its call.
func MakeToolOf[C any](definition openai.FunctionDefinitionParam, call C) ToolOf[C] {
	return ToolOf[C]{Definition: definition, Call: call}
}

// MakeTool is a leaf: the constructor of a [Tool] from its definition and
// its call.
func MakeTool(definition openai.FunctionDefinitionParam, call ToolCall) Tool {
	return MakeToolOf(definition, call)
}

// toolDefinition and toolCall are leaf accessors: the lens generator cannot
// derive lenses for ToolOf because its Call field has a function type.
func toolDefinition[C any](t ToolOf[C]) openai.FunctionDefinitionParam {
	return t.Definition
}

// toolCall is a leaf accessor, see [toolDefinition].
func toolCall[C any](t ToolOf[C]) C {
	return t.Call
}

// MakeToolOfCallLens focuses on the call of a [ToolOf]. It is built by hand
// from [toolCall] and [MakeToolOf], because the generator cannot derive it,
// see [toolDefinition]. The setter keeps the definition.
func MakeToolOfCallLens[C any]() L.Lens[ToolOf[C], C] {
	return L.MakeLens(toolCall[C], F.Uncurry2(F.Flow2(toolDefinition[C], F.Curry2(MakeToolOf[C]))))
}

// MakeToolCallLens is [MakeToolOfCallLens] of a plain [Tool].
func MakeToolCallLens() L.Lens[Tool, ToolCall] {
	return MakeToolOfCallLens[ToolCall]()
}

// MapToolCall changes the call of a tool, also into a call of another type,
// and keeps its definition. The definition and the call are two independent
// reads of the tool.
func MapToolCall[C, D any]() Reader[func(C) D, func(ToolOf[C]) ToolOf[D]] {
	return F.Flow3(
		reader.Local[D](toolCall[C]),
		reader.Ap[ToolOf[D], ToolOf[C], D],
		reader.Read[Reader[ToolOf[C], ToolOf[D]]](F.Flow2(toolDefinition[C], F.Curry2(MakeToolOf[D]))),
	)
}

// toolEntryOf keys a tool by the name of its definition.
func toolEntryOf[C any]() func(ToolOf[C]) pair.Pair[string, ToolOf[C]] {
	nameLens := MakeFunctionDefinitionParamNameLens()

	return F.Flow2(
		pair.Of[ToolOf[C]],
		pair.MapHead[ToolOf[C]](F.Flow2(toolDefinition[C], nameLens.Get)),
	)
}

// toolEntry is [toolEntryOf] of a plain [Tool].
func toolEntry() func(Tool) pair.Pair[string, Tool] {
	return toolEntryOf[ToolCall]()
}

// ToToolRegistryOf indexes tools by their definition name. If several tools
// share a name, the last one wins.
func ToToolRegistryOf[C any]() func([]ToolOf[C]) RegistryOf[C] {
	return R.FromArrayMap[ToolOf[C], string](Mg.Second[ToolOf[C]]())(toolEntryOf[C]())
}

// ToToolRegistry is [ToToolRegistryOf] of plain tools.
func ToToolRegistry() func([]Tool) ToolRegistry {
	return ToToolRegistryOf[ToolCall]()
}

// MakeToolRegistry is wiring: the variadic form of [ToToolRegistry].
func MakeToolRegistry(tools ...Tool) ToolRegistry {
	return ToToolRegistry()(tools)
}

// selectFrom picks the tools of a registry by name, in a registry of their
// own. A name without a tool is skipped.
func selectFrom[C any]() Reader[RegistryOf[C], Reader[[]string, RegistryOf[C]]] {
	return F.Flow3(
		F.Flip(R.Lookup[ToolOf[C], string]),
		A.FilterMap[string, ToolOf[C]],
		reader.Map[[]string](ToToolRegistryOf[C]()),
	)
}

// SelectToolsOf keeps the tools of a registry whose names are listed, e.g.
// the tools a mode allows. A listed name without a tool is skipped.
func SelectToolsOf[C any]() Reader[[]string, Endomorphism[RegistryOf[C]]] {
	return F.Flip(selectFrom[C]())
}

// SelectTools is [SelectToolsOf] of plain tools.
func SelectTools() Reader[[]string, Endomorphism[ToolRegistry]] {
	return SelectToolsOf[ToolCall]()
}

// ToToolCallerOf derives the lookup of the calls by name from a registry.
func ToToolCallerOf[C any]() func(RegistryOf[C]) ReaderOption[string, C] {
	callLens := MakeToolOfCallLens[C]()

	return F.Flow2(
		F.Flip(R.Lookup[ToolOf[C], string]),
		readeroption.Map[string](callLens.Get),
	)
}

// ToToolCaller derives the [ToolCaller] lookup from a registry.
func ToToolCaller() func(ToolRegistry) ToolCaller {
	return ToToolCallerOf[ToolCall]()
}

// ToToolParamsOf renders the registry into the tool definitions of a chat
// completion request, ordered by name so that requests are deterministic.
func ToToolParamsOf[C any]() A.Kleisli[RegistryOf[C], openai.ChatCompletionToolUnionParam] {
	return F.Flow2(
		R.ValuesOrd[ToolOf[C]](S.Ord),
		A.Map(F.Flow2(toolDefinition[C], openai.ChatCompletionFunctionTool)),
	)
}

// ToToolParams is [ToToolParamsOf] of plain tools.
func ToToolParams() A.Kleisli[ToolRegistry, openai.ChatCompletionToolUnionParam] {
	return ToToolParamsOf[ToolCall]()
}

// WithToolsOf returns the endomorphism that sets the tool definitions of a
// chat completion request from the registry.
func WithToolsOf[C any]() func(RegistryOf[C]) Endomorphism[openai.ChatCompletionNewParams] {
	toolsLens := oai.MakeChatCompletionNewParamsToolsLens()

	return F.Flow2(
		ToToolParamsOf[C](),
		toolsLens.Set,
	)
}

// WithTools is [WithToolsOf] of plain tools.
func WithTools() func(ToolRegistry) Endomorphism[openai.ChatCompletionNewParams] {
	return WithToolsOf[ToolCall]()
}

// MakeRegistryToolDeps builds [ToolDeps] whose caller resolves tools from the
// registry.
func MakeRegistryToolDeps() func(ToolRegistry) ToolDeps {
	return F.Flow2(
		ToToolCaller(),
		MakeToolDeps,
	)
}

// MakeStateRegistryToolDeps builds [StateToolDeps] whose caller resolves
// tools from the registry.
func MakeStateRegistryToolDeps[T any]() func(StateRegistry[T]) StateToolDeps[T] {
	return F.Flow2(
		ToToolCallerOf[StateCall[T]](),
		MakeStateToolDeps[T],
	)
}
