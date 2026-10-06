package replay

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	B "github.com/IBM/fp-go/v2/bytes"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/ioresult/file"
	"github.com/IBM/fp-go/v2/iterator/iterresult"
	J "github.com/IBM/fp-go/v2/json"
	N "github.com/IBM/fp-go/v2/number"
)

// Record is one line of a recorded Bob log.
//
// Data is kept undecoded because its shape depends on Module and Msg.
//
// fp-go:Lens
type Record struct {
	Ts     time.Time       `json:"ts"`
	Level  string          `json:"level"`
	Module string          `json:"module"`
	Msg    string          `json:"msg"`
	TaskID string          `json:"taskId,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// splitLines splits each content into its lines, including their line
// terminators. There is no line length limit (recorded lines exceed 400 KB).
func splitLines() iterresult.Operator[[]byte, []byte] {
	return iterresult.ChainSeqK(bytes.Lines)
}

// readLines reads r completely into memory and yields its lines. A read error
// is the only element.
func readLines() SeqKleisli[io.Reader, []byte] {
	return F.Flow3(
		ioresult.Eitherize1(io.ReadAll),
		iterresult.FromIOResult[[]byte],
		splitLines(),
	)
}

// readFileLines reads the file at a path completely into memory and yields its
// lines. A read error is the only element.
func readFileLines() SeqKleisli[string, []byte] {
	return F.Flow3(
		file.ReadFile,
		iterresult.FromIOResult[[]byte],
		splitLines(),
	)
}

// decodeRecords turns a sequence of JSON lines into records. Blank lines are
// skipped, a line that is not a valid record becomes an error element.
func decodeRecords() iterresult.Operator[[]byte, Record] {
	return F.Flow3(
		iterresult.Map(bytes.TrimSpace),
		iterresult.Filter(F.Flow2(B.Size, N.MoreThan(0))),
		iterresult.ChainEitherK(J.Unmarshal[Record]),
	)
}

// ReadRecords streams the records of a JSON-lines log read from an [io.Reader].
func ReadRecords() SeqKleisli[io.Reader, Record] {
	return F.Flow2(
		readLines(),
		decodeRecords(),
	)
}

// ReadLogFile streams the records of the JSON-lines log file at a path. The
// file is read on each iteration.
func ReadLogFile() SeqKleisli[string, Record] {
	return F.Flow2(
		readFileLines(),
		decodeRecords(),
	)
}
