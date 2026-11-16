package tools

import (
	"slices"

	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	A "github.com/IBM/fp-go/v2/array"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/endomorphism"
	F "github.com/IBM/fp-go/v2/function"
	M "github.com/IBM/fp-go/v2/monoid"
	L "github.com/IBM/fp-go/v2/optics/lens"
	P "github.com/IBM/fp-go/v2/optics/prism"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/readeroption"
	"github.com/IBM/fp-go/v2/readerresult"
	R "github.com/IBM/fp-go/v2/record"
	S "github.com/IBM/fp-go/v2/string"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// ToolResult is the result of a tool that may change a state of type T: the
// change, as an endomorphism of the state, and the text for the model. The
// change is the head, like the log of a writer; the text is the tail. A plain
// tool's result is the text with the identity, see [Unchanged].
//
// The state is a type parameter because package tools cannot import package
// session (session imports tools): the session instantiates T with its
// Session, see session.SessionCall.
//
// Every wrapper of a tool works on the one tool type [ToolOf] with a
// [StateCall], so that it applies to plain and session-changing tools alike:
//   - Truncate (C11): [TruncateCall] and [TruncateStateTools] shorten the
//     text and keep the change.
//   - SelectTools (D5): [SelectToolsOf] keeps the tools a mode allows.
//   - I1's approval wrapper: a refused call is a text with the identity.
//   - F2's Tap: tool started and finished events around a [StateCall].
//   - The G3 (MCP) and K7 (subagent) bridges: plain tools, lifted with
//     [LiftTool].
//   - H3 (update_todo_list), H5 (switch_mode) and J6: session-changing tools,
//     built with [MakeToolOf] from a [StateCall].
type ToolResult[T any] = Pair[Endomorphism[T], string]

// StateCall is the call of a tool that returns its text and a change of the
// state T, see [ToolResult].
type StateCall[T any] = thunk.Kleisli[string, ToolResult[T]]

// StateTool is a tool whose call may change the state T.
type StateTool[T any] = ToolOf[StateCall[T]]

// StateRegistry maps tool names to their [StateTool].
type StateRegistry[T any] = RegistryOf[StateCall[T]]

// StateToolCaller looks up the [StateCall] of a tool by name.
type StateToolCaller[T any] = ReaderOption[string, StateCall[T]]

// StateToolDeps are the dependencies of [MakeStateToolCall]. The getter has
// the name of the one of [ToolDeps], so a dependency struct implements one of
// them.
type StateToolDeps[T any] interface {
	GetToolCaller() StateToolCaller[T]
}

type stateToolDeps[T any] struct {
	toolCaller StateToolCaller[T]
}

func (d *stateToolDeps[T]) GetToolCaller() StateToolCaller[T] {
	return d.toolCaller
}

// MakeStateToolDeps is wiring: it combines a [StateToolCaller] into
// [StateToolDeps].
func MakeStateToolDeps[T any](tc StateToolCaller[T]) StateToolDeps[T] {
	return &stateToolDeps[T]{tc}
}

// AsStateToolDeps is wiring: it narrows any wider dependencies to
// [StateToolDeps], for use with [reader.Local] and [effect.Local].
func AsStateToolDeps[T any, R StateToolDeps[T]](r R) StateToolDeps[T] {
	return r
}

// ToolMessage is the tool message of one tool call with the change of the
// state the call made.
type ToolMessage[T any] = Pair[Endomorphism[T], openai.ChatCompletionMessageParamUnion]

// ToolUpdate is what the tool calls of a completion do: the change of the
// state, applied in the order of the tool calls, and the change of the next
// request, which appends the assistant and the tool messages.
type ToolUpdate[T any] = Pair[Endomorphism[T], Endomorphism[openai.ChatCompletionNewParams]]

// Unchanged pairs a text with the identity: the result of a call that
// changes nothing.
func Unchanged[T any]() func(string) ToolResult[T] {
	return pair.FromHead[string](endomorphism.Identity[T]())
}

// LiftCall lifts a plain call into a [StateCall] that changes nothing.
func LiftCall[T any]() func(ToolCall) StateCall[T] {
	return effect.Map[string](Unchanged[T]())
}

// LiftTool lifts a plain tool into a [StateTool] that changes nothing.
func LiftTool[T any]() func(Tool) StateTool[T] {
	return MapToolCall[ToolCall, StateCall[T]]()(LiftCall[T]())
}

// LiftRegistry lifts every tool of a plain registry, see [LiftTool].
func LiftRegistry[T any]() func(ToolRegistry) StateRegistry[T] {
	return R.Map[string](LiftTool[T]())
}

// LiftToolCaller lifts every call a plain caller finds, see [LiftCall].
func LiftToolCaller[T any]() func(ToolCaller) StateToolCaller[T] {
	return readeroption.Map[string](LiftCall[T]())
}

// LiftToolDeps lifts plain [ToolDeps] into [StateToolDeps], see
// [LiftToolCaller].
func LiftToolDeps[T any]() func(ToolDeps) StateToolDeps[T] {
	return F.Flow3(
		ToolDeps.GetToolCaller,
		LiftToolCaller[T](),
		MakeStateToolDeps[T],
	)
}

// OfferedToolNames are the names of the function tools a request offers the
// model, in order.
func OfferedToolNames() A.Kleisli[openai.ChatCompletionNewParams, string] {
	toolsLens := oai.MakeChatCompletionNewParamsToolsLens()
	nameLens := MakeFunctionDefinitionParamNameLens()

	return F.Flow2(
		toolsLens.Get,
		A.FilterMap(F.Flow3(
			openai.ChatCompletionToolUnionParam.GetFunction,
			option.FromNillable2[shared.FunctionDefinitionParam],
			option.Map(nameLens.Get),
		)),
	)
}

// OfferedCaller restricts a caller to the tools of names: any other name is
// not found, so its call is the tool message "Unable to find tool", see
// [MakeStateToolCall].
func OfferedCaller[T any]() func([]string) Endomorphism[StateToolCaller[T]] {
	return F.Flow4(
		F.Curry2(slices.Contains[[]string]),
		option.FromPredicate[string],
		reader.Compose[Option[StateCall[T]], string, Option[string]],
		reader.Compose[StateToolCaller[T]](option.Chain[string, StateCall[T]]),
	)
}

// OfferedToolDeps restricts the caller of [StateToolDeps] to the tools a
// request offers the model, see [OfferedToolNames] and [OfferedCaller]: the
// model can only call what it was offered, also when the registry of the
// caller has more tools, as after a switch_mode (H5) to a mode with fewer
// tools.
func OfferedToolDeps[T any]() func(openai.ChatCompletionNewParams) Endomorphism[StateToolDeps[T]] {
	return F.Flow4(
		OfferedToolNames(),
		OfferedCaller[T](),
		reader.Map[StateToolCaller[T]](MakeStateToolDeps[T]),
		reader.Compose[StateToolDeps[T]](StateToolDeps[T].GetToolCaller),
	)
}

// MakeStateToolCall resolves and executes a single tool call, like
// [MakeToolCall], and keeps the change the call made next to its tool
// message. An unknown tool and a failed call are tool messages with the
// error and the identity: a failed tool changes nothing. The effect never
// fails.
func MakeStateToolCall[T any]() effect.Kleisli[StateToolDeps[T], openai.ChatCompletionMessageToolCallUnion, ToolMessage[T]] {
	functionLens := MakeChatCompletionMessageToolCallUnionFunctionLens()
	nameLens := MakeChatCompletionMessageFunctionToolCallFunctionNameLens()
	argsLens := MakeChatCompletionMessageFunctionToolCallFunctionArgumentsLens()

	makeError := F.Flow2(
		makeErrorChatCompletionMessageParamUnion(),
		readerresult.Map[openai.ChatCompletionMessageToolCallUnion](pair.FromHead[openai.ChatCompletionMessageParamUnion](endomorphism.Identity[T]())),
	)

	// the text becomes the tool message, the change is kept
	makeSuccess := F.Flip(F.Flow2(
		F.Flip(makeSuccessChatCompletionMessageParamUnion()),
		pair.MapTail[Endomorphism[T], string, openai.ChatCompletionMessageParamUnion],
	))

	functionNameLens := functionLens.Compose(nameLens)

	functionArgsLens := functionLens.Compose(argsLens)

	toolNotFoundError := F.Pipe4(
		functionNameLens.Get,
		reader.Map[openai.ChatCompletionMessageToolCallUnion](S.Format[string]("Unable to find tool '%s'.")),
		readerresult.Asks,
		readerresult.Chain(makeError),
		effect.FromReaderResult,
	)

	generalError := F.Flow3(
		error.Error,
		makeError,
		effect.FromReaderResult,
	)

	return F.Pipe1(
		functionNameLens.Get,
		reader.Chain(F.Flow5(
			readeroption.Read[StateCall[T], string],
			readeroption.Local[StateCall[T]](StateToolDeps[T].GetToolCaller),
			readeroption.Map[StateToolDeps[T]](F.Flow3(
				effect.Local[ToolResult[T]](functionArgsLens.Get),
				effect.ChainReaderK(makeSuccess),
				effect.ChainLeft(generalError),
			)),
			readeroption.GetOrElse(F.Pipe1(
				toolNotFoundError,
				reader.Of[StateToolDeps[T]],
			)),
			reader.Sequence,
		)),
	)
}

// MakeStateToolCalls executes the tool calls concurrently, see
// [MakeStateToolCall]; the results keep the order of the calls.
func MakeStateToolCalls[T any]() effect.Kleisli[StateToolDeps[T], []openai.ChatCompletionMessageToolCallUnion, []ToolMessage[T]] {
	return F.Pipe1(
		MakeStateToolCall[T](),
		effect.TraverseArray,
	)
}

// inCallOrder composes endomorphisms so that the first one is applied first.
// The endomorphism monoid applies its right operand first, its reverse its
// left one.
func inCallOrder[T any]() M.Monoid[Endomorphism[T]] {
	return M.Reverse(endomorphism.Monoid[T]())
}

// handleStateToolCallsForChoice is [handleToolCallsForChoice] that also
// composes the changes of the calls, in the order of the calls. A choice
// without tool calls changes nothing.
func handleStateToolCallsForChoice[T any]() effect.Kleisli[StateToolDeps[T], openai.ChatCompletionChoice, ToolUpdate[T]] {
	messageLens := oai.MakeChatCompletionChoiceMessageLens()
	toolCallsLens := oai.MakeChatCompletionMessageToolCallsLens()

	toolCallsFromChoiceOptional := messageLens.
		Compose(toolCallsLens).
		ComposePrism(P.FromPredicate(A.IsNonEmpty[openai.ChatCompletionMessageToolCallUnion]))

	// the changes in call order and the messages in call order
	collect := A.FoldMap[ToolMessage[T]](pair.Monoid(
		inCallOrder[T](),
		A.Monoid[openai.ChatCompletionMessageParamUnion](),
	))(pair.MapTail[Endomorphism[T]](A.Of[openai.ChatCompletionMessageParamUnion]))

	makeToolCalls := F.Flow2(
		MakeStateToolCalls[T](),
		effect.Map[StateToolDeps[T]](collect),
	)

	messageToParam := F.Flow2(
		messageLens.Get,
		openai.ChatCompletionMessage.ToParam,
	)

	messagesLens := oai.MakeChatCompletionNewParamsMessagesLens()

	appendMessages := F.Flow3(
		A.Concat[openai.ChatCompletionMessageParamUnion],
		L.Modify[openai.ChatCompletionNewParams],
		reader.Read[Endomorphism[openai.ChatCompletionNewParams]](messagesLens),
	)

	// the assistant message comes before the tool messages; both are read from
	// the choice independently
	prependMessage := F.Flow3(
		messageToParam,
		A.Prepend[openai.ChatCompletionMessageParamUnion],
		F.Flow2(
			pair.MapTail[Endomorphism[T], []openai.ChatCompletionMessageParamUnion, []openai.ChatCompletionMessageParamUnion],
			effect.Map[StateToolDeps[T], Pair[Endomorphism[T], []openai.ChatCompletionMessageParamUnion], Pair[Endomorphism[T], []openai.ChatCompletionMessageParamUnion]],
		),
	)

	return F.Pipe3(
		readeroption.Asks(prependMessage),
		readeroption.Ap[Effect[StateToolDeps[T], Pair[Endomorphism[T], []openai.ChatCompletionMessageParamUnion]]](F.Pipe1(
			toolCallsFromChoiceOptional.GetOption,
			readeroption.Map[openai.ChatCompletionChoice](makeToolCalls),
		)),
		readeroption.Map[openai.ChatCompletionChoice](
			F.Pipe2(
				appendMessages,
				pair.MapTail[Endomorphism[T], []openai.ChatCompletionMessageParamUnion, Endomorphism[openai.ChatCompletionNewParams]],
				effect.Map[StateToolDeps[T]],
			),
		),
		readeroption.GetOrElse(
			F.Pipe2(
				pair.MakePair(endomorphism.Identity[T](), endomorphism.Identity[openai.ChatCompletionNewParams]()),
				effect.Of[StateToolDeps[T]],
				reader.Of[openai.ChatCompletionChoice],
			),
		),
	)
}

// HandleStateToolCalls resolves and executes every tool call across all
// choices of a completion, like [HandleToolCalls], and also composes the
// changes of the state the calls made, in the order of the tool calls,
// whatever order the concurrent calls finish in. A completion without tool
// calls yields the identity twice.
func HandleStateToolCalls[T any]() effect.Kleisli[StateToolDeps[T], *openai.ChatCompletion, ToolUpdate[T]] {
	choicesLens := oai.MakeChatCompletionChoicesRefLens()
	flatten := pair.Monoid(inCallOrder[T](), endomorphism.Monoid[openai.ChatCompletionNewParams]())

	return F.Pipe2(
		handleStateToolCallsForChoice[T](),
		effect.TraverseArray,
		reader.ProMap(
			choicesLens.Get,
			F.Pipe2(
				flatten,
				A.Fold[ToolUpdate[T]],
				effect.Map[StateToolDeps[T]],
			),
		),
	)
}
