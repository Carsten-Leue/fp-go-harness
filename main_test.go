package main

import (
	"path/filepath"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/mode"
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestToolsDeps(t *testing.T) tools.ToolsDeps {
	t.Helper()

	root := t.TempDir()
	return tools.MakeDefaultToolsDeps(root, filepath.Join(root, "logs"))()
}

func TestMakeRegistry_ToolsOfTheMode(t *testing.T) {
	m, err := result.Unwrap(mode.Lookup()(mode.AskID))
	require.NoError(t, err)

	assert.ElementsMatch(t,
		[]string{tools.ReadFileName, tools.ListFilesName, tools.GlobName, tools.GrepName},
		R.Keys(makeRegistry(m)(makeTestToolsDeps(t))))
}

func TestWithPrompt(t *testing.T) {
	system := openai.ChatCompletionNewParams{Messages: []openai.ChatCompletionMessageParamUnion{openai.SystemMessage("system")}}

	request := withPrompt()("hello")(system)

	assert.Equal(t, oai.DeepSeekModelFlash, request.Model)
	require.Len(t, request.Messages, 2)
	assert.Equal(t, "system", request.Messages[0].OfSystem.Content.OfString.Value)
	assert.Equal(t, "hello", request.Messages[1].OfUser.Content.OfString.Value)
	assert.Len(t, system.Messages, 1)
}
