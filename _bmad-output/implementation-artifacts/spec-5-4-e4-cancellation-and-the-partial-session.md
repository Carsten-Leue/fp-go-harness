---
title: 'E4 Cancellation and the partial session'
type: 'feature'
created: '2026-10-24'
status: 'done'
baseline_commit: '2690b7613377564b955822c786d606dcca4836bd'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/AGENTS.md'
  - '{project-root}/.bob/skills/fp-go/SKILL.md'
  - '{project-root}/.bob/skills/go-conventions/SKILL.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** When a turn is cancelled (Ctrl-C) or a request fails, `session.Run` fails the `Effect` and the session is lost: completed steps, usage and steered messages already drained (E3 deferred finding). Background processes started by `execute_command` keep running.

**Approach:** `Run` never fails: it returns a `TurnResult`, the session as far as the turn got paired with an `Outcome` (`Result[*ChatCompletion]`: the final completion, or the error that ended the turn). The CLI prints the partial answer and usage on stderr, stops the background processes on cancellation, and exits with 130.

## Boundaries & Constraints

**Always:** Every failure inside `Run` (iteration guard, poll, request) lands with the session it had at that point. A cancellation is `errors.Is(err, context.Canceled)`, which also matches the `signal.NotifyContext` cause. Mutable state (process table) stays at the edge. The `TurnResult` doc comment names its consumers: E5 continues `pair.Head`; J1 returns it; J3 maps Right by finish reason, cancelled → `cancelled`, `MaxIterationsError` → `max_turn_requests`, other Left → JSON-RPC error; J5 keeps the head in the ACP session table; K3/K5 map cancelled → `canceled`, others → `failed`; G6 ends a cancelled MCP request with the partial answer.

**Decision (Q1):** A background process is stopped by killing only the shell process in the table (`os.Process.Kill` of its pid, one portable leaf); its children may survive.

**Never:** No REPL (E5). No process-tree or process-group kill. Background processes are not stopped on a normal exit, nor bound to a tool call's context (fp-go's parallel `Ap` cancels its sub-context once the call returns). No imperative loops, no `ApSeq` to dodge races.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior |
|----------|--------------|---------------------------|
| Answer | model answers | `Pair(session, Right(completion))`, stdout as today, exit 0 |
| Cancel during a tool | ctx cancelled while a tool runs | tool message carries the error; next request fails; Left(cancelled), history has every round trip whose request returned |
| Cancel during request | ctx cancelled in request N | Left(cancelled), session = input of request N incl. steered messages |
| Request error | HTTP error after a poll drained messages | Left(err), steered messages in `current` |
| Iteration limit | limit reached | Left(`MaxIterationsError`) with the session; stderr partial + stats, exit 1 |
| CLI Ctrl-C | first Ctrl-C | turn stops, background processes stopped and gone from the table, stderr partial answer + stats, exit 130 |
| Second Ctrl-C | while stopping | default signal handling, process exits at once |

</frozen-after-approval>

## Code Map

- `session/loop.go` -- `Run`: `guard → Steer → Next` under `effect.TailRec`. Becomes `effect.Kleisli[SessionDeps, Session, TurnResult]`.
- `session/session.go` -- `Next`, `FinalResult`, `NextStep` stay; Next's `Land(FinalResult)` is mapped to `Land(Pair(s, Right(c)))` in `Run`.
- `session/continue.go` -- doc comment refers to `FinalResult` of `Run`; update to `TurnResult`.
- `session/steering.go` -- `Steer`, `ChannelSteering` (checks ctx), `NoSteering` (does not). Unchanged.
- `tools/process.go` -- `ProcessTable` (`IORef`), `startIn` registers and the waiter goroutine removes on exit. Add `StopProcesses() Reader[IORef[ProcessTable], IO[Void]]` that kills every process in the table.
- `cli/run.go` -- `answer`/`ask`/`modeAnswer`/`makeProgram`/`runPrompt`/`printResult`/`formatResult` carry `FinalResult`; switch to `TurnResult`. `ask` has `mode.ModeDeps` (embeds `tools.ToolsDeps` → `GetProcessTable`).
- `cli/command.go` -- `Action` runs `printResult()(cmd)(ctx)()`.
- `main.go` -- `signal.NotifyContext`; exits 1 on any error.
- fp-go v2.12.0 facts: `effect.Chain`/`Map`-then-`Chain` and `thunk.Chain`/`ChainLeft` wrap the continuation in `WithContextK` (skipped on a cancelled ctx). `effect.TailRec` checks ctx before each step. `effect.ChainLeft` (generic `readerreaderioeither`), `thunk.TapIOK`/`ChainFirstIOK` and `ioresult.*` do not.

