package replay

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"time"

	B "github.com/IBM/fp-go/v2/bytes"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/iterator/iterresult"
	J "github.com/IBM/fp-go/v2/json"
	N "github.com/IBM/fp-go/v2/number"
	"github.com/IBM/fp-go/v2/result"
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

// readLines yields the lines of r, including their line terminators. A read
// error other than [io.EOF] is yielded as the last element.
//
// This is the leaf around [bufio.Reader]; ReadBytes has no line length limit,
// unlike [bufio.Scanner] (recorded lines exceed 400 KB).
func readLines(r io.Reader) SeqResult[[]byte] {
	return func(yield func(Result[[]byte]) bool) {
		br := bufio.NewReader(r)
		for {
			line, err := br.ReadBytes('\n')
			if len(line) > 0 && !yield(result.Of(line)) {
				return
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					yield(result.Left[[]byte](err))
				}
				return
			}
		}
	}
}

// readFileLines yields the lines of the file at path and closes the file when
// the iteration ends.
//
// Written by hand because iterresult.WithResource and iterresult.Bracket
// release the resource once per element, not once per sequence.
func readFileLines(path string) SeqResult[[]byte] {
	return func(yield func(Result[[]byte]) bool) {
		f, err := os.Open(path)
		if err != nil {
			yield(result.Left[[]byte](err))
			return
		}
		defer f.Close()
		readLines(f)(yield)
	}
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
		readLines,
		decodeRecords(),
	)
}

// ReadLogFile streams the records of the JSON-lines log file at a path. The
// file is opened on each iteration and closed when the iteration ends.
func ReadLogFile() SeqKleisli[string, Record] {
	return F.Flow2(
		readFileLines,
		decodeRecords(),
	)
}
