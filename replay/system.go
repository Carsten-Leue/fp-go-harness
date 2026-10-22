package replay

import (
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/iterator/iter"
	"github.com/IBM/fp-go/v2/iterator/iterresult"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/reader"
	S "github.com/IBM/fp-go/v2/string"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
)

// partSeparator joins the text parts of a system message, so that a section
// that starts a part starts a line.
const partSeparator = "\n"

// stringContent is the text of a system message given as a string, empty
// otherwise.
func stringContent() func(openai.ChatCompletionSystemMessageParamContentUnion) string {
	ofStringLens := MakeChatCompletionSystemMessageParamContentUnionOfStringLens()

	return F.Flow2(
		ofStringLens.Get,
		F.Bind2nd(param.Opt[string].Or, ""),
	)
}

// partsContent is the text of a system message given as text parts, joined
// with [partSeparator]; empty otherwise.
func partsContent() func(openai.ChatCompletionSystemMessageParamContentUnion) string {
	partsLens := MakeChatCompletionSystemMessageParamContentUnionOfArrayOfContentPartsLens()
	textLens := MakeChatCompletionContentPartTextParamTextLens()

	return F.Flow2(
		partsLens.Get,
		A.FoldMap[openai.ChatCompletionContentPartTextParam](S.IntersperseMonoid(partSeparator))(textLens.Get),
	)
}

// contentText is the text of a system message. Only one of its two forms is
// set, so their concatenation is the text of the one that is.
func contentText() func(openai.ChatCompletionSystemMessageParamContentUnion) string {
	return reader.ApplicativeMonoid[openai.ChatCompletionSystemMessageParamContentUnion](S.Monoid).
		Concat(stringContent(), partsContent())
}

// SystemPrompt is the system prompt of a request: the text of its first
// message if that is a non-empty system message.
func SystemPrompt() option.Kleisli[openai.ChatCompletionNewParams, string] {
	messagesLens := oai.MakeChatCompletionNewParamsMessagesLens()
	ofSystemLens := MakeChatCompletionMessageParamUnionOfSystemLensO()
	contentLens := MakeChatCompletionSystemMessageParamContentLens()

	return F.Flow6(
		messagesLens.Get,
		A.Head[openai.ChatCompletionMessageParamUnion],
		option.Chain(ofSystemLens.Get),
		option.Map(F.Flow2(F.Deref[openai.ChatCompletionSystemMessageParam], contentLens.Get)),
		option.Map(contentText()),
		option.Chain(option.FromPredicate(S.IsNonEmpty)),
	)
}

// SystemPrompts picks the recorded /chat/completions requests out of a record
// stream and yields their [SystemPrompt]s, one per request; requests without
// one are skipped.
//
// Example:
//
//	prompts := SystemPrompts()(ReadLogFile()(path))
func SystemPrompts() iterresult.Operator[Record, string] {
	return F.Flow2(
		Requests(),
		iterresult.Chain(F.Flow2(
			SystemPrompt(),
			option.Fold(iter.Empty[Result[string]], iterresult.Of[string]),
		)),
	)
}
