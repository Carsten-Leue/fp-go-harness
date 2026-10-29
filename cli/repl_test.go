package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Carsten-Leue/fp-go-harness/mode"
	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/replay"
	"github.com/Carsten-Leue/fp-go-harness/session"
	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioref"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	R "github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	opt "github.com/openai/openai-go/v3/option"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	C "github.com/urfave/cli/v3"
)

// replOutput is what a REPL run printed and how it ended.
type replOutput struct {
	err    error
	out    string
	errOut string
}

// runReplWith runs the REPL of the ask mode in a temporary workspace over
// lines, against chat, and returns what it printed.
func runReplWith(t *testing.T, ctx context.Context, chat oai.ChatCompletionDeps, lines <-chan string) replOutput {
	t.Helper()

	var errOut bytes.Buffer
	return runReplTo(t, ctx, chat, lines, &errOut, &errOut)
}

// runReplTo runs the REPL like [runReplWith], with errWriter as the error
// writer of the command; errOut is what it wrote.
func runReplTo(t *testing.T, ctx context.Context, chat oai.ChatCompletionDeps, lines <-chan string, errWriter io.Writer, errOut fmt.Stringer) replOutput {
	t.Helper()
	withHome(t)

	cfg, err := result.Unwrap(decodeConfig()(R.FromEntries(A.From(
		pair.MakePair(KeyMode, mode.AskID),
		pair.MakePair(KeyWorkspace, t.TempDir()),
	))))
	require.NoError(t, err)
	env, err := result.Unwrap(makeModeEnv()(cfg)())
	require.NoError(t, err)

	var out bytes.Buffer
	cmd := &C.Command{Writer: &out, ErrWriter: errWriter}

	_, err = effect.RunSync(F.Pipe1(
		repl()(newReplEnv(cmd, lines, env)),
		effect.Provide[F.Void](chat),
	))(ctx)

	return replOutput{err, out.String(), errOut.String()}
}

// cancelOnWrite is an error writer that cancels a context on its first
// write, as Ctrl-C at the prompt does. The buffer is guarded: the REPL
// writes from its own goroutine.
type cancelOnWrite struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	cancel context.CancelFunc
}

func (w *cancelOnWrite) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	defer w.cancel()
	return w.buf.Write(p)
}

func (w *cancelOnWrite) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// bufferedLines is a channel of lines a test sends to, prefilled with lines.
// The test closes it to end the input.
func bufferedLines(lines ...string) chan string {
	ch := make(chan string, 8)
	for _, line := range lines {
		ch <- line
	}
	return ch
}

// closedLines is the input of lines, then its end.
func closedLines(lines ...string) chan string {
	ch := bufferedLines(lines...)
	close(ch)
	return ch
}

// fakeMessage is the part of a message of a request the fake server reads.
type fakeMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// fakeRequest is the part of a chat completion request the fake server reads.
type fakeRequest struct {
	Messages []fakeMessage `json:"messages"`
}

// lastUserLine is the first line of the last user message of a request: the
// REPL's line, without the environment_details that follow it.
func lastUserLine(req fakeRequest) string {
	last := ""
	for _, m := range req.Messages {
		var text string
		if m.Role == "user" && json.Unmarshal(m.Content, &text) == nil {
			last, _, _ = strings.Cut(text, "\n")
		}
	}
	return last
}

// answerText is the answer of the fake server: the number of messages of
// the request and its last user line.
func answerText(req fakeRequest) string {
	return fmt.Sprintf("n=%d last=%s", len(req.Messages), lastUserLine(req))
}

// toolCallCompletion asks for a tool that does not exist, so the tool result
// is an error message and the turn goes on.
func toolCallCompletion() openai.ChatCompletion {
	return openai.ChatCompletion{
		Object: "chat.completion",
		Choices: []openai.ChatCompletionChoice{{
			FinishReason: "tool_calls",
			Message: openai.ChatCompletionMessage{
				Role: "assistant",
				ToolCalls: []openai.ChatCompletionMessageToolCallUnion{{
					ID:       "call_1",
					Type:     "function",
					Function: openai.ChatCompletionMessageFunctionToolCallFunction{Name: "noop", Arguments: "{}"},
				}},
			},
		}},
	}
}

// fakeServer is a chat completion server whose handler decides from the
// request alone, it keeps no state. The decision gets the request and
// returns the completion, or nil for a failed request.
func fakeServer(t *testing.T, decide func(*nethttp.Request, fakeRequest) *openai.ChatCompletion) oai.ChatCompletionDeps {
	t.Helper()

	server := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		var req fakeRequest
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&req)) {
			return
		}
		completion := decide(r, req)
		if completion == nil {
			nethttp.Error(w, `{"error":{"message":"bad request"}}`, nethttp.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(completion))
	}))
	t.Cleanup(server.Close)

	client := openai.NewClient(opt.WithBaseURL(server.URL), opt.WithAPIKey("test-key"), opt.WithMaxRetries(0))
	return oai.MakeChatCompletionDeps(&client)
}

