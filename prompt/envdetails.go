package prompt

import (
	"time"

	"github.com/Carsten-Leue/fp-go-harness/tools"
	A "github.com/IBM/fp-go/v2/array"
	"github.com/IBM/fp-go/v2/effect"
	F "github.com/IBM/fp-go/v2/function"
	M "github.com/IBM/fp-go/v2/monoid"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/reader"
	S "github.com/IBM/fp-go/v2/string"
)

// Tags of the environment_details block and the sections nested in it, as in
// the recorded user messages.
const (
	EnvironmentDetailsTag = "environment_details"
	CurrentDatetimeTag    = "current_datetime"
	CurrentModeTag        = "current_mode"
	GitStatusTag          = "git_status_snapshot"
)

// GitStatusCommand is the command line whose output is the git status
// snapshot: the branch line followed by one line per changed file.
const GitStatusCommand = "git status --porcelain --branch"

// datetimeLayout formats the current time of the current_datetime section in
// UTC with milliseconds, as in the recordings.
const datetimeLayout = "2006-01-02T15:04:05.000Z07:00"

// Separators of the environment_details block: a line break between its
// nested sections, a blank line between the user's text and the block.
const (
	detailsSeparator = "\n"
	messageSeparator = "\n\n"
)

// EnvDetailsDeps gives the environment_details block the workspace, the
// processes that read its git status and the clock.
type EnvDetailsDeps interface {
	tools.WorkspaceDeps
	tools.ProcessDeps
	ClockDeps
}

type envDetailsDeps struct {
	tools.WorkspaceDeps
	tools.ProcessDeps
	now IO[time.Time]
}

func (d *envDetailsDeps) GetNow() IO[time.Time] { return d.now }

// MakeEnvDetailsDeps is wiring: it builds [EnvDetailsDeps] from its parts.
func MakeEnvDetailsDeps(ws tools.WorkspaceDeps, p tools.ProcessDeps, now IO[time.Time]) EnvDetailsDeps {
	return &envDetailsDeps{ws, p, now}
}

// AsEnvDetailsDeps is wiring: it narrows any wider dependencies to
// [EnvDetailsDeps], for use with [effect.Local].
func AsEnvDetailsDeps[R EnvDetailsDeps](r R) EnvDetailsDeps {
	return r
}

// formatDatetime is a leaf: the time in UTC, formatted with [datetimeLayout].
func formatDatetime(t time.Time) string {
	return t.UTC().Format(datetimeLayout)
}

// fenced is a leaf: the text in a Markdown code fence.
func fenced(s string) string {
	return "```\n" + s + "\n```"
}

// currentDatetime reads the clock into the current_datetime section.
func currentDatetime() Effect[EnvDetailsDeps, []Section] {
	return F.Pipe1(
		now[EnvDetailsDeps](),
		effect.Map[EnvDetailsDeps](F.Flow3(
			formatDatetime,
			F.Bind1st(MakeSection, CurrentDatetimeTag),
			A.Of[Section],
		)),
	)
}

// gitStatusProcess is the git status command, run in the workspace root.
func gitStatusProcess() Reader[EnvDetailsDeps, tools.Process] {
	return F.Flow2(
		EnvDetailsDeps.GetWorkspaceRoot,
		F.Bind1st(tools.MakeProcess, GitStatusCommand),
	)
}

// gitStatusSection is the git_status_snapshot section of a successful git
// status, none for a failed one, e.g. outside a repository or without git.
func gitStatusSection() A.Kleisli[tools.ProcessOutput, Section] {
	stdoutLens := tools.MakeProcessOutputStdoutLens()

	return F.Flow3(
		option.FromPredicate(tools.IsSuccess()),
		option.Map(F.Flow4(
			stdoutLens.Get,
			normalizeBody(),
			fenced,
			F.Bind1st(MakeSection, GitStatusTag),
		)),
		A.FromOption[Section],
	)
}

// gitStatus runs git status in the workspace with the [tools.RunProcess] of
// the dependencies. A command that cannot be run fails the effect.
func gitStatus() Effect[EnvDetailsDeps, []Section] {
	return F.Pipe3(
		EnvDetailsDeps.GetRunProcess,
		reader.Ap[Thunk[tools.ProcessOutput]](gitStatusProcess()),
		effect.FromReaderThunk[EnvDetailsDeps, tools.ProcessOutput],
		effect.Map[EnvDetailsDeps](gitStatusSection()),
	)
}

// currentMode is the current_mode section for the name of a mode.
func currentMode() effect.Kleisli[EnvDetailsDeps, string, []Section] {
	return F.Flow3(
		F.Bind1st(MakeSection, CurrentModeTag),
		A.Of[Section],
		effect.Of[EnvDetailsDeps, []Section],
	)
}

// detailsSections are the sections of the environment_details block for the
// name of a mode: current_datetime, current_mode and, in a git repository,
// git_status_snapshot. Each is a Kleisli over the name; the monoid lifts the
// concatenation of their sections over the name and the effect, so they are
// read concurrently and stay in this order.
func detailsSections() effect.Kleisli[EnvDetailsDeps, string, []Section] {
	sections := reader.ApplicativeMonoid[string](effect.ApplicativeMonoid[EnvDetailsDeps](A.Monoid[Section]()))

	return M.ConcatAll(sections)([]effect.Kleisli[EnvDetailsDeps, string, []Section]{
		F.Constant1[string](currentDatetime()),
		currentMode(),
		F.Constant1[string](gitStatus()),
	})
}

// renderDetails renders the nested sections into the environment_details
// block, one line break between them.
func renderDetails() func([]Section) string {
	return F.Flow3(
		A.FoldMap[Section](S.IntersperseMonoid(detailsSeparator))(RenderSection()),
		F.Bind1st(MakeSection, EnvironmentDetailsTag),
		RenderSection(),
	)
}

// appendDetails appends the rendered block to the user's text, after a blank
// line.
func appendDetails() func(string) Endomorphism[string] {
	return F.Flip(F.Curry2(S.Intersperse(messageSeparator)))
}

// EnvironmentDetails builds, for the name of the current mode, the
// endomorphism that wraps a user message: the text, a blank line and the
// environment_details block with the current date and time, the mode and the
// git status of the workspace. The block is read each time the effect runs,
// so every turn sees the current state. Outside a git repository there is no
// git status section.
func EnvironmentDetails() effect.Kleisli[EnvDetailsDeps, string, Endomorphism[string]] {
	return F.Flow2(
		detailsSections(),
		effect.Map[EnvDetailsDeps](F.Flow2(renderDetails(), appendDetails())),
	)
}
