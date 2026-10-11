package main

import (
	"fmt"

	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/reader"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
)

const (
	// askMode answers questions with the read-only tools.
	askMode = "ask"
	// agentMode carries out tasks with every tool, including those that
	// change the workspace.
	agentMode = "agent"
)

// mode is what a CLI mode sets: the request with its system prompt and the
// tools the model may call. Its tools field has a function type, so the
// lens generator cannot derive lenses for it; it is read directly at the
// boundary.
type mode struct {
	request openai.ChatCompletionNewParams
	tools   reader.Reader[tools.ToolsDeps, tools.ToolRegistry]
}

// modes are the CLI modes by name. Ask mode gets only the read-only tools,
// read from the read-only part of the deps.
func modes() map[string]mode {
	return map[string]mode{
		askMode: {
			request: oai.ForAskMode(),
			tools:   reader.Local[tools.ToolRegistry](tools.AsGrepDeps[tools.ToolsDeps])(tools.MakeReadTools()),
		},
		agentMode: {
			request: oai.ForAgentMode(),
			tools:   tools.MakeTools(),
		},
	}
}

// unknownModeError is a leaf: one formatter over the mode name.
func unknownModeError(name string) func() error {
	return func() error {
		return fmt.Errorf("unknown mode %q, use %q or %q", name, askMode, agentMode)
	}
}

// lookupMode finds a CLI mode by name. The lookup and the error for an
// unknown name both read the name.
func lookupMode() result.Kleisli[string, mode] {
	return F.Pipe1(
		F.Flow2(unknownModeError, result.FromOption[mode]),
		reader.Ap[result.Result[mode]](F.Flip(R.Lookup[mode, string])(modes())),
	)
}
