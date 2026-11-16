package cli

import (
	"bufio"
	"context"
	"io"
	"strings"

	oai "github.com/Carsten-Leue/fp-go-harness/openai"
	"github.com/Carsten-Leue/fp-go-harness/session"
	thunk "github.com/IBM/fp-go/v2/context/readerioresult"
	"github.com/IBM/fp-go/v2/effect"
	"github.com/IBM/fp-go/v2/endomorphism"
	F "github.com/IBM/fp-go/v2/function"
	"github.com/IBM/fp-go/v2/ioresult"
	"github.com/IBM/fp-go/v2/option"
	"github.com/IBM/fp-go/v2/pair"
	P "github.com/IBM/fp-go/v2/predicate"
	"github.com/IBM/fp-go/v2/reader"
	RRIOE "github.com/IBM/fp-go/v2/readerreaderioeither"
	"github.com/IBM/fp-go/v2/result"
	"github.com/IBM/fp-go/v2/tailrec"
	C "github.com/urfave/cli/v3"
)

// maxLineSize is the longest line the REPL reads, so a pasted prompt longer
// than [bufio.MaxScanTokenSize] does not end the input.
const maxLineSize = 1 << 20

// interactiveFlag is the name of the flag that starts the REPL with the
// prompt of the arguments as its first line.
const interactiveFlag = "interactive"

// promptMarker is the marker the REPL prints on the standard error before it
// waits for a line.
const promptMarker = "> "

// replStep is one step of the REPL: the session to continue with, or the end
// of the input.
type replStep = tailrec.Trampoline[session.Session, F.Void]

// replEffect is an effect of the REPL, against any provider's
// ChatCompletionDeps.
type replEffect[T any] = Effect[oai.ChatCompletionDeps, T]

// scanLines is a leaf: it starts the one goroutine that reads the lines of r
// and sends them to the returned channel, first the line of first, if any.
// Blank lines are skipped. The channel is closed at the end of the input.
// The channel is both the source of the REPL's lines and its steering queue.
// A goroutine with a scanner has no fp-go lift.
func scanLines(r io.Reader, first Option[string]) IO[lineQueue] {
	return func() lineQueue {
		lines := make(chan string)
		go func() {
			defer close(lines)
			if line, ok := option.Unwrap(first); ok {
				lines <- line
			}
			scanner := bufio.NewScanner(r)
			scanner.Buffer(nil, maxLineSize)
			for scanner.Scan() {
				if strings.TrimSpace(scanner.Text()) != "" {
					lines <- scanner.Text()
				}
			}
		}()
		return lines
	}
}

// nextLine is a leaf: it waits for the next line of the channel, None when
// the channel is closed or the context is cancelled while it waits. A
// blocking select has no fp-go lift.
func nextLine(lines lineQueue) ReaderIOResult[Option[string]] {
	none := result.Of(option.None[string]())
	return func(ctx context.Context) IOResult[Option[string]] {
		return func() Result[Option[string]] {
			select {
			case <-ctx.Done():
				return none
			case line, ok := <-lines:
				return result.Of(option.Wrap(line, ok))
			}
		}
	}
}

// rootReader is a leaf: the reader of the root command, the standard input
// unless a test replaces it, see [rootWriter].
func rootReader(cmd *C.Command) io.Reader {
	return cmd.Root().Reader
}

// printPrompt prints the prompt marker on the standard error of a command.
// The marker is best effort: the REPL ignores a failed write.
func printPrompt() Reader[*C.Command, IOResult[int]] {
	return F.Flow2(
		rootErrWriter,
		F.Flow2(writeString(), reader.Read[IOResult[int]](promptMarker)),
	)
}

// continueSession is the step after a reported turn: the REPL continues with
// its session, whatever ended the turn, an interrupt included.
func continueSession() func(session.TurnResult) replStep {
	return F.Flow2(pair.Head[session.Session, session.Outcome], tailrec.Bounce[F.Void, session.Session])
}

// replStart is the session the REPL starts with: the request of its mode,
// with the model set. A failure to build it is printed on the standard
// error.
func replStart() Reader[replEnv, replEffect[session.Session]] {
	envLens := MakereplEnvEnvLens()
	commandLens := MakereplEnvCommandLens()

	return F.Pipe1(
		F.Flow3(
			commandLens.Get,
			printError(),
			effect.TapLeftIOK[session.Session, oai.ChatCompletionDeps, Result[int]],
		),
		reader.Ap[replEffect[session.Session]](F.Flow3(
			envLens.Get,
			modeRequest[oai.ChatCompletionDeps](),
			effect.Map[oai.ChatCompletionDeps](F.Flow2(withModel(), session.MakeSession)),
		)),
	)
}

