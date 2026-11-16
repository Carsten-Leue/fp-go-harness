package tools

import (
	B "github.com/IBM/fp-go/v2/bytes"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	J "github.com/IBM/fp-go/v2/json"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/readerresult"
	"github.com/openai/openai-go/v3"
)

type ToolCall = thunk.Kleisli[string, string]

type ToolCaller = ReaderOption[string, ToolCall]

type ToolDeps interface {
	GetToolCaller() ToolCaller
}

type toolDeps struct {
	toolCaller ToolCaller
}

func (d *toolDeps) GetToolCaller() ToolCaller {
	return d.toolCaller
}

// MakeToolDeps is wiring: it combines a [ToolCaller] into [ToolDeps].
func MakeToolDeps(tc ToolCaller) ToolDeps {
	return &toolDeps{tc}
}

// AsToolDeps is wiring: it narrows any wider dependencies to [ToolDeps], for
// use with [reader.Local] and [effect.Local].
func AsToolDeps[R ToolDeps](r R) ToolDeps {
	return r
}

type errorResult struct {
	Error   bool   `json:"error"`
	Message string `json:"message"`
}

// makeErrorResult is a leaf: the constructor of an error result.
func makeErrorResult(message string) errorResult {
	return errorResult{Error: true, Message: message}
}

func makeSuccessChatCompletionMessageParamUnion() func(string) Reader[openai.ChatCompletionMessageToolCallUnion, openai.ChatCompletionMessageParamUnion] {
	idLens := MakeChatCompletionMessageToolCallUnionIDLens()

	return F.Flow2(
		F.Curry2(openai.ToolMessage[string]),
		reader.Local[openai.ChatCompletionMessageParamUnion](idLens.Get),
	)
}

func makeErrorChatCompletionMessageParamUnion() func(string) ReaderResult[openai.ChatCompletionMessageToolCallUnion, openai.ChatCompletionMessageParamUnion] {
	toMessage := F.Flow2(
		B.ToString,
		makeSuccessChatCompletionMessageParamUnion(),
	)

	return F.Flow4(
		makeErrorResult,
		J.Marshal,
		readerresult.FromResult,
		readerresult.ChainReaderK(toMessage),
	)
}

// atUnit runs a session-changing handler with plain [ToolDeps]: the plain
// tools are lifted at the unit state, see [LiftToolDeps], and the change,
// always the identity, is dropped. The plain handlers are built this way, so
// there is one implementation of the tool calls.
func atUnit[T, U any]() func(effect.Kleisli[StateToolDeps[F.Void], T, Pair[Endomorphism[F.Void], U]]) effect.Kleisli[ToolDeps, T, U] {
	return reader.Map[T](F.Flow2(
		effect.Local[Pair[Endomorphism[F.Void], U]](LiftToolDeps[F.Void]()),
		effect.Map[ToolDeps](pair.Tail[Endomorphism[F.Void], U]),
	))
}

// MakeToolCall builds a Kleisli arrow that resolves and executes a single tool
// call requested by the model. Given a ToolCaller registry, it looks up the
// tool by name, invokes it with the call's arguments, and turns the outcome —
// success, an unknown tool name, or a failed invocation — into a tool
// ChatCompletionMessageParamUnion carrying the call's ID. The returned Effect
// never fails: every error is captured in the resulting message rather than
// propagated as a Result error. It is [MakeStateToolCall] of plain tools.
func MakeToolCall() effect.Kleisli[ToolDeps, openai.ChatCompletionMessageToolCallUnion, openai.ChatCompletionMessageParamUnion] {
	return atUnit[openai.ChatCompletionMessageToolCallUnion, openai.ChatCompletionMessageParamUnion]()(MakeStateToolCall[F.Void]())
}

func MakeToolCalls() effect.Kleisli[ToolDeps, []openai.ChatCompletionMessageToolCallUnion, []openai.ChatCompletionMessageParamUnion] {
	return F.Pipe1(
		MakeToolCall(),
		effect.TraverseArray,
	)
}

// handleToolCallsForChoice is [handleStateToolCallsForChoice] of plain tools.
func handleToolCallsForChoice() effect.Kleisli[ToolDeps, openai.ChatCompletionChoice, Endomorphism[openai.ChatCompletionNewParams]] {
	return atUnit[openai.ChatCompletionChoice, Endomorphism[openai.ChatCompletionNewParams]]()(handleStateToolCallsForChoice[F.Void]())
}

// HandleToolCalls resolves and executes every tool call across all choices of
// a ChatCompletion, folding the per-choice results (see
// handleToolCallsForChoice) into a single Endomorphism that appends the
// resulting assistant/tool messages to a ChatCompletionNewParams. A
// completion whose choices carry no tool calls yields the identity
// endomorphism. It is [HandleStateToolCalls] of plain tools.
func HandleToolCalls() effect.Kleisli[ToolDeps, *openai.ChatCompletion, Endomorphism[openai.ChatCompletionNewParams]] {
	return atUnit[*openai.ChatCompletion, Endomorphism[openai.ChatCompletionNewParams]]()(HandleStateToolCalls[F.Void]())
}
