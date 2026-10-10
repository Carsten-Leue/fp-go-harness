// Package replay reads recorded Bob session logs (JSON lines), redacts the
// sensitive values in them and replays recorded tasks in tests:
// [MakeReplayChatCompletionDeps] answers each chat completion request with the
// next recorded response, without network access.
package replay

//go:generate go tool gen lens
