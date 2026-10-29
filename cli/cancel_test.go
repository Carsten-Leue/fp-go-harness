package cli

import (
	"context"
	"encoding/json"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Carsten-Leue/fp-go-harness/env"
	"github.com/Carsten-Leue/fp-go-harness/http"
	"github.com/Carsten-Leue/fp-go-harness/mode"
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/session"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioref"
	"github.com/IBM/fp-go/v2/pair"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redirect is a transport that sends every request to target, so the
// DeepSeek client talks to a fake server.
type redirect struct {
	target *url.URL
}

func (r redirect) RoundTrip(req *nethttp.Request) (*nethttp.Response, error) {
	out := req.Clone(req.Context())
	out.URL.Scheme = r.target.Scheme
	out.URL.Host = r.target.Host
	return nethttp.DefaultTransport.RoundTrip(out)
}

// startBackgroundCompletion is a completion that asks to start command in
// the background.
func startBackgroundCompletion(t *testing.T, command string) openai.ChatCompletion {
	t.Helper()

	args, err := json.Marshal(map[string]any{"command": command, "background": true})
	require.NoError(t, err)

	return openai.ChatCompletion{
		ID:     "chatcmpl-1",
		Object: "chat.completion",
		Choices: []openai.ChatCompletionChoice{{
			FinishReason: "tool_calls",
			Message: openai.ChatCompletionMessage{
				Role: "assistant",
				ToolCalls: []openai.ChatCompletionMessageToolCallUnion{{
					ID:       "call_1",
					Type:     "function",
					Function: openai.ChatCompletionMessageFunctionToolCallFunction{Name: tools.ExecuteCommandName, Arguments: string(args)},
				}},
			},
		}},
	}
}

// TestAsk_CancelStopsBackgroundProcesses runs a turn of the agent mode
// against a fake DeepSeek server: the first answer starts a background
// command, the second request blocks until the user cancels. The turn ends
// with the cancellation and the session, and the background process is
// stopped and leaves the table.
func TestAsk_CancelStopsBackgroundProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("starts shell processes")
	}
	withHome(t)

	waiting := make(chan struct{}, 1)
	start := startBackgroundCompletion(t, "sleep 30; exit 0")

	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		body, err := io.ReadAll(r.Body)
		if !assert.NoError(t, err) {
			return
		}
		// the request after the tool call carries its result
		if strings.Contains(string(body), `"role":"tool"`) {
			waiting <- struct{}{}
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(start))
	}))
	t.Cleanup(server.Close)

	target, err := url.Parse(server.URL)
	require.NoError(t, err)

	cfg, err := result.Unwrap(decodeConfig()(R.FromEntries(A.From(
		pair.MakePair(KeyMode, mode.AgentID),
		pair.MakePair(KeyWorkspace, t.TempDir()),
	))))
	require.NoError(t, err)

	deps, err := result.Unwrap(makeModeDeps()(cfg)())
	require.NoError(t, err)
	table := deps.GetProcessTable()
	t.Cleanup(func() { tools.StopProcesses()(table)() })

	run, err := result.Unwrap(result.Map(ask())(modeEnvOf()(cfg)(deps)))
	require.NoError(t, err)

	var deepSeek oai.DeepSeekDeps = &deepSeekDeps{
		http.MakeHttpDeps(&nethttp.Client{Transport: redirect{target}}),
		env.MakeVarsEnvironmentDeps()(R.Singleton("DEEPSEEK_API_KEY", "test-key")),
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		<-waiting
		// the background process was started by the tool call before
		assert.Len(t, ioref.Read(table)(), 1)
		cancel()
	}()

	turn, err := effect.RunSync(F.Pipe1(
		run("start the server"),
		effect.Provide[session.TurnResult](deepSeek),
	))(ctx)
	require.NoError(t, err)

	assert.True(t, session.IsCancelled()(result.ToError(pair.Tail(turn))))
	historyLens := session.MakeSessionhistoryLens()
	assert.Len(t, historyLens.Get(pair.Head(turn)), 1)
	assert.Eventually(t, func() bool { return len(ioref.Read(table)()) == 0 }, 20*time.Second, 50*time.Millisecond)
}
