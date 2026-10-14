// Package prompt assembles the system prompt from tagged sections.
//
// A [Section] pairs a tag with its body and renders as
//
//	<tag>
//	body
//	</tag>
//
// [RenderSections] folds rendered sections into one prompt, separated by a
// blank line, as in the recorded system prompts.
package prompt
