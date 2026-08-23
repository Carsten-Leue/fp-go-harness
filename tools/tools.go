package tools

import (
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	A "github.com/IBM/fp-go/v2/array"
	B "github.com/IBM/fp-go/v2/bytes"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/endomorphism"
	F "github.com/IBM/fp-go/v2/function"
	J "github.com/IBM/fp-go/v2/json"
	L "github.com/IBM/fp-go/v2/optics/lens"
	LP "github.com/IBM/fp-go/v2/optics/lens/prism"
	P "github.com/IBM/fp-go/v2/optics/prism"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/readeroption"
	"github.com/IBM/fp-go/v2/readerresult"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
	"github.com/openai/openai-go/v3"
)

type ToolCall = thunk.Kleisli[string, string]

type ToolCaller = ReaderOption[string, ToolCall]

type errorResult struct {
	Error   bool   `json:"error"`
	Message string `json:"message"`
}

func makeErrorResult(message string) errorResult {
	return errorResult{Error: true, Message: message}
}

func makeErrorChatCompletionMessageParamUnion() func(string) ReaderResult[openai.ChatCompletionMessageToolCallUnion, openai.ChatCompletionMessageParamUnion] {
	idLens := MakeChatCompletionMessageToolCallUnionIDLens()

	return F.Flow4(
		makeErrorResult,
		J.Marshal,
		result.Map(F.Flow4(
			B.ToString,
			F.Curry2(openai.ToolMessage[string]),
			reader.Local[openai.ChatCompletionMessageParamUnion](idLens.Get),
			readerresult.Asks,
		)),
		result.GetOrElse(readerresult.Left[openai.ChatCompletionMessageToolCallUnion, openai.ChatCompletionMessageParamUnion]),
	)
}

func makeSuccessChatCompletionMessageParamUnion() func(string) Reader[openai.ChatCompletionMessageToolCallUnion, openai.ChatCompletionMessageParamUnion] {
	idLens := MakeChatCompletionMessageToolCallUnionIDLens()

	return F.Flow2(
		F.Curry2(openai.ToolMessage[string]),
		reader.Local[openai.ChatCompletionMessageParamUnion](idLens.Get),
	)
}

// TODO the missing helper
func effectFromReaderResult[C, A any](rdr ReaderResult[C, A]) Effect[C, A] {
	return F.Pipe1(
		rdr,
		reader.Map[C](thunk.FromResult[A]),
	)
}

// MakeToolCall builds a Kleisli arrow that resolves and executes a single tool
// call requested by the model. Given a ToolCaller registry, it looks up the
// tool by name, invokes it with the call's arguments, and turns the outcome —
// success, an unknown tool name, or a failed invocation — into a tool
// ChatCompletionMessageParamUnion carrying the call's ID. The returned Effect
// never fails: every error is captured in the resulting message rather than
// propagated as a Result error.
func MakeToolCall() effect.Kleisli[ToolCaller, openai.ChatCompletionMessageToolCallUnion, openai.ChatCompletionMessageParamUnion] {
	functionLens := MakeChatCompletionMessageToolCallUnionFunctionLens()
	nameLens := MakeChatCompletionMessageFunctionToolCallFunctionNameLens()
	argsLens := MakeChatCompletionMessageFunctionToolCallFunctionArgumentsLens()

	makeSuccess := makeSuccessChatCompletionMessageParamUnion()
	makeError := makeErrorChatCompletionMessageParamUnion()

	functionNameLens := F.Pipe1(
		functionLens,
		L.Compose[openai.ChatCompletionMessageToolCallUnion](nameLens),
	)

	functionArgsLens := F.Pipe1(
		functionLens,
		L.Compose[openai.ChatCompletionMessageToolCallUnion](argsLens),
	)

	toolNotFoundError := F.Pipe4(
		functionNameLens.Get,
		reader.Map[openai.ChatCompletionMessageToolCallUnion](S.Format[string]("Unable to find tool '%s'.")),
		readerresult.Asks,
		readerresult.Chain(makeError),
		effectFromReaderResult,
	)

	generalError := F.Flow3(
		error.Error,
		makeError,
		effectFromReaderResult,
	)

	return F.Pipe1(
		functionNameLens.Get,
		reader.Chain(F.Flow4(
			readeroption.Read[ToolCall, string],
			readeroption.Map[ToolCaller](F.Flow3(
				effect.Local[string](functionArgsLens.Get),
				effect.ChainReaderK(makeSuccess),
				effect.ChainLeft(generalError),
			)),
			readeroption.GetOrElse(F.Pipe1(
				toolNotFoundError,
				reader.Of[ToolCaller],
			)),
			reader.Sequence,
		)),
	)
}

func MakeToolCalls() effect.Kleisli[ToolCaller, []openai.ChatCompletionMessageToolCallUnion, []openai.ChatCompletionMessageParamUnion] {
	return F.Pipe1(
		MakeToolCall(),
		effect.TraverseArray,
	)
}

func handleToolCallsForChoice() effect.Kleisli[ToolCaller, openai.ChatCompletionChoice, Endomorphism[openai.ChatCompletionNewParams]] {
	messageLens := oai.MakeChatCompletionChoiceMessageLens()
	toolCallsLens := oai.MakeChatCompletionMessageToolCallsLens()

	toolCallsFromChoiceOptional := F.Pipe2(
		messageLens,
		L.Compose[openai.ChatCompletionChoice](toolCallsLens),
		LP.Compose[openai.ChatCompletionChoice](P.FromPredicate(A.IsNonEmpty[openai.ChatCompletionMessageToolCallUnion])),
	)

	makeToolCalls := MakeToolCalls()

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

	return F.Pipe4(
		toolCallsFromChoiceOptional.GetOption,
		readeroption.Map[openai.ChatCompletionChoice](makeToolCalls),
		readeroption.ApS(
			F.Flow2(
				A.Prepend[openai.ChatCompletionMessageParamUnion],
				effect.Map[ToolCaller],
			), F.Pipe1(
				messageToParam,
				readeroption.Asks,
			),
		),
		readeroption.Map[openai.ChatCompletionChoice](
			F.Pipe1(
				appendMessages,
				effect.Map[ToolCaller],
			),
		),
		readeroption.GetOrElse(
			F.Pipe2(
				reader.Ask[openai.ChatCompletionNewParams](),
				effect.Of[ToolCaller],
				reader.Of[openai.ChatCompletionChoice],
			),
		),
	)
}

func HandleToolCalls() effect.Kleisli[ToolCaller, *openai.ChatCompletion, Endomorphism[openai.ChatCompletionNewParams]] {
	choicesLens := oai.MakeChatCompletionChoicesRefLens()
	flattenEndo := endomorphism.Monoid[openai.ChatCompletionNewParams]()

	return F.Pipe2(
		handleToolCallsForChoice(),
		effect.TraverseArray,
		reader.ProMap(
			choicesLens.Get,
			F.Pipe2(
				flattenEndo,
				A.Fold[Endomorphism[openai.ChatCompletionNewParams]],
				effect.Map[ToolCaller],
			),
		),
	)
}