// answerCompletion is an answer of the fake server for a request.
func answerCompletion(req fakeRequest) *openai.ChatCompletion {
	return makeAnswerCompletion(answerText(req))
}

// TestScanLines_LongLine reads a line longer than the default limit of a
// scanner.
func TestScanLines_LongLine(t *testing.T) {
	long := strings.Repeat("x", 100_000)

	var got []string
	for line := range scanLines(strings.NewReader(long+"\nb\n"), option.None[string]())() {
		got = append(got, line)
	}
	assert.Equal(t, []string{long, "b"}, got)
}

// TestRunCommand_Dispatch runs a command with the flags of the prompt
// command and checks which commands run the REPL, and that the prompt of
// --interactive is the first line, before the lines of the input.
func TestRunCommand_Dispatch(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		interactive bool
		lines       []string
	}{
		{"prompt", []string{"hello", "world"}, false, []string{"hello world", "in"}},
		{"no prompt", nil, true, []string{"in"}},
		{"interactive prompt", []string{"--interactive", "hello"}, true, []string{"hello", "in"}},
		{"interactive", []string{"--interactive"}, true, []string{"in"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var interactive bool
			var lines []string
			cmd := MakePromptCommand()
			cmd.Reader = strings.NewReader("in\n")
			cmd.Action = func(_ context.Context, cmd *C.Command) error {
				interactive = isInteractive()(cmd)
				queue, err := result.Unwrap(scanCommand()(cmd)())
				for line := range queue {
					lines = append(lines, line)
				}
				return err
			}

			require.NoError(t, cmd.Run(t.Context(), append([]string{"fp-go-harness"}, tc.args...)))
			assert.Equal(t, tc.interactive, interactive)
			assert.Equal(t, tc.lines, lines)
		})
	}
}

func TestScanLines(t *testing.T) {
	lines := scanLines(strings.NewReader("a\n\n  \n b\n"), option.Some("first"))()

	var got []string
	for line := range lines {
		got = append(got, line)
	}
	assert.Equal(t, []string{"first", "a", " b"}, got)
}

// TestRepl_TwoLines replays two answers for two lines and ends at the end of
// the input.
func TestRepl_TwoLines(t *testing.T) {
	chat := replay.MakeReplayChatCompletionDeps()(A.From(
		replay.ChatResponse{FinishReason: "stop", Text: "first answer"},
		replay.ChatResponse{FinishReason: "stop", Text: "second answer"},
	))()

	got := runReplWith(t, t.Context(), chat, scanLines(strings.NewReader("a\nb\n"), option.None[string]())())

	require.NoError(t, got.err)
	assert.Equal(t, "first answer\n\n"+statsOf(1)+"\nsecond answer\n\n"+statsOf(1)+"\n", got.out)
	assert.Equal(t, strings.Repeat(promptMarker, 3), got.errOut)
}

// statsOf is the statistics of a session without usage after n iterations
// in its last turn.
func statsOf(n int) string {
	return fmt.Sprintf("[iterations: %d, prompt tokens: 0, completion tokens: 0, total tokens: 0]", n)
}

// TestRepl_SecondRequestCarriesConversation asserts that the second turn
// sends the system prompt, the first line, its answer and the second line.
func TestRepl_SecondRequestCarriesConversation(t *testing.T) {
	chat := fakeServer(t, func(_ *nethttp.Request, req fakeRequest) *openai.ChatCompletion {
		return answerCompletion(req)
	})

	got := runReplWith(t, t.Context(), chat, closedLines("a", "b"))

	require.NoError(t, got.err)
	assert.Contains(t, got.out, "n=2 last=a\n")
	assert.Contains(t, got.out, "n=4 last=b\n")
}

// TestRepl_Steering sends a line while the first request of a turn runs: the
// next request of the same turn carries it after the tool result.
func TestRepl_Steering(t *testing.T) {
	lines := bufferedLines("a")
	chat := fakeServer(t, func(_ *nethttp.Request, req fakeRequest) *openai.ChatCompletion {
		if len(req.Messages) == 2 {
			lines <- "steer"
			close(lines)
			completion := toolCallCompletion()
			return &completion
		}
		return answerCompletion(req)
	})

	got := runReplWith(t, t.Context(), chat, lines)

	require.NoError(t, got.err)
	// system, a, the tool call, its result, steer
	assert.Equal(t, "n=5 last=steer\n\n"+statsOf(2)+"\n", got.out)
}

