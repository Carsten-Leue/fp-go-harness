---
title: 'E5a Interactive REPL: turns and steering'
type: 'feature'
created: '2026-10-27'
status: 'done'
baseline_commit: 'b149bbbc161421fe7fa6f456c0e948759e939dc1'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/AGENTS.md'
  - '{project-root}/.bob/skills/fp-go/SKILL.md'
  - '{project-root}/.bob/skills/go-conventions/SKILL.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** The CLI answers one prompt and exits. A user cannot hold a conversation over several turns or steer a running turn, although `Continue`, `ChannelSteering` and `TurnResult` exist.

**Approach:** Without a prompt, or with `--interactive`, the command runs a REPL: an `IO` loop through `tailrec` at the edge over a `Session`. The session starts from the mode's request; each stdin line is wrapped with the turn's environment_details, applied with `session.Continue`, run with `session.Run` and `ChannelSteering` over the same stdin line channel, and reported like the one-shot turn.

## Boundaries & Constraints

**Always:** One line channel, fed by one stdin reader goroutine (a leaf), is both the prompt source and the steering queue. **Decision (Q2):** lines typed after a turn's last request stay in that channel and become the next turn's line. A blank line is skipped. The prompt marker `> ` goes to stderr, answers and stats to stdout, failed-turn reports to stderr (`reportTurn`). A failed turn other than a cancellation (request error, iteration limit) is reported and the REPL continues on `pair.Head` of its `TurnResult`. A cancelled turn (Ctrl-C) is reported, background processes are stopped by the existing tap, and the REPL ends with the cancellation (exit 130, as the one-shot CLI). End of input, or a cancelled context while waiting for a line, exits with 0. A prompt given with `--interactive` is the first line. `--interactive` is a command flag, not a `Config` variable. Mutable state (the stdin goroutine) stays in named leaves.

**Never:** No per-turn Ctrl-C (E5b), no `--task` or persistence (E5c). No line editing / raw terminal mode. No change to `main.go`, `Run`, `Steer`, `ChannelSteering`. No imperative loop in the REPL body; no `ApSeq`/`Chain` to dodge races.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior |
|----------|--------------|---------------------------|
| Two lines | `a\nb\n`, EOF | two turns; 2nd request carries 1st answer and `b`; two answers + stats on stdout; exit 0 |
| Steering | line arrives while a turn runs, before its next request | that request carries it after the tool result |
| Late line | line arrives after the turn's last request | it is the next turn's line |
| Request error | completion fails | report on stderr, next line continues the partial session |
| Ctrl-C in turn | ctx cancelled while a request blocks | report on stderr, REPL ends with the cancellation |
| EOF / Ctrl-C at prompt | waiting for input | exit 0 |
| One-shot | `fp-go-harness "prompt"` | unchanged |

</frozen-after-approval>

## Code Map

- `cli/run.go` -- `answer(cfg, registry)` builds `SessionDeps` with `NoSteering` and lifts with `LocalEffectK(MakeDeepSeekChatCompletionDeps)`. Extract `runSession(cfg, registry, poll)` over `oai.ChatCompletionDeps` (`MakeSession` stays outside), so `answer` = `MakeSession` → `runSession(NoSteering)` → lift, and the REPL reuses it. `ask` builds `request` (mode request) and `details` (env details over the mode name) and taps `stopWhenCancelled` with `RRIOE.TapIOK`; reuse those pieces. The initial REPL request needs the model set as `withPrompt` does. `reportTurn` prints a turn and fails on Left; the REPL drops that error. `isCancelledTurn` decides whether the REPL ends.
- `cli/command.go` -- `MakePromptCommand`, `configFlags`; add `--interactive`; the Action picks one-shot or REPL (no args → REPL). Read stdin from `cmd.Root().Reader` (default `os.Stdin`) for tests.
- `session/continue.go`, `session/steering.go` (`ChannelSteering`, `MakeSteeringDeps`) -- unchanged.
- `replay/completion.go` -- `MakeReplayChatCompletionDeps()([]ChatResponse)` for tests; `cli/cancel_test.go` -- `redirect` and fake-server pattern for a blocking or tool-calling completion; `cli/command_test.go` -- `withHome`, `TestPromptCommand_MissingPrompt` changes (no prompt now starts the REPL).
- Windows console: Ctrl-C makes a pending `ReadConsole` return 0 bytes without error, so the reader goroutine is not ended by it.

## Tasks & Acceptance

