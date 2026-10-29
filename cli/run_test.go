package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Carsten-Leue/fp-go-harness/mode"
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/session"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/ioref"
	"github.com/IBM/fp-go/v2/pair"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	C "github.com/urfave/cli/v3"
)

// makeAnswerCompletion is a completion whose first choice answers with
// content.
func makeAnswerCompletion(content string) *openai.ChatCompletion {
	return &openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{{
			FinishReason: "stop",
			Message:      openai.ChatCompletionMessage{Role: "assistant", Content: content},
		}},
	}
}

// makeTurnSession builds a session with the given history and request
// messages.
func makeTurnSession(history session.History, messages ...openai.ChatCompletionMessageParamUnion) session.Session {
	historyLens := session.MakeSessionhistoryLens()

	return historyLens.Set(history)(session.MakeSession(openai.ChatCompletionNewParams{Messages: messages}))
}

// reportTo reports a turn on a command whose output goes to two buffers, and
// returns the result, the standard output and the standard error.
func reportTo(t *testing.T, turn session.TurnResult) (Result[int], string, string) {
	t.Helper()

	var out, errOut bytes.Buffer
	cmd := &C.Command{Writer: &out, ErrWriter: &errOut}

	return reportTurn()(cmd)(turn)(), out.String(), errOut.String()
}

const emptyStats = "[iterations: 0, prompt tokens: 0, completion tokens: 0, total tokens: 0]"

func TestReportTurn_Answer(t *testing.T) {
	turn := pair.MakePair(makeTurnSession(nil), result.Of(makeAnswerCompletion("It is sunny.")))

	res, out, errOut := reportTo(t, turn)

	assert.True(t, result.IsRight(res))
	assert.Equal(t, "It is sunny.\n\n"+emptyStats+"\n", out)
	assert.Empty(t, errOut)
}

// TestReportTurn_Failure asserts the one rule for every failed turn: the
// error, the last assistant text of the session and the statistics go to the
// standard error, and the report fails with the error.
func TestReportTurn_Failure(t *testing.T) {
	toolCall := openai.ChatCompletionMessage{Role: "assistant", ToolCalls: []openai.ChatCompletionMessageToolCallUnion{{ID: "call_1", Type: "function"}}}
	history := session.History{pair.MakePair(openai.ChatCompletionNewParams{}, makeAnswerCompletion("From the history."))}
	maxErr := &session.MaxIterationsError{MaxIterations: 2}

	for name, tc := range map[string]struct {
		session session.Session
		err     error
		want    string
	}{
		"cancelled during a tool": {
			makeTurnSession(nil, openai.UserMessage("hi"), openai.AssistantMessage("Let me look."), toolCall.ToParam(), openai.ToolMessage("context canceled", "call_1")),
			context.Canceled,
			"context canceled\n\nLet me look.\n\n" + emptyStats + "\n",
		},
		"text only in the history": {
			makeTurnSession(history, openai.UserMessage("hi"), toolCall.ToParam()),
			maxErr,
			maxErr.Error() + "\n\nFrom the history.\n\n" + emptyStats + "\n",
		},
		"request wins over history": {
			makeTurnSession(history, openai.AssistantMessage("From the request.")),
			errors.New("boom"),
			"boom\n\nFrom the request.\n\n" + emptyStats + "\n",
		},
		"no text": {
			makeTurnSession(nil, openai.UserMessage("hi")),
			errors.New("boom"),
			"boom\n\n" + emptyStats + "\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			res, out, errOut := reportTo(t, pair.MakePair(tc.session, result.Left[*openai.ChatCompletion](tc.err)))

			assert.Equal(t, result.Left[int](tc.err), res)
			assert.Empty(t, out)
			assert.Equal(t, tc.want, errOut)
		})
	}
}

func TestIsCancelledTurn(t *testing.T) {
	s := makeTurnSession(nil)

	assert.True(t, isCancelledTurn()(pair.MakePair(s, result.Left[*openai.ChatCompletion](context.Canceled))))
	assert.False(t, isCancelledTurn()(pair.MakePair(s, result.Left[*openai.ChatCompletion](errors.New("boom")))))
	assert.False(t, isCancelledTurn()(pair.MakePair(s, result.Of(makeAnswerCompletion("done")))))
}

// TestStopWhenCancelled starts a real background process and asserts that
// only a cancelled turn stops it.
func TestStopWhenCancelled(t *testing.T) {
	if testing.Short() {
		t.Skip("starts shell processes")
	}

	table := ioref.MakeIORef(tools.ProcessTable{})()
	t.Cleanup(func() { tools.StopProcesses()(table)() })

	started, err := result.Unwrap(tools.StartShell(tools.DefaultShell(), t.TempDir())(table)(tools.MakeProcess("sleep 30; exit 0", t.TempDir()))(t.Context())())
	require.NoError(t, err)

	s := makeTurnSession(nil)
	stop := stopWhenCancelled()(table)

	stop(pair.MakePair(s, result.Left[*openai.ChatCompletion](errors.New("boom"))))()
	stop(pair.MakePair(s, result.Of(makeAnswerCompletion("done"))))()
	assert.Equal(t, A.Of(started.Pid), R.Keys(ioref.Read(table)()))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	stop(pair.MakePair(s, result.Left[*openai.ChatCompletion](context.Cause(ctx))))()
	assert.Eventually(t, func() bool { return len(ioref.Read(table)()) == 0 }, 20*time.Second, 50*time.Millisecond)
}

func makeTestToolsDeps(t *testing.T) tools.ToolsDeps {
	t.Helper()

	root := t.TempDir()
	return tools.MakeDefaultToolsDeps(root, filepath.Join(root, "logs"))()
}

// defaultConfig decodes the configuration without any variables.
func defaultConfig(t *testing.T) Config {
	t.Helper()

	cfg, err := result.Unwrap(decodeConfig()(R.Empty[string, string]()))
	require.NoError(t, err)
	return cfg
}

func TestMakeRegistry_ToolsOfTheMode(t *testing.T) {
	m, err := result.Unwrap(mode.Lookup()(mode.MakeModeTableDeps(mode.Modes()))(mode.AskID))
	require.NoError(t, err)

	assert.ElementsMatch(t,
		[]string{tools.ReadFileName, tools.ListFilesName, tools.GlobName, tools.GrepName},
		R.Keys(makeRegistry()(newModeEnv(defaultConfig(t), mode.MakeDefaultModeDeps(makeTestToolsDeps(t), mode.Modes()), m))))
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

// TestModeDirs asserts the precedence of the custom mode directories: the
// home directory's first, so the workspace's override them.
func TestModeDirs(t *testing.T) {
	root := filepath.Join("work", "space")
	home := filepath.Join("home", "user")

	assert.Equal(t, []string{
		filepath.Join(home, mode.ModesDir),
		filepath.Join(root, mode.ModesDir),
	}, modeDirs()(root)(home))
}
