package replay

import (
	"path/filepath"
	"testing"

	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/iterator/iterresult"
	J "github.com/IBM/fp-go/v2/json"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/result"
	openai "github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decodeRequest(t *testing.T, body string) openai.ChatCompletionNewParams {
	t.Helper()

	params, err := result.Unwrap(J.Unmarshal[openai.ChatCompletionNewParams]([]byte(body)))
	require.NoError(t, err)
	return params
}

func TestSystemPromptOfString(t *testing.T) {
	assert.Equal(t, option.Some("<role_definition>"), SystemPrompt()(decodeRequest(t, recordedRequest)))
}

func TestSystemPromptOfParts(t *testing.T) {
	request := decodeRequest(t, `{"model":"m","messages":[{"role":"system","content":[`+
		`{"type":"text","text":"<tool_use>\nOne tool.\n</tool_use>"},`+
		`{"type":"text","text":"<base_rules>\nStay.\n</base_rules>"}]}]}`)

	assert.Equal(t,
		option.Some("<tool_use>\nOne tool.\n</tool_use>\n<base_rules>\nStay.\n</base_rules>"),
		SystemPrompt()(request))
}

func TestSystemPromptNone(t *testing.T) {
	assert.Equal(t, option.None[string](), SystemPrompt()(decodeRequest(t, `{"model":"m","messages":[{"role":"user","content":"hi"}]}`)))
	assert.Equal(t, option.None[string](), SystemPrompt()(decodeRequest(t, `{"model":"m","messages":[{"role":"system","content":""}]}`)))
	assert.Equal(t, option.None[string](), SystemPrompt()(decodeRequest(t, `{"model":"m","messages":[]}`)))
}

func TestSystemPrompts(t *testing.T) {
	prompts := F.Pipe3(
		filepath.Join("testdata", "synthetic-task.log"),
		ReadLogFile(),
		SystemPrompts(),
		iterresult.Collect[string],
	)()

	assert.Equal(t, result.Of(A.Replicate(2, "You are a helpful assistant.")), prompts)
}