**Execution:**
- [x] `cli/run.go` -- extract `runSession`; `answer` built on it -- REPL reuses the turn.
- [x] `cli/repl.go` -- leaf `scanLines(io.Reader, first Option[string]) <-chan string` (goroutine, closes on EOF); leaf `nextLine(lines)` as a `Thunk[Option[string]]` (select: line → Some, closed or ctx done → None); per-line turn effect (details + `Continue` → `runSession(ChannelSteering(lines))` with the process-stop tap); step = prompt marker → `nextLine` → turn → `reportTurn` → `Bounce` or `Land`, under a stack-safe `TailRec` over `Session`. The chat deps are a parameter, DeepSeek in the command, replay or fake server in tests -- core.
- [x] `cli/command.go` -- flag and mode selection -- entry.
- [x] `cli/repl_test.go` -- every matrix row: scripted stdin + replay deps; steering and late line with a fake completion (stateless, tells requests apart by message count) and a buffered lines channel; request error; cancellation with a completion that blocks until its ctx ends.
- [x] `cli/command_test.go` -- adapt `MissingPrompt` (empty stdin → REPL exits 0).
- [x] `README.md`, `PLAN.md` (E5 description, E5a done; tick E5 only after E5c), `sprint-status.yaml` (5-4 done, 5-5 in progress) -- docs.

**Acceptance Criteria:**
- Given the change, when `go generate ./...`, `runCodeActions.bat`, `go vet ./...`, `go test ./...` run, then all pass and every fake is read-only or mutex-guarded.

## Implementation Notes

- **`session.RunPrompt`.** `Run` polls the steering before every request, the first included, so a line queued before a turn (piped `a
b
`) was sent with the first request and the matrix row *Two lines* failed (one turn). `RunPrompt` is `Run` without the poll at iteration zero, which `Continue` and `MakeSession` set: a line queued before the turn waits for the second request (*Steering*) or for the next turn (*Two lines*, *Late line*). It is a gate on the iteration lens, no state. `Run` keeps its behavior; its body moved into `runWith(steer)`, shared by both. The CLI uses `RunPrompt` for one-shot and REPL turns (identical under `NoSteering`). Tests: `session/steering_test.go` `TestRunPrompt_*`.
- **Nullary and point-free wiring.** At the user's request every function of `cli/run.go` and `cli/repl.go` is nullary and point-free; the parameters moved into two environments with generated lenses ([cli/modeenv.go](../../cli/modeenv.go)): `modeEnv` (config, mode, mode deps) for `makeRegistry`, `sessionDeps`, `runSession`, `answer`, `modeRequest`, `modeDetails`, `ask`, `stopProcesses`, and `replEnv` (mode env, command, line channel) for the REPL stages. `withMode` / `modeAnswer` are replaced by `lookupMode`, `modeEnvOf` and `makeModeEnv`; `makeProgram` and `replProgram` share `provideDeepSeek`. The channel field is typed by the alias `lineQueue = <-chan string`, because the lens generator types an unnamed channel field as `any`. `printLine`, `printErrLine`, `printErrorLine` and `joinParagraphs` are gone (`io.WriteString`, `S.Append`, `S.Format`, `S.Intersperse`). Leaves that remain, each named as such: the constructors `newDeepSeekDeps`, `newModeEnv`, `newReplEnv`; the `C.Command` accessors `rootWriter`, `rootErrWriter`, `rootReader` (function-typed fields, no lenses); `scanLines` (goroutine), `nextLine` (blocking select); the formatters `sessionStats`, `joinFailure`.
- **Cancel at the prompt.** `TestRepl_CancelAtPrompt` cancels when the prompt is written (a mutex-guarded writer), as Ctrl-C at the prompt does. A context cancelled before the REPL starts fails reading the mode's request, like the one-shot CLI.
- `TestPromptCommand_MissingPrompt` now runs the REPL on an empty stdin: exit 0, one prompt marker, no request; it sets a dummy `DEEPSEEK_API_KEY` because the DeepSeek deps are built before the loop.
- **Review patches.** The REPL prints a failure to build the DeepSeek deps (a missing `DEEPSEEK_API_KEY`) through `liftDeepSeek`, as the one-shot CLI does; before, it exited 1 silently. `scanLines` reads lines up to 1 MiB. `continueOrEnd` validates with `result.FromPredicate` and lifts once; `effect.TapLeftIOK` / `effect.FromIO` replace their `readerreaderioeither` spellings (same behavior, no context check); `file.Join` replaces the `joinPath` leaf; type parameters are named `T`.

## Review Triage Log