// TestRepl_LateLine sends a line while the last request of a turn runs: it
// is the line of the next turn.
func TestRepl_LateLine(t *testing.T) {
	lines := bufferedLines("a")
	chat := fakeServer(t, func(_ *nethttp.Request, req fakeRequest) *openai.ChatCompletion {
		switch lastUserLine(req) {
		case "a":
			lines <- "late"
		case "late":
			close(lines)
		}
		return answerCompletion(req)
	})

	got := runReplWith(t, t.Context(), chat, lines)

	require.NoError(t, got.err)
	assert.Equal(t, "n=2 last=a\n\n"+statsOf(1)+"\nn=4 last=late\n\n"+statsOf(1)+"\n", got.out)
}

// TestRepl_RequestError fails the request of the second line: the failure is
// reported on the standard error and the third line continues the partial
// session, which keeps the second line.
func TestRepl_RequestError(t *testing.T) {
	chat := fakeServer(t, func(_ *nethttp.Request, req fakeRequest) *openai.ChatCompletion {
		if lastUserLine(req) == "b" {
			return nil
		}
		return answerCompletion(req)
	})

	got := runReplWith(t, t.Context(), chat, closedLines("a", "b", "c"))

	require.NoError(t, got.err)
	assert.Contains(t, got.out, "n=2 last=a\n")
	assert.Contains(t, got.out, "n=5 last=c\n")
	assert.Contains(t, got.errOut, "400 Bad Request")
}

// TestRepl_CancelInTurn cancels the context while a request blocks: the turn
// is reported and the REPL ends with the cancellation.
func TestRepl_CancelInTurn(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	chat := fakeServer(t, func(r *nethttp.Request, _ fakeRequest) *openai.ChatCompletion {
		cancel()
		<-r.Context().Done()
		return nil
	})

	got := runReplWith(t, ctx, chat, bufferedLines("a", "b"))

	assert.True(t, session.IsCancelled()(got.err), "%v", got.err)
	assert.Empty(t, got.out)
	assert.Contains(t, got.errOut, context.Canceled.Error())
	// the REPL ended after the first turn, it did not prompt again
	assert.Equal(t, 1, strings.Count(got.errOut, promptMarker))
}

// TestRepl_CancelAtPrompt cancels the context once the REPL prints its
// prompt, while it waits for a line: it ends without an error.
func TestRepl_CancelAtPrompt(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	errWriter := &cancelOnWrite{cancel: cancel}

	got := runReplTo(t, ctx, fakeServer(t, func(*nethttp.Request, fakeRequest) *openai.ChatCompletion { return nil }), make(chan string), errWriter, errWriter)

	require.NoError(t, got.err)
	assert.Empty(t, got.out)
	assert.Equal(t, promptMarker, got.errOut)
}

// TestRepl_CancelStopsBackgroundProcesses runs a REPL turn of the agent mode:
// the first answer starts a background command, the request after it cancels
// the context and blocks. The REPL ends with the cancellation and the
// background process leaves the table.
func TestRepl_CancelStopsBackgroundProcesses(t *testing.T) {
	if testing.Short() {
		t.Skip("starts shell processes")
	}
	withHome(t)

	cfg, err := result.Unwrap(decodeConfig()(R.FromEntries(A.From(
		pair.MakePair(KeyMode, mode.AgentID),
		pair.MakePair(KeyWorkspace, t.TempDir()),
	))))
	require.NoError(t, err)
	env, err := result.Unwrap(makeModeEnv()(cfg)())
	require.NoError(t, err)
	table := MakemodeEnvDepsLens().Get(env).GetProcessTable()
	t.Cleanup(func() { tools.StopProcesses()(table)() })

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	start := startBackgroundCompletion(t, "sleep 30; exit 0")
	chat := fakeServer(t, func(r *nethttp.Request, req fakeRequest) *openai.ChatCompletion {
		// the request after the tool call carries its result
		if A.Any(func(m fakeMessage) bool { return m.Role == "tool" })(req.Messages) {
			assert.Len(t, ioref.Read(table)(), 1)
			cancel()
			<-r.Context().Done()
			return nil
		}
		return &start
	})

	var out, errOut bytes.Buffer
	cmd := &C.Command{Writer: &out, ErrWriter: &errOut}
	_, err = effect.RunSync(F.Pipe1(
		repl()(newReplEnv(cmd, bufferedLines("start the server"), env)),
		effect.Provide[F.Void](chat),
	))(ctx)

	assert.True(t, session.IsCancelled()(err), "%v", err)
	assert.Eventually(t, func() bool { return len(ioref.Read(table)()) == 0 }, 20*time.Second, 50*time.Millisecond)
}

// TestRepl_EOF ends at once without input.
func TestRepl_EOF(t *testing.T) {
	got := runReplWith(t, t.Context(), fakeServer(t, func(*nethttp.Request, fakeRequest) *openai.ChatCompletion { return nil }), scanLines(strings.NewReader(""), option.None[string]())())

	require.NoError(t, got.err)
	assert.Empty(t, got.out)
	assert.Equal(t, promptMarker, got.errOut)
}
