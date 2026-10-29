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
	"github.com/openai/openai-go/v3"
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

// continueOrEnd decides the step after a reported turn: a cancelled turn ends
// the REPL with its cancellation, any other turn continues with its session.
func continueOrEnd() effect.Kleisli[oai.ChatCompletionDeps, session.TurnResult, replStep] {
	return F.Flow3(
		result.FromPredicate(
			P.Not(isCancelledTurn()),
			F.Flow2(pair.Tail[session.Session, session.Outcome], result.ToError[*openai.ChatCompletion]),
		),
		result.Map(F.Flow2(pair.Head[session.Session, session.Outcome], tailrec.Bounce[F.Void, session.Session])),
		effect.FromResult[oai.ChatCompletionDeps, replStep],
	)
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

// readLine prints the prompt marker, then waits for the next line, see
// [nextLine]. Neither step checks the context, so a cancelled context ends
// the wait with None.
func readLine() Reader[replEnv, replEffect[Option[string]]] {
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

// withLine is the change of a line to the session, the line wrapped by the
// environment_details of the turn and added with [session.Continue].
func withLine() Reader[replEnv, Reader[string, replEffect[endomorphism.Endomorphism[session.Session]]]] {
	envLens := MakereplEnvEnvLens()

	lineChange := F.Flow3(
		reader.Read[string, string],
		reader.Map[endomorphism.Endomorphism[string]](session.Continue()),
		effect.Map[oai.ChatCompletionDeps, endomorphism.Endomorphism[string], endomorphism.Endomorphism[session.Session]],
	)

	return F.Flow3(
		F.Flow2(envLens.Get, modeDetails[oai.ChatCompletionDeps]()),
		reader.Read[replEffect[endomorphism.Endomorphism[session.Session]], replEffect[endomorphism.Endomorphism[string]]],
		reader.Compose[replEffect[endomorphism.Endomorphism[session.Session]]](lineChange),
	)
}

// applyTo applies the change of an effect to a session.
func applyTo() Reader[session.Session, func(replEffect[endomorphism.Endomorphism[session.Session]]) replEffect[session.Session]] {
	return F.Flow2(
		reader.Read[session.Session, session.Session],
		effect.Map[oai.ChatCompletionDeps, endomorphism.Endomorphism[session.Session], session.Session],
	)
}

// runTurn runs a turn of the session with the lines of the REPL steering it,
// see [runSession].
func runTurn() Reader[replEnv, func(replEffect[session.Session]) replEffect[session.TurnResult]] {
	envLens := MakereplEnvEnvLens()
	linesLens := MakereplEnvLinesLens()

	return F.Pipe2(
		F.Flow2(envLens.Get, runSession()),
		reader.Ap[effect.Kleisli[oai.ChatCompletionDeps, session.Session, session.TurnResult]](F.Flow2(linesLens.Get, session.ChannelSteering)),
		reader.Map[replEnv](RRIOE.Chain[oai.ChatCompletionDeps, context.Context, error, session.Session, session.TurnResult]),
	)
}

// reportTurnTap prints the report of a turn, see [reportTurn]. The REPL
// drops the error of a failed turn: it decides with [continueOrEnd].
func reportTurnTap() Reader[replEnv, endomorphism.Endomorphism[replEffect[session.TurnResult]]] {
	commandLens := MakereplEnvCommandLens()

	return F.Flow3(
		commandLens.Get,
		reportTurn(),
		RRIOE.TapIOK[oai.ChatCompletionDeps, context.Context, error, session.TurnResult, Result[int]],
	)
}

// turn runs a turn of a session, stops the background processes after a
// cancelled turn, reports the turn and decides the next step. The taps do
// not check the context, so a cancelled turn is reported too.
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
		reader.Map[replEnv](reader.Map[replEffect[session.Session]](RRIOE.Chain[oai.ChatCompletionDeps, context.Context, error](continueOrEnd()))),
	)
}

// The stages of [lineTurn], each a function of the change of a line.
type (
	sessionOf = func(replEffect[endomorphism.Endomorphism[session.Session]]) replEffect[session.Session]
	lineStep  = func(string) replEffect[replStep]
)