// promptLine prints the prompt marker, then waits for the next line, see
// [nextLine]. Neither step checks the context, so a cancelled context ends
// the wait with None.
func promptLine() Reader[replEnv, replEffect[Option[string]]] {
	commandLens := MakereplEnvCommandLens()
	linesLens := MakereplEnvLinesLens()

	return F.Pipe1(
		F.Flow4(
			linesLens.Get,
			F.Flow2(nextLine, effect.FromThunk[oai.ChatCompletionDeps, Option[string]]),
			F.Constant1[Result[int], replEffect[Option[string]]],
			RRIOE.Chain[oai.ChatCompletionDeps, context.Context, error, Result[int], Option[string]],
		),
		reader.Ap[replEffect[Option[string]]](F.Flow3(
			commandLens.Get,
			printPrompt(),
			effect.FromIO[oai.ChatCompletionDeps, Result[int]],
		)),
	)
}

// readLine prompts for the next line under a child of the context that an
// interrupt cancels, see [promptLine] and [interruptible]: Ctrl-C at the
// prompt ends the wait with None. A cancelled context of the REPL itself
// fails at once, before the prompt, with the check of [thunk.WithContext].
func readLine() Reader[replEnv, replEffect[Option[string]]] {
	interruptsLens := MakereplEnvInterruptsLens()

	return F.Pipe2(
		F.Flow2(interruptsLens.Get, interruptible[oai.ChatCompletionDeps, Option[string]]()),
		reader.Ap[replEffect[Option[string]]](promptLine()),
		reader.Map[replEnv](reader.Map[oai.ChatCompletionDeps](thunk.WithContext[Option[string]])),
	)
}

// The values of [withLine]: the environment_details that wrap a line, and the
// session continued with a line, before and after the details are read.
type (
	lineDetails   = replEffect[endomorphism.Endomorphism[string]]
	wrappedLine   = func(endomorphism.Endomorphism[string]) session.Session
	continuedLine = func(string) replEffect[session.Session]
)

// lineChange is the change of a line to the session, the line wrapped by the
// environment_details of the turn and added with [session.Continue].
func lineChange() func(string) func(endomorphism.Endomorphism[string]) endomorphism.Endomorphism[session.Session] {
	return F.Flow2(
		reader.Read[string, string],
		reader.Map[endomorphism.Endomorphism[string]](session.Continue()),
	)
}

// sessionLine is a session continued with a line, once the
// environment_details that wrap it are known, see [lineChange].
func sessionLine() Reader[session.Session, func(string) wrappedLine] {
	return F.Flip(F.Flow2(lineChange(), F.Flip[endomorphism.Endomorphism[string], session.Session, session.Session]))
}

// withLine continues a session with a line: the line wrapped by the
// environment_details of the turn, which show the todo list of the session
// (H3), and added with [session.Continue], see [sessionLine]. The details
// and the continuation are independent reads of the session.
func withLine() Reader[replEnv, Reader[session.Session, continuedLine]] {
	envLens := MakereplEnvEnvLens()

	continuation := F.Pipe1(
		sessionLine(),
		reader.Map[session.Session](F.Flow2(
			reader.Map[string](effect.Map[oai.ChatCompletionDeps, endomorphism.Endomorphism[string], session.Session]),
			F.Flip[string, lineDetails, replEffect[session.Session]],
		)),
	)

	return F.Flow4(
		envLens.Get,
		modeDetails[oai.ChatCompletionDeps](),
		reader.Local[lineDetails](session.Todos()),
		F.Flow2(
			reader.Ap[continuedLine, session.Session, lineDetails],
			reader.Read[Reader[session.Session, continuedLine]](continuation),
		),
	)
}

// turnKleisli is a turn of a session, see [runTurn].
type turnKleisli = effect.Kleisli[oai.ChatCompletionDeps, session.Session, session.TurnResult]

