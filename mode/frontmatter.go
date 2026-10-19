package mode

import (
	"errors"
	"strings"

	A "github.com/IBM/fp-go/v2/array"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/readerresult"
	"github.com/IBM/fp-go/v2/result"
	S "github.com/IBM/fp-go/v2/string"
)

// FrontMatterDelimiter opens and closes the front matter of a Markdown file,
// each time on a line of its own.
const FrontMatterDelimiter = "---"

var (
	errNoFrontMatter       = errors.New("no front matter: the file must start with a line of " + FrontMatterDelimiter)
	errUnclosedFrontMatter = errors.New("front matter without a closing line of " + FrontMatterDelimiter)
)

// FrontMatter is a Markdown file split at its front matter: the text of the
// front matter (head) and the body after it (tail).
type FrontMatter = Pair[string, string]

// lines splits a text into lines that keep their line ending, so joining
// them gives back the text.
func lines() func(string) []string {
	return F.Bind2nd(strings.SplitAfter, "\n")
}

// joinLines joins lines that keep their line ending.
func joinLines() func([]string) string {
	return F.Bind2nd(strings.Join, "")
}

// isDelimiter tells whether a line is a [FrontMatterDelimiter]. Surrounding
// white space, including a CRLF line ending, doesn't count.
func isDelimiter() P.Predicate[string] {
	return F.Flow2(
		strings.TrimSpace,
		F.Bind1st(S.Eq, FrontMatterDelimiter),
	)
}

// startsWithDelimiter tells whether the first line opens a front matter.
func startsWithDelimiter() P.Predicate[[]string] {
	return F.Flow2(
		A.Head[string],
		option.Exists(isDelimiter()),
	)
}

// delimiterIndex keeps the index of a line that is a delimiter.
func delimiterIndex() func(int, string) Option[int] {
	return pair.Unpaired(F.Flow2(
		option.FromPredicate(F.Flow2(pair.Tail[int, string], isDelimiter())),
		option.Map(pair.Head[int, string]),
	))
}

// closingDelimiter is the index of the first delimiter of the lines after
// the opening one.
func closingDelimiter() result.Kleisli[[]string, int] {
	return F.Flow2(
		A.FindFirstMapWithIndex(delimiterIndex()),
		result.FromOption[int](F.Constant(errUnclosedFrontMatter)),
	)
}

// splitAt is a leaf: one constructor over the lines before the delimiter at
// i and the lines after it.
func splitAt(i int, ls []string) Pair[[]string, []string] {
	return pair.MakePair(
		A.Slice[string](0, i)(ls),
		A.SliceRight[string](i+1)(ls),
	)
}

// splitAtClosing splits the lines after the opening delimiter at the closing
// one and joins each part. The lines are read twice: to find the delimiter
// and to split at it.
func splitAtClosing() result.Kleisli[[]string, FrontMatter] {
	join := joinLines()

	return F.Pipe2(
		closingDelimiter(),
		readerresult.ChainReaderK(F.Curry2(splitAt)),
		readerresult.Map[[]string](pair.BiMap(join, join)),
	)
}

// SplitFrontMatter splits a Markdown file into its front matter and its
// body. The file starts with a line of [FrontMatterDelimiter], and the front
// matter ends at the next one; both lines belong to neither part. The text
// of the front matter isn't decoded. A file that doesn't start with a
// delimiter, or whose front matter isn't closed, is an error.
func SplitFrontMatter() result.Kleisli[string, FrontMatter] {
	return F.Flow3(
		lines(),
		result.FromPredicate(startsWithDelimiter(), F.Constant1[[]string](errNoFrontMatter)),
		result.Chain(F.Flow2(A.SliceRight[string](1), splitAtClosing())),
	)
}
