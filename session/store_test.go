package session

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/result"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runTurn runs one turn of a session against the deps and returns the
// session of its final result.
func runTurn(t *testing.T, s Session, deps SessionDeps) Session {
	t.Helper()

	final, err := result.Unwrap(Run()(s)(deps)(t.Context())())
	require.NoError(t, err)

	return pair.Head(final)
}

// saveTask stores a session under an ID.
func saveTask(t *testing.T, store TaskStoreDeps, id TaskID, s Session) Result[F.Void] {
	t.Helper()

	return Save()(pair.MakePair(id, s))(store)(t.Context())()
}

// loadTask loads the session stored under an ID.
func loadTask(t *testing.T, store TaskStoreDeps, id TaskID) Result[Session] {
	t.Helper()

	return Load()(id)(store)(t.Context())()
}

// encoded is the JSON a session is stored as.
func encoded(t *testing.T, s Session) string {
	t.Helper()

	data, err := result.Unwrap(encodeSession()(s))
	require.NoError(t, err)

	return string(data)
}

// TestSaveLoad_RoundTrip asserts that a session after a tool round trip is
// stored as a JSON file named by its ID, in a tasks directory that Save
// creates, and that Load restores the same session.
func TestSaveLoad_RoundTrip(t *testing.T) {
	responses := []openai.ChatCompletion{
		makeToolCallCompletion("chatcmpl-1", "call-1"),
		makeStopCompletion("chatcmpl-2", "It is sunny."),
	}
	s := runTurn(t, MakeSession(makeLoopRequest()), makeSequencedSessionDeps(t, responses, makeWeatherRegistry(), 10))

	dir := filepath.Join(t.TempDir(), "home", "tasks")
	store := MakeDefaultTaskStoreDeps(dir)

	require.Equal(t, result.Of(F.VOID), saveTask(t, store, "task-1", s))
	assert.FileExists(t, filepath.Join(dir, "task-1.json"))

	loaded, err := result.Unwrap(loadTask(t, store, "task-1"))
	require.NoError(t, err)

	assert.JSONEq(t, encoded(t, s), encoded(t, loaded))
	assert.Equal(t, s.iterations, loaded.iterations)
	assert.Equal(t, s.usage.TotalTokens, loaded.usage.TotalTokens)
	require.Len(t, loaded.history, 2)
	assert.Equal(t, "chatcmpl-1", pair.Tail(loaded.history[0]).ID)
	assert.Equal(t, "call-1", pair.Tail(loaded.history[0]).Choices[0].Message.ToolCalls[0].ID)
	assert.Len(t, loaded.current.Messages, len(s.current.Messages))
}

// TestSave_ReplacesTask asserts that saving under an existing ID replaces
// the stored session.
func TestSave_ReplacesTask(t *testing.T) {
	store := MakeDefaultTaskStoreDeps(t.TempDir())

	require.Equal(t, result.Of(F.VOID), saveTask(t, store, "task-1", MakeSession(makeLoopRequest())))
	second := Continue()("and in Paris?")(MakeSession(makeLoopRequest()))
	require.Equal(t, result.Of(F.VOID), saveTask(t, store, "task-1", second))

	loaded, err := result.Unwrap(loadTask(t, store, "task-1"))
	require.NoError(t, err)
	assert.Len(t, loaded.current.Messages, 3)
}

// TestLoad_ContinuesStoredSession asserts that a session saved after its
// first turn, loaded and continued, sends the whole conversation in its
// second turn: the persisted task resumes like the recorded ones.
func TestLoad_ContinuesStoredSession(t *testing.T) {
	responses := []openai.ChatCompletion{
		makeStopCompletion("chatcmpl-1", "It is sunny."),
		makeStopCompletion("chatcmpl-2", "It rains in Paris."),
	}
	deps := makeSequencedSessionDeps(t, responses, makeWeatherRegistry(), 10)
	store := MakeDefaultTaskStoreDeps(t.TempDir())

	require.Equal(t, result.Of(F.VOID), saveTask(t, store, "task-1", runTurn(t, MakeSession(makeLoopRequest()), deps)))

	loaded, err := result.Unwrap(loadTask(t, store, "task-1"))
	require.NoError(t, err)

	got := runTurn(t, Continue()("and in Paris?")(loaded), deps)

	assert.Equal(t, 1, got.iterations)
	assert.Equal(t, int64(60), got.usage.PromptTokens)
	require.Len(t, got.history, 2)

	// the second request: system, user, first answer, second user message
	request := pair.Head(got.history[1])
	require.Len(t, request.Messages, 4)
	assert.Equal(t, "It is sunny.", request.Messages[2].OfAssistant.Content.OfString.Value)
	assert.Equal(t, openai.UserMessage("and in Paris?"), request.Messages[3])
}

// TestLoad_UnknownTask asserts that loading an ID that was never saved fails
// with an error that matches fs.ErrNotExist, so a caller can start a new
// session instead.
func TestLoad_UnknownTask(t *testing.T) {
	store := MakeDefaultTaskStoreDeps(t.TempDir())

	_, err := result.Unwrap(loadTask(t, store, "missing"))

	assert.True(t, errors.Is(err, fs.ErrNotExist), "got %v", err)
}

// TestLoad_BrokenFile asserts that a task file that isn't JSON fails Load.
func TestLoad_BrokenFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "task-1.json"), []byte("not json"), 0o644))

	_, err := result.Unwrap(loadTask(t, MakeDefaultTaskStoreDeps(dir), "task-1"))

	assert.Error(t, err)
}

// failingWrites are file write deps that fail every call, to show that
// nothing is written.
func failingWrites() tools.FileWriteDeps {
	unexpected := errors.New("unexpected write")

	return tools.MakeFileWriteDeps(
		F.Constant1[string](ioresult.Left[string](unexpected)),
		F.Constant1[string](F.Constant1[[]byte](ioresult.Left[[]byte](unexpected))),
	)
}

// failingReads are file read deps that fail every call.
func failingReads() tools.FileDeps {
	return tools.MakeFileDeps(F.Constant1[string](ioresult.Left[[]byte](errors.New("unexpected read"))))
}

// TestStore_InvalidTaskID asserts that IDs that are not plain file names fail
// Save and Load before the file system is touched.
func TestStore_InvalidTaskID(t *testing.T) {
	store := MakeTaskStoreDeps(t.TempDir(), failingReads(), failingWrites())

	for _, id := range []TaskID{"", ".", "..", "../escape", "a/b", filepath.Join("a", "b")} {
		t.Run(id, func(t *testing.T) {
			_, saveErr := result.Unwrap(saveTask(t, store, id, MakeSession(makeLoopRequest())))
			require.Error(t, saveErr)
			assert.Contains(t, saveErr.Error(), "invalid task id")

			_, loadErr := result.Unwrap(loadTask(t, store, id))
			require.Error(t, loadErr)
			assert.Contains(t, loadErr.Error(), "invalid task id")
		})
	}
}

// TestSessionIso_RoundTrip asserts that the DTO keeps every field of a
// session.
func TestSessionIso_RoundTrip(t *testing.T) {
	entry := pair.MakePair(makeLoopRequest(), &openai.ChatCompletion{ID: "chatcmpl-1"})
	s := Session{
		history:    History{entry},
		current:    Continue()("next")(MakeSession(makeLoopRequest())).current,
		usage:      openai.CompletionUsage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12},
		iterations: 3,
	}

	assert.Equal(t, s, SessionIso().ReverseGet(SessionIso().Get(s)))
}