## Tasks & Acceptance

**Execution:**
- [x] `session/loop.go` -- add `Outcome`, `TurnResult`, `IsCancelled() Predicate[error]`; rebuild `Run` so each stage recovers with its input session via `effect.ChainLeft`; join steer→next and loop with `readerreaderioeither.Chain` / `TailRec` (same type as `Effect`, no ctx check) so drained messages survive -- partial session.
- [x] `session/loop_test.go`, `session/steering_test.go` -- adapt to `TurnResult`; add the cancel test (fake completion: tool call, then a fake tool that blocks until its ctx ends and records it under a mutex, then a completion blocking until ctx done; the test cancels once the tool started), request-error-keeps-steering test, max-iterations-as-outcome test -- matrix rows.
- [x] `tools/process.go`, `tools/process_test.go` -- `StopProcesses`; test: start a real background command `sleep 30; exit 0` through `DefaultShell` (valid in sh and PowerShell; the second statement keeps sh from exec'ing straight into `sleep`, so the test kills the shell, not the sleeper), stop, table empties (`assert.Eventually`) -- C6 remainder.
- [x] `cli/run.go`, `cli/command.go`, `cli/run_test.go` -- `TurnResult` through the pipeline; on Left print to stderr, by one rule for every Left (cancel, request error, iteration limit): the error, then the text of the last assistant message that has text, searched across the whole session (history, then `current`; nothing when there is none), then the stats, and return the error; stop processes with a ctx-free combinator when `IsCancelled`, the only cancel-specific step in the CLI; test with a cancelled ctx.
- [x] `main.go` -- `context.AfterFunc(ctx, stop)` for the second Ctrl-C; the exit code is one fold over the outcome: `IsCancelled` → 130, any other error → 1, success → 0 (no separate `MaxIterationsError` case).
- [x] `README.md`, `PLAN.md` (tick E4), `_bmad-output/implementation-artifacts/deferred-work.md` (mark E3 entry resolved), `sprint-status.yaml` (5-3 done, 5-4) -- docs.

**Acceptance Criteria:**
- Given any run, when `Run` ends, then its `Effect` succeeds and its `Outcome` carries the error, if any.
- Given the change, when `go generate ./...`, `runCodeActions.bat`, `go vet ./...` and `go test ./...` run, then all pass and every fake is read-only or mutex-guarded.

## Implementation Notes

- Every error is printed once, by the CLI: a failed turn by its report (error, last assistant text, stats), an error before the turn (missing prompt, unknown mode, decoding, a context cancelled before the turn started) with `%+v` through a ctx-free `thunk.TapLeftIOK`; usage errors by urfave/cli itself. `main` no longer prints, it only folds the error into the exit code. Before, `main` printed the error after the report, which would have shown a turn's error twice (and already doubled urfave's usage errors).
- The assistant text of a request message goes through lenses generated for `ChatCompletionMessageParamUnion`, `ChatCompletionAssistantMessageParam` and `ChatCompletionAssistantMessageParamContentUnion` (added to the directive in `openai/doc.go`).
- No `landAnswer` converter: since fp-go v2.13.0 `Next` lands its answer as the `TurnResult` directly (`Next` is `effect.Kleisli[SessionDeps, Session, TurnStep]`), so `Run` uses it as its step; the `Trampoline` lenses are gone and `session/session_test.go` reads a step through `prism.FromLand` / `FromBounce`. At the user's request, AGENTS.md and both skills state that every function is nullary and point-free.
- The process stop sits in `cli.ask` as `readerreaderioeither.TapIOK` (ctx-free, same type as `Effect`); `effect.TapIOK` is built on `Chain` and would skip on the cancelled context. `cli/cancel_test.go` runs a cancelled agent-mode turn against a fake DeepSeek server and checks that the background process leaves the table.

## Spec Change Log

- 2026-10-24 (design round, before implementation; intent unchanged): Carsten confirmed that children of a killed shell, and background processes left after a normal exit, may keep running on Windows until E5, so Q1 stays. Asked to keep the implementation as free of special cases as sensible. Changes under Tasks: (1) the `StopProcesses` test uses `sleep 30; exit 0`, because `sh -c "sleep 30"` execs into `sleep` and would hide that only the shell is killed; (2) the partial answer is the last assistant text anywhere in the session, not "content of the last history completion", which is empty when the turn was cancelled during a tool call; one rule for every Left; (3) the exit code is one fold over the outcome.

## Review Triage Log

Iteration 0 (blind, edge-case, verification-gap, fp-go conformance):

