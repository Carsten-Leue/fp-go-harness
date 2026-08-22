package tools

import (
	B "github.com/IBM/fp-go/v2/bytes"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	J "github.com/IBM/fp-go/v2/json"
	"github.com/IBM/fp-go/v2/lazy"
	L "github.com/IBM/fp-go/v2/optics/lens"
	"github.com/IBM/fp-go/v2/option"
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
			readerresult.Asks[openai.ChatCompletionMessageToolCallUnion],
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

	toolNotFoundError := F.Pipe5(
		functionNameLens.Get,
		reader.Map[openai.ChatCompletionMessageToolCallUnion](S.Format[string]("Unable to find tool '%s'.")),
		readerresult.Asks,
		readerresult.Chain(makeError),
		effectFromReaderResult[openai.ChatCompletionMessageToolCallUnion, openai.ChatCompletionMessageParamUnion],
		lazy.Of,
	)

	generalError := F.Flow3(
		error.Error,
		makeError,
		effectFromReaderResult[openai.ChatCompletionMessageToolCallUnion, openai.ChatCompletionMessageParamUnion],
	)

	return F.Pipe1(
		functionNameLens.Get,
		reader.Chain(F.Flow3(
			readeroption.Read[ToolCall, string],
			reader.Map[ToolCaller](F.Flow2(
				option.Map(F.Flow3(
					effect.Local[string](functionArgsLens.Get),
					effect.ChainReaderK(makeSuccess),
					effect.ChainLeft(generalError),
				)),
				option.GetOrElse(toolNotFoundError),
			)),
			reader.Sequence,
		)),
	)
}
