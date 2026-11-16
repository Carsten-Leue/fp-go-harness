package skill

import (
	"io/fs"

	"github.com/Carsten-Leue/fp-go-harness/prompt"
	"github.com/IBM/fp-go/v2/endomorphism"
	"github.com/IBM/fp-go/v2/io"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	"github.com/IBM/fp-go/v2/reader"
	"github.com/IBM/fp-go/v2/record"
	"github.com/IBM/fp-go/v2/result"
)

type (
	Endomorphism[A any]         = endomorphism.Endomorphism[A]
	IO[A any]                   = io.IO[A]
	IOResult[A any]             = ioresult.IOResult[A]
	Option[A any]               = option.Option[A]
	Pair[A, B any]              = pair.Pair[A, B]
	Reader[R, A any]            = reader.Reader[R, A]
	Record[K comparable, V any] = record.Record[K, V]
	Result[A any]               = result.Result[A]

	Section = prompt.Section
	// Directory is a skill directory: its path, and the file system it is
	// opened as.
	Directory = Pair[string, fs.FS]
)
