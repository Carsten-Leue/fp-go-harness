// Package replay reads recorded session logs (JSON lines), redacts the
// sensitive values in them and replays recorded tasks in tests:
// [MakeReplayChatCompletionDeps] answers each chat completion request with the
// next recorded response, without network access. [SystemPrompts] yields the
// system prompts of the recorded requests, for comparison with an assembled
// one.
package replay

//go:generate go tool gen lens
//go:generate go tool gen lens --type ChatCompletionMessageParamUnion,ChatCompletionSystemMessageParam,ChatCompletionSystemMessageParamContentUnion,ChatCompletionContentPartTextParam --filename gen_openai.go github.com/openai/openai-go/v3
