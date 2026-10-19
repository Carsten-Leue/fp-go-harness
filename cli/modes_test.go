package cli

import (
	"path/filepath"
	"testing"

	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestToolsDeps(t *testing.T) tools.ToolsDeps {
	t.Helper()

	root := t.TempDir()
	return tools.MakeDefaultToolsDeps(root, filepath.Join(root, "logs"))()
}

func TestLookupMode_Ask(t *testing.T) {
	m, err := result.Unwrap(lookupMode()(askMode))
	require.NoError(t, err)

	assert.Equal(t, oai.ForAskMode(), m.request)
	// only the read-only tools
	assert.ElementsMatch(t,
		[]string{tools.ReadFileName, tools.ListFilesName, tools.GlobName, tools.GrepName},
		R.Keys(m.tools(makeTestToolsDeps(t))))
}

func TestLookupMode_Agent(t *testing.T) {
	m, err := result.Unwrap(lookupMode()(agentMode))
	require.NoError(t, err)

	assert.Equal(t, oai.ForAgentMode(), m.request)
	assert.ElementsMatch(t,
		[]string{
			tools.ReadFileName, tools.ListFilesName, tools.GlobName, tools.GrepName,
			tools.WriteFileName, tools.SearchAndReplaceName, tools.ApplyDiffName, tools.InsertContentName,
			tools.ExecuteCommandName,
		},
		R.Keys(m.tools(makeTestToolsDeps(t))))
}

func TestLookupMode_Unknown(t *testing.T) {
	_, err := result.Unwrap(lookupMode()("plan"))
	assert.EqualError(t, err, `unknown mode "plan", use "ask" or "agent"`)
}

func TestMakeRequest_ToolsOfTheMode(t *testing.T) {
	m, err := result.Unwrap(lookupMode()(askMode))
	require.NoError(t, err)

	request := makeRequest(m.request, m.tools(makeTestToolsDeps(t)))("hello")

	assert.Len(t, request.Tools, 4)
	// the system prompt of the mode, then the prompt
	assert.Len(t, request.Messages, 2)
	assert.Equal(t, oai.DeepSeekModelFlash, request.Model)
}
