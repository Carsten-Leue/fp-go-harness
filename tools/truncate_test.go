package tools

import (
	"errors"
	"strings"
	"testing"

	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/result"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrefix(t *testing.T) {
	assert.Equal(t, "hel", prefix(3, "hello"))
	assert.Equal(t, "", prefix(0, "hello"))
	assert.Equal(t, "hello", prefix(5, "hello"))
}

func TestTruncationNote(t *testing.T) {
	assert.Equal(t,
		"ab\n\n[Output truncated: showing the first 2 of 4 bytes.]",
		truncationNote("ab", "abcd"),
	)
}

func TestOrKeep(t *testing.T) {
	double := func(n int) int { return 2 * n }
	isEven := func(n int) bool { return n%2 == 0 }
	f := orKeep[int]()(double)(isEven)

	assert.Equal(t, 8, f(4))
	assert.Equal(t, 3, f(3))
}

func TestIsLongerThan(t *testing.T) {
	p := isLongerThan()(3)

	assert.True(t, p("abcd"))
	assert.False(t, p("abc"))
	assert.False(t, p(""))
	// counts bytes, not runes: "ää" is four bytes
	assert.True(t, p("ää"))
}

func TestValidPrefix(t *testing.T) {
	assert.Equal(t, "ab", validPrefix()(2)("abc"))
	assert.Equal(t, "", validPrefix()(0)("abc"))
	// "ä" is two bytes, a cut after 2 bytes splits it and drops the partial sequence
	assert.Equal(t, "a", validPrefix()(2)("aäb"))
	assert.Equal(t, "aä", validPrefix()(3)("aäb"))
}

func TestTruncated_AlwaysAppendsNote(t *testing.T) {
	assert.Equal(t,
		"hel\n\n[Output truncated: showing the first 3 of 5 bytes.]",
		truncated()(3)("hello"),
	)
	// truncated is unconditional; the length check lives in truncateText
	assert.Equal(t,
		"hello\n\n[Output truncated: showing the first 5 of 5 bytes.]",
		truncated()(5)("hello"),
	)
}

func TestTruncateText_BoundaryUnchanged(t *testing.T) {
	assert.Equal(t, "abc", truncateText()(3)("abc"))
}

func TestIsErrorLongerThan(t *testing.T) {
	p := isErrorLongerThan()(2)

	assert.True(t, p(errors.New("abc")))
	assert.False(t, p(errors.New("ab")))
}

func TestShortenedError(t *testing.T) {
	err := shortenedError()(2)(errors.New("abcd"))

	assert.EqualError(t, err, "ab\n\n[Output truncated: showing the first 2 of 4 bytes.]")
}

func TestTruncateError_LongErrorShortened(t *testing.T) {
	sentinel := errors.New("abcd")
	err := truncateError()(2)(sentinel)

	assert.EqualError(t, err, "ab\n\n[Output truncated: showing the first 2 of 4 bytes.]")
	assert.NotErrorIs(t, err, sentinel)
}

func TestTruncateError_ShortErrorKept(t *testing.T) {
	sentinel := errors.New("ab")

	assert.Same(t, sentinel, truncateError()(2)(sentinel))
	assert.Same(t, sentinel, truncateError()(5)(sentinel))
}

func TestTruncateThunk_Success(t *testing.T) {
	f := truncateThunk()(3)

	assert.Equal(t, result.Of("ab"), f(thunk.Of("ab"))(t.Context())())
	assert.Equal(t,
		result.Of("abc\n\n[Output truncated: showing the first 3 of 6 bytes.]"),
		f(thunk.Of("abcdef"))(t.Context())(),
	)
}

func TestTruncateThunk_Failure(t *testing.T) {
	f := truncateThunk()(3)

	sentinel := errors.New("ab")
	_, err := result.Unwrap(f(thunk.Left[string](sentinel))(t.Context())())
	assert.Same(t, sentinel, err)

	_, err = result.Unwrap(f(thunk.Left[string](errors.New("abcdef")))(t.Context())())
	assert.EqualError(t, err, "abc\n\n[Output truncated: showing the first 3 of 6 bytes.]")
}

func TestTruncateText_ShortTextUnchanged(t *testing.T) {
	assert.Equal(t, "hello", truncateText()(5)("hello"))
	assert.Equal(t, "", truncateText()(0)(""))
}

func TestTruncateText_LongTextCutWithNote(t *testing.T) {
	assert.Equal(t,
		"hel\n\n[Output truncated: showing the first 3 of 5 bytes.]",
		truncateText()(3)("hello"),
	)
}

// TestTruncateText_KeepsValidUTF8 asserts that a cut inside a multi-byte
// sequence drops the partial sequence.
func TestTruncateText_KeepsValidUTF8(t *testing.T) {
	// "ä" is two bytes, so a cut after 2 bytes splits it
	assert.Equal(t,
		"a\n\n[Output truncated: showing the first 1 of 4 bytes.]",
		truncateText()(2)("aäb"),
	)
}

func TestTruncate_ShortensSuccess(t *testing.T) {
	call := Truncate()(4)(makeEchoTool("echo", "").Call)

	assert.Equal(t, "ab", runCall(t, call, "ab"))
	assert.Equal(t,
		"abcd\n\n[Output truncated: showing the first 4 of 6 bytes.]",
		runCall(t, call, "abcdef"),
	)
}

func TestTruncate_ShortensLongError(t *testing.T) {
	call := Truncate()(4)(func(arguments string) Thunk[string] {
		return thunk.Left[string](errors.New(arguments))
	})

	res := call("abcdef")(t.Context())()
	assert.Equal(t,
		result.Left[string](errors.New("abcd\n\n[Output truncated: showing the first 4 of 6 bytes.]")),
		res,
	)
}

func TestTruncate_KeepsShortError(t *testing.T) {
	sentinel := errors.New("ab")
	call := Truncate()(4)(func(string) Thunk[string] {
		return thunk.Left[string](sentinel)
	})

	_, err := result.Unwrap(call("")(t.Context())())
	assert.ErrorIs(t, err, sentinel)
}

func TestTruncateTools_AppliesToEveryTool(t *testing.T) {
	registry := TruncateTools()(3)(MakeToolRegistry(makeEchoTool("a", "a:"), makeEchoTool("b", "b:")))
	require.Len(t, registry, 2)

	caller := ToToolCaller()(registry)
	for _, name := range []string{"a", "b"} {
		call, ok := option.Unwrap(caller(name))
		require.True(t, ok)
		assert.True(t, strings.HasPrefix(runCall(t, call, "xyz"), name+":x\n\n[Output truncated:"))
		assert.Equal(t, name, toolDefinition(registry[name]).Name)
	}
}