| # | Finding | Verdict | Evidence | Route |
|---|---|---|---|---|
| 1 | `Next` joins `recordCompletion` and `dispatch` with `effect.Chain`, which checks ctx: a cancel after the response drops the completion, its usage and a final answer (blind, edge) | medium | `session/session.go` `Next` ends with `effect.Chain(dispatch)`; `effect.Chain` wraps in `WithContextK`. Contradicts matrix "history has every round trip whose request returned" | patch |
| 2 | `ChannelSteering` fails mid-drain on cancel, losing messages already received (blind, edge) | medium | `tryReceive` returns `Left(Cause)` on `ctx.Done`, `thunk.TailRec` checks ctx; pre-existing E3 semantics codified by `TestChannelSteering_Cancelled`; one-shot CLI uses `NoSteering` | defer |
| 3 | Cancellation relies on a stage checking ctx; with ctx-ignoring deps the loop runs to the limit (blind) | low | real deps (openai client, `execute_command`) honour ctx; fix adds a guard check | reject |
| 4 | `killPid` may kill a reused pid (blind) | low | window between table read and kill is tiny; fix changes `BackgroundProcess` | reject |
| 5 | Children of the shell survive (blind) | false | decided by the frozen intent, Q1 | reject |
| 6 | Processes only stopped after a cancellation; second Ctrl-C skips the stop (blind, edge ×2) | false | frozen intent: not stopped on normal exit; second Ctrl-C uses default handling | reject |
| 7 | `partialAnswer` may print an earlier turn's text after `Continue` (blind, edge) | low | the one-shot CLI has no `Continue`; the rule is the spec's own (history, then `current`) — fix edits the spec | reject |
| 8 | `assistantText` reads only string content (blind, edge) | low | the session's assistant messages come from `ToParam` of completions, which use string content | reject |
| 9 | `openai/gen.go` missing from the diff (blind) | false | excluded from the review diff on purpose; the file is regenerated in the tree and builds | reject |
| 10 | TurnResult→FinalResult unwrap repeated in three tests (blind) | low | test-only; fix adds public surface | reject |
| 11 | No test runs a failed turn through `MakePromptCommand` to assert a single stderr print (blind, verification-gap) | medium | DeepSeek base URL is fixed in `openai/deepseek.go`; parts tested separately | defer |
| 12 | `TestAsk_CancelStopsBackgroundProcesses` fragile (body match, goroutine without timeout) (blind) | low | openai-go JSON is deterministic; a hang still fails at the test timeout | reject |
| 13 | `IsCancelled` vs the real `NotifyContext` cause untested (blind) | false | `os/signal.signalError.Is(context.Canceled)` returns true (Go source) | reject |
| 14 | Cancelled tool message is raw "context canceled" (blind) | low | design choice for E5 | reject |
| 15 | `TurnResult` doc lists roadmap consumers (blind) | false | required by the frozen intent | reject |
| 16 | PLAN.md E4 test sentence garbled (blind) | false | the sentence reads correctly | reject |
| 17 | README doesn't say a second Ctrl-C skips the report and the process stop (blind) | low | true; direct doc fix | patch |
| 18 | Answer write failure exits 1 without a print (edge) | low | rare (closed stdout); fix adds a tap | reject |
| 19 | `%+v` decoding output moved into the CLI is untested (verification-gap) | medium | `TestPromptCommand_InvalidMaxIterations` asserts only `Error` | patch |
| 20 | Middle `try` around `steer` is indistinguishable from the outer one (verification-gap) | low | guard passes the session through; removing it changes the Design Notes nesting | reject |
| 21 | `joinFailure` folds `Option` to a slice by hand (fp-go) | medium | `A.FromOption` exists (go doc) — skill, combinator that matches | patch |
| 22 | `partialAnswer` concatenates two readers with `Map`/`Ap` (fp-go) | medium | `reader.ApplicativeMonoid(A.Monoid[string]())` exists (go doc); monoid over readers per skill | patch |
| 23 | `readerreaderioeither` exception documented only in the skill, not AGENTS.md (fp-go) | medium | fix edits agent-context files | defer |
| 24 | `cli/cancel_test.go` inlines `MakeSessionhistoryLens().Get` (fp-go) | medium | skill Rule 4, lenses in local variables | patch |

## Design Notes

Each stage is lifted by one reader `attempt: Kleisli[Session, NextStep] → Kleisli[Session, NextStep]` that reads the session twice (`reader.Ap`): once for the stage, once for `ChainLeft(Land(Pair(s, Left(err))))`. Nesting `attempt(guard >=> attempt(steer >=> attempt(next)))` makes every failure land with the session its stage received, because the inner stages never fail.

## Verification

**Commands:**
- `go generate ./... && go vet ./... && go test ./...` -- expected: pass
- `runCodeActions.bat .` (PowerShell) -- expected: no changes left
