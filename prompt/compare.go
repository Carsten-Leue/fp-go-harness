package prompt

import (
	"regexp"
	"slices"
	"strings"

	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/endomorphism"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	S "github.com/IBM/fp-go/v2/string"
)

// SectionDiff compares the top-level sections of a recorded system prompt with
// those of an assembled one, by tag. The order matches when Recorded and
// Assembled are equal.
//
// It has no generated lenses: for a struct with only slice fields the
// generator emits unused imports.
type SectionDiff struct {
	// Missing are the tags of the recorded prompt the assembled one lacks, in
	// recorded order.
	Missing []string
	// Extra are the tags of the assembled prompt the recorded one lacks, in
	// assembled order.
	Extra []string
	// Recorded are the tags both prompts have, in recorded order.
	Recorded []string
	// Assembled are the tags both prompts have, in assembled order.
	Assembled []string
}

// tagScan is the state of [SectionTags] while it reads the lines of a prompt:
// the tag of the section it is in (empty outside a section) and the tags
// found so far.
type tagScan = Pair[string, []string]

// TagPair pairs the tags of a recorded prompt (head) with those of an
// assembled one (tail).
type TagPair = Pair[[]string, []string]

type (
	diffFromAssembled = func([]string) SectionDiff
	diffFromRecorded  = func([]string) diffFromAssembled
)

// openingTagPattern matches a line that is an opening tag and nothing else.
var openingTagPattern = regexp.MustCompile(`^<([A-Za-z_][\w-]*)>$`)

// openingTag is the tag a line opens, if the line is an opening tag on its
// own.
func openingTag() option.Kleisli[string, string] {
	return F.Flow2(
		openingTagPattern.FindStringSubmatch,
		A.Lookup[string](1),
	)
}

// closingTag is a leaf: the line that closes the section of a tag.
func closingTag(tag string) string {
	return "</" + tag + ">"
}

// isOutside holds while the scan is not inside a section.
func isOutside() P.Predicate[tagScan] {
	return F.Flow2(pair.Head[string, []string], S.IsEmpty)
}

// openTag sets the section the scan is in to a tag; the empty tag leaves it.
func openTag() Reader[string, Endomorphism[tagScan]] {
	return F.Flow2(
		F.Constant1[string, string],
		pair.MapHead[[]string, string, string],
	)
}

// recordTag adds a tag to the tags found so far.
func recordTag() Reader[string, Endomorphism[tagScan]] {
	return F.Flow2(
		F.Flip(F.Curry2(A.Append[string])),
		pair.MapTail[string, []string, []string],
	)
}

// stepMonoid composes steps that read the same line.
func stepMonoid() Monoid[Reader[string, Endomorphism[tagScan]]] {
	return reader.ApplicativeMonoid[string](endomorphism.Monoid[tagScan]())
}

// whenOutside applies a step only while the scan is not inside a section.
func whenOutside() Endomorphism[Endomorphism[tagScan]] {
	return F.Flow2(
		F.Curry2(P.Fold[tagScan, tagScan])(F.Identity[tagScan]),
		reader.Read[Endomorphism[tagScan]](isOutside()),
	)
}

// enter is the step into the section of a tag: open it and record it, unless
// the scan is in a section already, whose nested tags are skipped. Opening and
// recording change different sides of the scan, so their order doesn't
// matter.
func enter() Reader[string, Endomorphism[tagScan]] {
	return F.Flow2(
		stepMonoid().Concat(openTag(), recordTag()),
		whenOutside(),
	)
}

// opens is the step for an opening tag: a line that is one enters its
// section, any other line leaves the scan as it is.
func opens() Reader[string, Endomorphism[tagScan]] {
	return F.Flow3(
		openingTag(),
		option.Map(enter()),
		option.GetOrElse(F.Constant(F.Identity[tagScan])),
	)
}

// closedBy holds when a line closes the section the scan is in.
func closedBy() Reader[string, P.Predicate[tagScan]] {
	return F.Flow2(
		F.Curry2(S.Eq),
		reader.Local[bool](F.Flow2(pair.Head[string, []string], closingTag)),
	)
}

