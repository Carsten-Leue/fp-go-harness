package replay

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/iterator/iterresult"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// envRecordingsDir names the directory with the recorded Bob logs PLAN.md was
// derived from. They are not part of the repository; tests that need them are
// skipped when it is not set.
const envRecordingsDir = "REPLAY_RECORDINGS_DIR"

func recordingsDir(t *testing.T) string {
	dir, ok := os.LookupEnv(envRecordingsDir)
	if !ok {
		t.Skipf("%s not set", envRecordingsDir)
	}
	return dir
}

const sampleLog = `{"ts":"2026-09-24T13:23:12.408Z","level":"info","module":"Extension","msg":"Activating extension"}

{"ts":"2026-10-06T08:21:40.000Z","level":"debug","module":"ChatManager","msg":"Starting agent loop","taskId":"t-1","data":{"iteration":1}}
`

func newReader(s string) io.Reader {
	return strings.NewReader(s)
}

func collect(seq SeqResult[Record]) Result[[]Record] {
	return iterresult.Collect(seq)()
}

func TestReadRecords(t *testing.T) {
	records := F.Pipe2(
		newReader(sampleLog),
		ReadRecords(),
		collect,
	)

	assert.Equal(t, result.Of([]Record{
		{
			Ts:     time.Date(2026, 9, 24, 13, 23, 12, 408_000_000, time.UTC),
			Level:  "info",
			Module: "Extension",
			Msg:    "Activating extension",
		},
		{
			Ts:     time.Date(2026, 10, 6, 8, 21, 40, 0, time.UTC),
			Level:  "debug",
			Module: "ChatManager",
			Msg:    "Starting agent loop",
			TaskID: "t-1",
			Data:   json.RawMessage(`{"iteration":1}`),
		},
	}), records)
}

func TestReadRecordsWithoutTrailingNewline(t *testing.T) {
	records := F.Pipe3(
		newReader(`{"level":"info","msg":"a"}`+"\r\n"+`{"level":"warn","msg":"b"}`),
		ReadRecords(),
		collect,
		result.Map(A.Size[Record]),
	)

	assert.Equal(t, result.Of(2), records)
}

func TestReadRecordsInvalidLine(t *testing.T) {
	const log = `{"msg":"ok"}` + "\nnot json\n" + `{"msg":"after"}` + "\n"

	var oks, errs int
	for r := range ReadRecords()(newReader(log)) {
		if result.IsLeft(r) {
			errs++
		} else {
			oks++
		}
	}
	// the invalid line is an error element, the stream continues after it
	assert.Equal(t, 2, oks)
	assert.Equal(t, 1, errs)
	assert.True(t, result.IsLeft(collect(ReadRecords()(newReader(log)))))
}

func TestReadRecordsLongLine(t *testing.T) {
	long := strings.Repeat("x", 1<<20)

	msgs := F.Pipe3(
		newReader(`{"msg":"`+long+`"}`+"\n"),
		ReadRecords(),
		collect,
		result.Map(A.Map(MakeRecordMsgLens().Get)),
	)

	assert.Equal(t, result.Of([]string{long}), msgs)
}

func TestReadRecordsStopsEarly(t *testing.T) {
	var msgs []string
	for r := range ReadRecords()(newReader(sampleLog)) {
		rec, err := result.Unwrap(r)
		require.NoError(t, err)
		msgs = append(msgs, rec.Msg)
		break
	}

	assert.Equal(t, []string{"Activating extension"}, msgs)
}

func TestReadLogFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.log")
	require.NoError(t, os.WriteFile(path, []byte(sampleLog), 0o600))

	modules := F.Pipe3(
		path,
		ReadLogFile(),
		collect,
		result.Map(A.Map(MakeRecordModuleLens().Get)),
	)

	assert.Equal(t, result.Of([]string{"Extension", "ChatManager"}), modules)
}

func TestReadLogFileMissing(t *testing.T) {
	records := F.Pipe2(
		filepath.Join(t.TempDir(), "missing.log"),
		ReadLogFile(),
		collect,
	)

	assert.True(t, result.IsLeft(records))
}

func TestReadRecordedLog(t *testing.T) {
	path := filepath.Join(recordingsDir(t), "bob-bob4z-shell-proxy-p98004-20260924T152312.log")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("recording not available: %v", err)
	}

	records, err := result.Unwrap(F.Pipe2(path, ReadLogFile(), collect))
	require.NoError(t, err)

	assert.Len(t, records, 190)
	assert.Equal(t, "RuntimeLogging", records[0].Module)
	assert.Equal(t, "File logging enabled", records[0].Msg)
	assert.Equal(t, "2026-09-24T13:23:12.406Z", records[0].Ts.Format("2006-01-02T15:04:05.000Z07:00"))
}