// runTurn runs a turn of the session with the lines of the REPL steering it,
// see [runSession], under a child of the context that an interrupt cancels,
// see [interruptible]. The turn is total, so Ctrl-C ends it with its partial
// session and the cancellation, and the REPL goes on.
func runTurn() Reader[replEnv, func(replEffect[session.Session]) replEffect[session.TurnResult]] {
	envLens := MakereplEnvEnvLens()
	linesLens := MakereplEnvLinesLens()
	interruptsLens := MakereplEnvInterruptsLens()

	return F.Pipe4(
		F.Flow2(envLens.Get, runSession()),
		reader.Ap[turnKleisli](F.Flow2(linesLens.Get, session.ChannelSteering)),
		reader.Map[replEnv](reader.Compose[replEffect[session.TurnResult], session.Session, replEffect[session.TurnResult]]),
		reader.Ap[turnKleisli](F.Flow2(interruptsLens.Get, interruptible[oai.ChatCompletionDeps, session.TurnResult]())),
		reader.Map[replEnv](RRIOE.Chain[oai.ChatCompletionDeps, context.Context, error, session.Session, session.TurnResult]),
	)
}

// reportTurnTap prints the report of a turn, see [reportTurn]. The REPL
// drops the error of a failed turn: it continues, see [continueSession].
func reportTurnTap() Reader[replEnv, endomorphism.Endomorphism[replEffect[session.TurnResult]]] {
	commandLens := MakereplEnvCommandLens()

	return F.Flow3(
		commandLens.Get,
		reportTurn(),
		RRIOE.TapIOK[oai.ChatCompletionDeps, context.Context, error, session.TurnResult, Result[int]],
	)
}

// turn runs a turn of a session, stops the background processes after a
// cancelled turn, reports the turn and continues with its session. The taps
// do not check the context, so a cancelled turn is reported too.
func turn() Reader[replEnv, func(replEffect[session.Session]) replEffect[replStep]] {
	envLens := MakereplEnvEnvLens()

	// Concat runs right to left: the processes are stopped first, then the
	// turn is reported
	taps := reader.ApplicativeMonoid[replEnv](endomorphism.Monoid[replEffect[session.TurnResult]]()).Concat(
		reportTurnTap(),
		F.Flow2(envLens.Get, stopProcesses[oai.ChatCompletionDeps]()),
	)

	return F.Pipe3(
		runTurn(),
		reader.Map[replEnv](reader.Compose[replEffect[session.TurnResult], replEffect[session.Session], replEffect[session.TurnResult]]),
		reader.Ap[func(replEffect[session.Session]) replEffect[session.TurnResult]](taps),
		reader.Map[replEnv](reader.Map[replEffect[session.Session]](effect.Map[oai.ChatCompletionDeps](continueSession()))),
	)
}

// lineStep is the step of the REPL for a line, see [lineTurn].
type lineStep = func(string) replEffect[replStep]

// lineTurn is the turn of a session for a line: the session is continued
// with the line, see [withLine], then the turn runs. The stages are composed
// in data-flow order.
func lineTurn() Reader[replEnv, func(session.Session) lineStep] {
	return F.Pipe2(
		withLine(),
		reader.Map[replEnv](reader.Compose[lineStep, session.Session, continuedLine]),
		reader.Ap[func(session.Session) lineStep](F.Flow2(turn(), reader.Map[string, replEffect[session.Session], replEffect[replStep]])),
	)
}

// replLoopStep is one step of the REPL from a session: it reads a line and
// runs its turn, or lands at the end of the input.
func replLoopStep() Reader[replEnv, func(session.Session) replEffect[replStep]] {
	landEOF := F.Constant(effect.Of[oai.ChatCompletionDeps](tailrec.Land[session.Session](F.VOID)))

	type onLine = func(replEffect[Option[string]]) replEffect[replStep]

	return F.Pipe2(
		F.Flow2(lineTurn(), reader.Map[session.Session](F.Flow2(
			F.Bind1st(option.Fold[string, replEffect[replStep]], landEOF),
			RRIOE.Chain[oai.ChatCompletionDeps, context.Context, error, Option[string], replStep],
		))),
		reader.Map[replEnv](reader.Compose[replEffect[replStep], session.Session, onLine]),
		reader.Ap[func(session.Session) replEffect[replStep]](F.Flow2(readLine(), reader.Read[replEffect[replStep], replEffect[Option[string]]])),
	)
}