| # | Source | Finding | Verdict | Route | Evidence |
|---|---|---|---|---|---|
| 1 | edge | DeepSeek deps failure (no API key) exits 1 without a message | medium | patch | Reproduced with the built binary: `exit=1`, empty stderr. Fixed by `liftDeepSeek`'s `TapLeftIOK(printError)`; `TestPromptCommand_ReplMissingAPIKey`. |
| 2 | blind, edge | `scanLines` ends silently on a line > 64 KiB or a read error | medium | patch | `bufio.Scanner` default limit; a pasted prompt ends the REPL with 0. Buffer raised to 1 MiB, `TestScanLines_LongLine`. Reporting `scanner.Err()` would need a second channel → deferred. |
| 3 | blind, edge | Background processes of earlier turns not stopped when the REPL ends with 0 | medium | defer | Real, but the intent stops processes only after a cancelled turn (as the one-shot CLI); at exit the processes outlive the CLI exactly as in one-shot mode. |
| 4 | blind, edge | Reader goroutine leaks after the REPL ends | low | reject | Process exits right after the REPL; the spec makes the goroutine the one stdin reader leaf. Tests leaking a blocked goroutine on a finished reader is negligible. |
| 5 | blind | No-arg run now waits on stdin (breaking change) | false | reject | Frozen intent: "Without a prompt … the command runs a REPL". |
| 6 | blind | Failed turns don't affect the exit code | false | reject | Frozen intent: "End of input … exits with 0", failed turns continue. Now stated in README. |
| 7 | blind | Usage text of `promptOf` outdated | low | patch | Message updated to `[--interactive] [<prompt>]`. |
| 8 | blind, fp-go | README REPL bullet swallows the mode sub-list | medium | patch | AGENTS.md README rule; bullet moved below the mode list. |
| 9 | blind, verif | No test for `--interactive` dispatch and first line | medium | patch | Verified gap; `TestRunCommand_Dispatch`. |
| 10 | blind, verif | No test that a cancelled REPL turn stops background processes | medium | patch | Verified gap; `TestRepl_CancelStopsBackgroundProcesses`. |
| 11 | blind | Steering lines are not wrapped with environment_details | low | patch | Documented in README (plain user messages); behavior of `ChannelSteering` unchanged by intent. |
| 12 | blind | README late-line wording imprecise | low | patch | Reworded to "after the turn's last poll". |
| 13 | blind | Failed line leaves two consecutive user messages | low | reject | Intent: continue on `pair.Head` of the partial session; DeepSeek accepts it (test passes against the API shape). |
| 14 | blind | `RunPrompt` depends on `iterations == 0` | maybe-false | defer | Holds for `Continue`/`MakeSession`; a loaded task (E5c) must be checked → noted for E5c. |
| 15 | blind | Generated optics for unexported types / unused | false | reject | AGENTS.md: field access through generated lenses; generator output is left as written. |
| 16 | blind | PLAN E5b overlap and missing `main.go` note | low | patch | E5b text corrected; E5a test list updated. |
| 17 | edge | Ctrl-C with a queued line may start a cancelled turn (exit 130 instead of 0) | low | reject | Needs a line queued and Ctrl-C at the prompt at once; the turn is then reported as cancelled, which is also a correct reading. A guard adds a branch. |
| 18 | edge | Whitespace-only `--interactive " "` becomes a turn | low | reject | Negligible in use; fix adds a filter. |
| 19 | edge | `joinParagraphs` removal changes output for empty content | false | reject | `S.Intersperse` drops the separator only for an empty side; an empty answer printing only the stats is the monoid-lawful output, and `TestRepl_TwoLines` pins the normal case. |
| 20 | edge | Spec says `session.Run`, code uses `RunPrompt` | false | reject | Recorded in the Spec Change Log 2026-10-29. |
| 21 | edge | `Run`'s body rewritten despite "no change to Run" | false | reject | Behavior unchanged, recorded in the Spec Change Log; `session` tests for `Run` pass. |
| 22 | verif | No test for `modeDirs` order | medium | patch | Verified gap; `TestModeDirs`. |
| 23 | fp-go | `RRIOE.TapLeftIOK` where `effect.TapLeftIOK` exists | medium | patch | `effect.TapLeftIOK` → `context/readerreaderioresult.TapLeftIOK` → `RRIOE.TapLeftIOK`, no context check. |
| 24 | fp-go | `RRIOE.FromIO` where `effect.FromIO` exists | medium | patch | Same chain, verified in the module source. |
| 25 | fp-go | `P.Fold` choosing `Of`/`Fail` in `continueOrEnd` | medium | patch | fp-go skill: no `Fold` choosing Left/Right; `result.FromPredicate` + `effect.FromResult`. |
| 26 | fp-go | `joinPath` leaf where `file.Join` exists | medium | patch | `file.Join(name)(root)`; used elsewhere in the repo. |
| 27 | fp-go | Type parameters named `D` | medium | patch | AGENTS.md *Two build breaks*: name type parameters `T`. |

## Spec Change Log

- 2026-10-29 (implementation; intent unchanged): added `session.RunPrompt` next to `Run` (Code Map listed the session package as unchanged) to meet the *Two lines* and *Late line* rows deterministically; `Run`, `Steer` and `ChannelSteering` keep their behavior.

## Verification

**Commands:**
- `go generate ./...; go vet ./...; go test ./...` -- expected: pass
- `runCodeActions.bat .` (PowerShell) -- expected: no changes left
