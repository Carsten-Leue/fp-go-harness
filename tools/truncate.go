package tools

import (
	"errors"
	"fmt"
	"strings"

	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/endomorphism"
	ER "github.com/IBM/fp-go/v2/errors"
	F "github.com/IBM/fp-go/v2/function"
	N "github.com/IBM/fp-go/v2/number"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	R "github.com/IBM/fp-go/v2/record"
	S "github.com/IBM/fp-go/v2/string"
)

// prefix is a leaf: the first n bytes of s. string has no slicing combinator.
func prefix(n int, s string) string {
	return s[:n]
}

// truncationNote is a leaf: one formatter over the kept part and the full text.
func truncationNote(kept, s string) string {
	return fmt.Sprintf("%s\n\n[Output truncated: showing the first %d of %d bytes.]", kept, len(kept), len(s))
}

// orKeep applies an endomorphism to the values a predicate accepts and keeps
// every other value. It is generic, so it keeps its type parameter: Go can't
// infer T through a nullary call.
func orKeep[T any]() func(Endomorphism[T]) func(P.Predicate[T]) Endomorphism[T] {
	return F.Curry2(P.Fold[T, T])(F.Identity[T])
}

// isLongerThan accepts a text of more than n bytes.
func isLongerThan() Reader[int, P.Predicate[string]] {
	return F.Flow2(N.MoreThan[int], reader.Local[bool](S.Size))
}

// validPrefix keeps the first n bytes of a text and drops a UTF-8 sequence
// split by the cut.
func validPrefix() Reader[int, Endomorphism[string]] {
	return F.Flow2(
		F.Curry2(prefix),
		reader.Map[string](F.Bind2nd(strings.ToValidUTF8, "")),
	)
}

// truncated cuts a text to n bytes, see [validPrefix], and appends the note
// that says how much was kept. The note reads the text a second time.
func truncated() Reader[int, Endomorphism[string]] {
	return F.Flow2(validPrefix(), reader.Chain(F.Curry2(truncationNote)))
}

// truncateText shortens a text to at most n bytes and appends a note that says
// how much was kept, see [truncated]. Text of at most n bytes is returned
// unchanged.
func truncateText() Reader[int, Endomorphism[string]] {
	return F.Pipe2(
		truncated(),
		reader.Map[int](orKeep[string]()),
		reader.Ap[Endomorphism[string]](isLongerThan()),
	)
}

// isErrorLongerThan accepts an error whose text is longer than n bytes.
func isErrorLongerThan() Reader[int, P.Predicate[error]] {
	return F.Flow2(isLongerThan(), reader.Local[bool](ER.ToString))
}

// shortenedError replaces an error by one with its truncated text, see
// [truncated].
func shortenedError() Reader[int, Endomorphism[error]] {
	return F.Flow3(
		truncated(),
		reader.Map[string](errors.New),
		reader.Local[error](ER.ToString),
	)
}

// truncateError replaces an error whose text is longer than n bytes by one
// with the truncated text. Shorter errors are kept as they are, so that
// [errors.Is] still sees them.
func truncateError() Reader[int, Endomorphism[error]] {
	return F.Pipe2(
		shortenedError(),
		reader.Map[int](orKeep[error]()),
		reader.Ap[Endomorphism[error]](isErrorLongerThan()),
	)
}

// truncateThunk shortens the value of a successful [Thunk] with
// [truncateText] and the text of a failed one with [truncateError]. Both are
// endomorphisms read from the same n, so they compose in the endomorphism
// monoid lifted over the reader; they act on different branches of the
// result, so their order doesn't matter.
func truncateThunk() Reader[int, Endomorphism[Thunk[string]]] {
	return reader.ApplicativeMonoid[int](endomorphism.Monoid[Thunk[string]]()).Concat(
		F.Flow2(truncateText(), thunk.Map[string, string]),
		F.Flow2(truncateError(), thunk.MapLeft[string]),
	)
}

// Truncate shortens the output of a tool call to at most n bytes, plus a note
// that the output was truncated. It applies to the text of a failed call too,
// because that text goes back to the model as the tool message.
func Truncate() Reader[int, Endomorphism[ToolCall]] {
	return F.Flow2(truncateThunk(), reader.Map[string, Thunk[string], Thunk[string]])
}

// TruncateTools applies [Truncate] to the call of every tool of a registry.
func TruncateTools() Reader[int, Endomorphism[ToolRegistry]] {
	callLens := MakeToolCallLens()

	return F.Flow3(Truncate(), callLens.Modify, R.Map[string, Tool, Tool])
}