// repl is the REPL of an environment, see [replEnv]. The session starts from
// the request of the mode. Each line is wrapped by the environment_details of
// the turn, added with [session.Continue] and run with the lines of the
// channel steering the turn after its first request. The turn is reported
// like a one-shot turn, and the next line continues its session, also after
// a failed or interrupted turn; after a cancelled turn the background
// processes are stopped. The wait for a line and each turn run under their
// own child of the context, which an interrupt cancels, see [readLine] and
// [runTurn]: Ctrl-C during a turn ends only that turn, Ctrl-C at the prompt
// ends the REPL without an error, as the end of the input does. A failure to
// build the first request is printed on the standard error; the turns report
// their own errors.
//
// The steps are joined by readerreaderioeither's Chain and TailRec, which do
// not check the context. When the context of the REPL itself is cancelled, a
// running turn ends with its report and the REPL with the cancellation, at
// the next prompt.
func repl() Reader[replEnv, replEffect[F.Void]] {
	return F.Pipe1(
		F.Flow3(
			replLoopStep(),
			RRIOE.TailRec[oai.ChatCompletionDeps, context.Context, error, session.Session, F.Void],
			RRIOE.Chain[oai.ChatCompletionDeps, context.Context, error, session.Session, F.Void],
		),
		reader.Ap[replEffect[F.Void]](replStart()),
	)
}

// replOf is the REPL of a source of interrupts and a command, over a channel
// of lines and the environment of a mode, see [newReplEnv].
func replOf() Reader[InterruptDeps, Reader[*C.Command, Reader[lineQueue, Reader[modeEnv, replEffect[F.Void]]]]] {
	return F.Flow2(
		F.Curry4(newReplEnv),
		reader.Map[*C.Command](reader.Map[lineQueue](reader.Map[modeEnv](repl()))),
	)
}

// scanCommand starts the reader of the lines of a command, the prompt of the
// arguments first, see [scanLines].
func scanCommand() Reader[*C.Command, IOResult[lineQueue]] {
	return F.Pipe2(
		F.Flow2(rootReader, F.Curry2(scanLines)),
		reader.Ap[IO[lineQueue]](argsPrompt()),
		reader.Map[*C.Command](ioresult.FromIO[lineQueue]),
	)
}

// replWithLines is the REPL of a source of interrupts and a command over the
// environment of a mode, once the reader of its lines is started, see
// [scanCommand].
func replWithLines() Reader[InterruptDeps, Reader[*C.Command, ioresult.Kleisli[modeEnv, replEffect[F.Void]]]] {
	withLines := F.Flow3(
		scanCommand(),
		reader.Read[IOResult[replEffect[F.Void]], IOResult[lineQueue]],
		reader.Compose[IOResult[replEffect[F.Void]]](ioresult.Map[lineQueue, replEffect[F.Void]]),
	)

	return F.Flow4(
		replOf(),
		reader.Map[*C.Command](F.Flip[lineQueue, modeEnv, replEffect[F.Void]]),
		reader.Map[*C.Command](reader.Compose[IOResult[replEffect[F.Void]], modeEnv, func(lineQueue) replEffect[F.Void]]),
		reader.Ap[ioresult.Kleisli[modeEnv, replEffect[F.Void]]](withLines),
	)
}

// makeRepl is wiring: it looks the mode of a configuration up and builds its
// REPL over the lines of the reader of a command, the prompt of the
// arguments first. The reader goroutine starts only after the lookup.
func makeRepl() Reader[InterruptDeps, Reader[*C.Command, ioresult.Kleisli[Config, replEffect[F.Void]]]] {
	return F.Flow2(
		replWithLines(),
		reader.Map[*C.Command](F.Flow2(
			ioresult.Chain[modeEnv, replEffect[F.Void]],
			reader.Compose[IOResult[replEffect[F.Void]]](makeModeEnv()),
		)),
	)
}

// The REPL against DeepSeek, before and after its deps are provided.
type (
	deepSeekRepl = Effect[oai.DeepSeekDeps, F.Void]
	runDeepSeek  = func(replEffect[F.Void]) ReaderIOResult[F.Void]
)

// liftDeepSeek runs the REPL of a command against DeepSeek: its chat deps
// are built from the DeepSeek deps, and a failure to build them, such as a
// missing API key, is printed on the standard error of the command. The tap
// does not check the context.
func liftDeepSeek() Reader[*C.Command, func(replEffect[F.Void]) deepSeekRepl] {
	return F.Flow4(
		printError(),
		effect.TapLeftIOK[oai.ChatCompletionDeps, oai.DeepSeekDeps, Result[int]],
		reader.Read[Effect[oai.DeepSeekDeps, oai.ChatCompletionDeps]](oai.MakeDeepSeekChatCompletionDeps()),
		F.Flow2(
			F.Constant1[oai.DeepSeekDeps, Effect[oai.DeepSeekDeps, oai.ChatCompletionDeps]],
			effect.LocalEffectK[F.Void, oai.ChatCompletionDeps, oai.DeepSeekDeps],
		),
	)
}

