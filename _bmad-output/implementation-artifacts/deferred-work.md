# Deferred work

## Deferred from: code review of E3 Steering (2026-10-23)

- **Steered messages are lost when the request after the poll fails** (`session/loop.go` `Run`).
  `Steer` drains the queue into the session, then `Next` fails (HTTP error, cancellation). `Run`
  returns only the error, so the session and the drained user messages are gone. The session
  itself was already lost on failure before E3; E4 ("return the partial session as a `Land`
  result") is the place to keep both.
  **Resolved in E4 (2026-10-24):** `Run` never fails; it returns a `TurnResult`, the session as
  far as the turn got paired with its outcome. A failed request lands with the session after
  `Steer`, so the drained messages are in its request (`TestRun_RequestErrorKeepsSteeringMessages`).
- source_spec: `_bmad-output/implementation-artifacts/spec-5-4-e4-cancellation-and-the-partial-session.md`
  summary: `ChannelSteering` should land with the messages it already drained when the context is cancelled mid-drain, instead of failing and losing them.
  evidence: `tryReceive` returns `Left(context.Cause(ctx))` on `ctx.Done` and `thunk.TailRec` checks ctx before each receive, so messages taken before the cancel are in neither the session nor the queue; matters once E5/ACP use `ChannelSteering` (the one-shot CLI uses `NoSteering`). `TestChannelSteering_Cancelled` codifies today's behavior.
- source_spec: `_bmad-output/implementation-artifacts/spec-5-4-e4-cancellation-and-the-partial-session.md`
  summary: Add an end-to-end test that runs a failed or cancelled turn through `MakePromptCommand` and asserts one stderr report, empty stdout and the error for the exit code.
  evidence: `reportTurn` and the pre-turn `runReported` are tested separately, but the DeepSeek base URL is fixed in `openai/deepseek.go`, so a turn that sends a request can't run through the command in a test without an injectable client or base URL.
- source_spec: `_bmad-output/implementation-artifacts/spec-5-4-e4-cancellation-and-the-partial-session.md`
  summary: Document in AGENTS.md (Composing `Effect[C, A]`, combinators table) that a cancellation-safe loop or tap uses `readerreaderioeither.Chain` / `TailRec` / `TapIOK`, and align the fp-go skill checklist line "loops with `effect.TailRec`".
  evidence: `effect.Chain`, `effect.TailRec` and `effect.TapIOK` check the context and have no non-checking variant; the exception is only in `.bob/skills/fp-go/SKILL.md` (Loops), so AGENTS.md still reads as forbidding it.
- source_spec: `_bmad-output/implementation-artifacts/spec-5-5-e5-interactive-repl.md`
  summary: E5b — Ctrl-C stops only the running REPL turn: a per-turn child context cancelled by an interrupt channel, the signal wiring moved from `main.go` into the command actions (one-shot keeps `NotifyContext` + `AfterFunc`), and the REPL continues on the partial session; Ctrl-C at the prompt exits 0.
  evidence: Split from E5 at the user's request for smaller steps; E5a ends the REPL on a cancellation, so this is a separate, shippable change on top of it.
- source_spec: `_bmad-output/implementation-artifacts/spec-5-5-e5-interactive-repl.md`
  summary: E5c — `--task <id>` for the REPL: load the saved task (`session.Load`) and continue it, save after every turn under the id (`session.Save`, a failed save ends the REPL), and without `--task` create a new id (`crypto/rand.Text()`) and print it on stderr. Decision (Q1): an unknown id fails with its error, exit 1. Test: scripted stdin + replay deps, restart with the printed id continues the history.
  evidence: Split from E5 at the user's request for smaller steps; persistence is independent of the turn loop of E5a and of the per-turn cancellation of E5b.
- source_spec: `_bmad-output/implementation-artifacts/spec-5-5-e5-interactive-repl.md`
  summary: Stop the background processes of all REPL turns when the REPL ends normally (end of input, Ctrl-C at the prompt), not only after a cancelled turn.
  evidence: `turn` taps `stopProcesses` only for a cancelled turn; a background command of an earlier turn outlives the REPL exiting with 0.
- source_spec: `_bmad-output/implementation-artifacts/spec-5-5-e5-interactive-repl.md`
  summary: Report a stdin read error (`scanner.Err()`) in the REPL instead of treating it as the end of input.
  evidence: `scanLines` closes the channel on any `Scan` failure; lines up to 1 MiB are read since the review, beyond that or on an I/O error the REPL exits 0.
- source_spec: `_bmad-output/implementation-artifacts/spec-5-5-e5-interactive-repl.md`
  summary: E5c must check that `session.RunPrompt` skips the first poll for a loaded task, whose iteration count may not be 0.
  evidence: unverified (maybe-false, medium): `RunPrompt` gates on `iterations == 0`, which `Continue` and `MakeSession` set; settle with a test continuing a `session.Load`ed task.