// leave is the scan outside a section, with the tags found so far.
func leave() Endomorphism[tagScan] {
	return openTag()("")
}

// closes is the step for a closing tag: only the line that closes the section
// the scan is in ends it. Outside a section it changes nothing, because the
// empty tag it would close is the one it leaves.
func closes() Reader[string, Endomorphism[tagScan]] {
	return F.Flow2(
		closedBy(),
		P.Fold(F.Identity[tagScan], leave()),
	)
}

// lineStep is the step over one line, [opens] and [closes]. Each of them is
// right inside and outside a section, and no line both opens and closes one,
// so they compose in either order.
func lineStep() Reader[string, Endomorphism[tagScan]] {
	return stepMonoid().Concat(closes(), opens())
}

// scanLine is [lineStep] in the shape [A.Reduce] takes: the scan first, then
// the line.
func scanLine() func(tagScan, string) tagScan {
	return F.Uncurry2(F.Flip(lineStep()))
}

// SectionTags are the tags of the top-level sections of a rendered prompt, in
// order. A section starts at a line that is an opening tag, such as
// "<tool_use>", and ends at the first line that closes it; the tags nested in
// it are not listed, and a tag nested in itself ends the outer section. Text between sections is skipped, and CRLF line endings are
// fine.
func SectionTags() func(string) []string {
	return F.Flow4(
		F.Bind2nd(strings.Split, "\n"),
		A.Map(F.Bind2nd(strings.TrimSuffix, "\r")),
		A.Reduce(scanLine(), pair.MakePair("", A.Empty[string]())),
		pair.Tail[string, []string],
	)
}

// containedIn is, for a list of tags, the predicate that a tag is one of them.
func containedIn() Reader[[]string, P.Predicate[string]] {
	return F.Curry2(slices.Contains[[]string])
}

// filterBy is, for a predicate built from a list of tags, the tags of the head
// for which the predicate built from the tail holds. [pair.Merge] passes the
// tail to the filter first and the head second.
func filterBy() Reader[Reader[[]string, P.Predicate[string]], Reader[TagPair, []string]] {
	return F.Flow2(
		reader.Map[[]string](A.Filter[string]),
		pair.Merge[Reader[[]string, Endomorphism[[]string]], []string, []string, []string],
	)
}

// missing are the tags of the head that the tail lacks.
func missing() Reader[TagPair, []string] {
	return F.Pipe2(
		containedIn(),
		reader.Map[[]string](P.Not[string]),
		filterBy(),
	)
}

// common are the tags of the head that the tail has too.
func common() Reader[TagPair, []string] {
	return filterBy()(containedIn())
}

// makeSectionDiff is a leaf and a constructor that [F.Curry4] lifts.
func makeSectionDiff(missing, extra, recorded, assembled []string) SectionDiff {
	return SectionDiff{Missing: missing, Extra: extra, Recorded: recorded, Assembled: assembled}
}

// CompareTags compares the tags of a recorded prompt (head) with those of an
// assembled one (tail). Each field of the [SectionDiff] is read from the pair
// on its own.
func CompareTags() Reader[TagPair, SectionDiff] {
	return F.Pipe4(
		missing(),
		reader.Map[TagPair](F.Curry4(makeSectionDiff)),
		reader.Ap[diffFromRecorded](F.Flow2(pair.Swap[[]string, []string], missing())),
		reader.Ap[diffFromAssembled](common()),
		reader.Ap[SectionDiff](F.Flow2(pair.Swap[[]string, []string], common())),
	)
}

// CompareSections compares the top-level sections of a recorded system prompt
// (head) with those of an assembled one (tail), by their [SectionTags].
//
// Example:
//
//	diff := CompareSections()(pair.MakePair(recorded, assembled))
func CompareSections() Reader[Pair[string, string], SectionDiff] {
	return F.Flow2(
		pair.BiMap(SectionTags(), SectionTags()),
		CompareTags(),
	)
}