// replProgram is wiring: it decodes the [Config] from the variables of a run
// and returns the REPL of its mode for a source of interrupts and a command
// against DeepSeek, with the DeepSeek deps reading their API key from the
// same variables, see [provideDeepSeek] and [liftDeepSeek]. The variables are
// read twice: for the configuration and for the deps; the command for the
// REPL and for the printer of a failure to build the deps.
func replProgram() Reader[InterruptDeps, Reader[*C.Command, ioresult.Kleisli[Record[string, string], ReaderIOResult[F.Void]]]] {
	provide := F.Flow5(
		liftDeepSeek(),
		reader.Local[ReaderIOResult[F.Void], deepSeekRepl, replEffect[F.Void]],
		reader.Map[Record[string, string], func(deepSeekRepl) ReaderIOResult[F.Void], runDeepSeek],
		reader.Read[Reader[Record[string, string], runDeepSeek]](provideDeepSeek[F.Void]()),
		reader.Map[Record[string, string]](ioresult.Map[replEffect[F.Void], ReaderIOResult[F.Void]]),
	)

	program := F.Flow2(
		makeRepl(),
		reader.Map[*C.Command](F.Flow2(
			ioresult.Chain[Config, replEffect[F.Void]],
			reader.Compose[IOResult[replEffect[F.Void]]](F.Flow2(decodeConfig(), ioresult.FromResult[Config])),
		)),
	)

	return F.Flow3(
		program,
		reader.Map[*C.Command](reader.Ap[IOResult[ReaderIOResult[F.Void]], Record[string, string], IOResult[replEffect[F.Void]]]),
		reader.Ap[ioresult.Kleisli[Record[string, string], ReaderIOResult[F.Void]]](provide),
	)
}

// replTarget is the command's REPL, once its setup is done, see [runRepl].
type replTarget = Reader[*C.Command, IOResult[ReaderIOResult[F.Void]]]

// runRepl runs the REPL of a source of interrupts and the command under the
// configuration of its run. An error before the REPL starts is printed on
// the standard error; the turns report their own errors.
func runRepl() Reader[InterruptDeps, Reader[*C.Command, ReaderIOResult[F.Void]]] {
	printSetupError := F.Flow2(printError(), ioresult.TapLeft[ReaderIOResult[F.Void], int])

	return F.Flow5(
		replProgram(),
		reader.Map[*C.Command](ioresult.Chain[Record[string, string], ReaderIOResult[F.Void]]),
		reader.Ap[IOResult[ReaderIOResult[F.Void]]](loadEnv()),
		F.Flow2(
			reader.Ap[IOResult[ReaderIOResult[F.Void]], *C.Command, IOResult[ReaderIOResult[F.Void]]],
			reader.Read[replTarget](printSetupError),
		),
		reader.Map[*C.Command](F.Flow2(
			thunk.FromIOResult[ReaderIOResult[F.Void]],
			thunk.Flatten[F.Void],
		)),
	)
}

// isInteractive matches the commands that run the REPL: with the
// interactive flag, or without a prompt.
func isInteractive() P.Predicate[*C.Command] {
	return F.Pipe1(
		F.Bind2nd((*C.Command).Bool, interactiveFlag),
		P.Or(F.Flow2((*C.Command).NArg, P.IsZero[int]())),
	)
}

// commandRun is the run of a command, see [runCommand].
type commandRun = Reader[*C.Command, ReaderIOResult[F.Void]]

// runCommand runs the REPL or answers the one prompt of the command, see
// [isInteractive], with a source of interrupts. The one prompt runs under a
// child of the context that an interrupt cancels, see [interruptibleThunk],
// so Ctrl-C ends the run with the cancellation; the REPL scopes each wait
// and each turn, see [repl].
func runCommand() Reader[InterruptDeps, commandRun] {
	oneShot := F.Flow2(
		interruptibleThunk[F.Void](),
		reader.Compose[ReaderIOResult[F.Void]](F.Flow2(printResult(), thunk.MapTo[int](F.VOID))),
	)

	return F.Pipe3(
		oneShot,
		reader.Map[InterruptDeps](F.Curry2(P.Fold[*C.Command, ReaderIOResult[F.Void]])),
		reader.Ap[func(P.Predicate[*C.Command]) commandRun](runRepl()),
		reader.Map[InterruptDeps](reader.Read[commandRun](isInteractive())),
	)
}
