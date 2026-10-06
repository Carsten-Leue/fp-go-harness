package tools

//go:generate go tool gen lens --type ChatCompletionMessageToolCallUnion,ChatCompletionMessageFunctionToolCallFunction github.com/openai/openai-go/v3
//go:generate go tool gen lens --type FunctionDefinitionParam --filename gen_shared.go github.com/openai/openai-go/v3/shared