// lineTurn is the turn of a session for a line: the change of the line is
// applied to the session, then the turn runs.
func lineTurn() Reader[replEnv, func(session.Session) lineStep] {
	return F.Pipe3(
		F.Flow2(withLine(), reader.Local[replEffect[session.Session], replEffect[endomorphism.Endomorphism[session.Session]], string]),
		reader.Map[replEnv](reader.Compose[lineStep, sessionOf, func(string) replEffect[session.Session]]),
		reader.Ap[func(sessionOf) lineStep](F.Flow2(turn(), reader.Map[string, replEffect[session.Session], replEffect[replStep]])),
		reader.Map[replEnv](reader.Compose[lineStep](applyTo())),
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
// like a one-shot turn; after a cancelled turn the background processes are
// stopped and the REPL ends with the cancellation. At the end of the input,
// or when the context is cancelled while it waits for a line, the REPL ends
// without an error. A failure to build the first request is printed on the
// standard error; the turns report their own errors.
//
// The steps are joined by readerreaderioeither's Chain and TailRec, which do
// not check the context, so a cancelled context ends a waiting REPL with
// success and a running turn with its report.
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

// replOf is the REPL of a command, over a channel of lines and the
// environment of a mode, see [newReplEnv].
func replOf() Reader[*C.Command, Reader[lineQueue, Reader[modeEnv, replEffect[F.Void]]]] {
	return F.Flow2(
		F.Curry3(newReplEnv),
		reader.Map[lineQueue](reader.Map[modeEnv](repl())),
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

// replWithLines is the REPL of a command over the environment of a mode,
// once the reader of its lines is started, see [scanCommand].
func replWithLines() Reader[*C.Command, ioresult.Kleisli[modeEnv, replEffect[F.Void]]] {
	withLines := F.Flow3(
		scanCommand(),
		reader.Read[IOResult[replEffect[F.Void]], IOResult[lineQueue]],
		reader.Compose[IOResult[replEffect[F.Void]]](ioresult.Map[lineQueue, replEffect[F.Void]]),
	)

	return F.Pipe2(
		F.Flow2(replOf(), F.Flip[lineQueue, modeEnv, replEffect[F.Void]]),
		reader.Map[*C.Command](reader.Compose[IOResult[replEffect[F.Void]], modeEnv, func(lineQueue) replEffect[F.Void]]),
		reader.Ap[ioresult.Kleisli[modeEnv, replEffect[F.Void]]](withLines),
	)
}

// makeRepl is wiring: it looks the mode of a configuration up and builds its
// REPL over the lines of the reader of a command, the prompt of the
// arguments first. The reader goroutine starts only after the lookup.
func makeRepl() Reader[*C.Command, ioresult.Kleisli[Config, replEffect[F.Void]]] {
	return F.Flow3(
		replWithLines(),
		ioresult.Chain[modeEnv, replEffect[F.Void]],
		reader.Compose[IOResult[replEffect[F.Void]]](makeModeEnv()),
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
// and returns the REPL of its mode for a command against DeepSeek, with the
// DeepSeek deps reading their API key from the same variables, see
// [provideDeepSeek] and [liftDeepSeek]. The variables are read twice: for the
// configuration and for the deps; the command for the REPL and for the
// printer of a failure to build the deps.
func replProgram() Reader[*C.Command, ioresult.Kleisli[Record[string, string], ReaderIOResult[F.Void]]] {
	provide := F.Flow5(
		liftDeepSeek(),
		reader.Local[ReaderIOResult[F.Void], deepSeekRepl, replEffect[F.Void]],
		reader.Map[Record[string, string], func(deepSeekRepl) ReaderIOResult[F.Void], runDeepSeek],
		reader.Read[Reader[Record[string, string], runDeepSeek]](provideDeepSeek[F.Void]()),
		reader.Map[Record[string, string]](ioresult.Map[replEffect[F.Void], ReaderIOResult[F.Void]]),
	)

	program := F.Flow3(
		makeRepl(),
		ioresult.Chain[Config, replEffect[F.Void]],
		reader.Compose[IOResult[replEffect[F.Void]]](F.Flow2(decodeConfig(), ioresult.FromResult[Config])),
	)

	return F.Pipe1(
		F.Flow2(program, reader.Ap[IOResult[ReaderIOResult[F.Void]], Record[string, string], IOResult[replEffect[F.Void]]]),
		reader.Ap[ioresult.Kleisli[Record[string, string], ReaderIOResult[F.Void]]](provide),
	)
}

// runRepl runs the REPL of the command under the configuration of its run.
// An error before the REPL starts is printed on the standard error; the
// turns report their own errors.
func runRepl() Reader[*C.Command, ReaderIOResult[F.Void]] {
	setup := F.Pipe1(
		F.Flow2(replProgram(), ioresult.Chain[Record[string, string], ReaderIOResult[F.Void]]),
		reader.Ap[IOResult[ReaderIOResult[F.Void]]](loadEnv()),
	)

	return F.Pipe2(
		F.Flow2(printError(), ioresult.TapLeft[ReaderIOResult[F.Void], int]),
		reader.Ap[IOResult[ReaderIOResult[F.Void]]](setup),
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

// runCommand runs the REPL or answers the one prompt of the command, see
// [isInteractive].
func runCommand() Reader[*C.Command, ReaderIOResult[F.Void]] {
	return P.Fold(
		F.Flow2(printResult(), thunk.MapTo[int](F.VOID)),
		runRepl(),
	)(isInteractive())
}
