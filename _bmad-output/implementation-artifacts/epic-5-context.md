# Epic 5 Context: Conversational

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Turn the one-shot agent into one a user can hold a conversation with: several turns over one session, tasks saved and resumed, messages added while a turn runs, Ctrl-C that stops the running turn but keeps what it has done, an interactive REPL in the terminal, and tools whose result can change the session. This is milestone M4. It matters beyond the CLI: the partial-session result (E4) and session-changing tools (E6) are hub designs that ACP, A2A, the MCP server, todos and mode switching all build on, so their shape must be right the first time. E1 and E2 are done. E3 is done too: the planning docs still say "review", but PR #69 merged it. Open: E4, E5, E6. There is no separate PRD or architecture document; the requirements and design decisions come from the roadmap plan and the repository's coding rules.

## Stories

- Story 5.1: E1 Multi-turn (done)
- Story 5.2: E2 Persistence (done)
- Story 5.3: E3 Steering (merged; sprint status still says review)
- Story 5.4: E4 Cancellation and the partial session
- Story 5.5: E5 Interactive REPL
- Story 5.6: E6 Session-changing tools

## Requirements & Constraints

- **Cancellation:** the `context.Context` of the turn's `Thunk` reaches the HTTP request and the running tools. Cancelling it aborts the request in flight, kills foreground commands, and stops background processes started by the session (open since C6), which then leave the process table.
- **Partial session:** when a turn is cancelled or its request fails with an infrastructure error, the caller still gets every completed step, the steered messages already drained and the usage so far. The outcome (cancelled, or the error) comes back as a value the caller can match on. A cancellation is an outcome, not a failed `Effect`. This also fixes the E3 finding that drained steered messages are lost when the next request fails. Mark that deferred-work entry resolved.
- **CLI Ctrl-C:** after the turn stops, the CLI prints the partial answer and usage to stderr and exits with code 130. A second Ctrl-C exits at once.
- **REPL:** runs without a prompt or with `--interactive`. Each line is one turn: the first comes from the mode's request, the later ones go through `Continue` → `Run`, and each answer and its usage are printed. A line typed while a turn runs is queued as steering. Ctrl-C stops only the current turn, and the REPL continues on the partial session. Ctrl-C at an empty prompt, or end of input, exits. `--task <id>` loads and continues a saved task. Without it, a new id is created and printed. The session is saved after every turn.
- **Session-changing tools:** a second kind of tool returns the text plus an `Endomorphism[Session]`. `Next` appends the tool messages first, then applies the endomorphisms in tool-call order, whatever order the concurrent calls finish in. If such a tool fails, its result is a tool message with the identity endomorphism. Plain tools lift into the new kind with the identity, and every existing tool test passes unchanged.
- Tool failures are data (tool messages). Only infrastructure errors (network, auth, cancellation) use the error channel.
- Loops are stack-safe (`effect.TailRec` / `tailrec`), never recursion inside an effect.
- Tests run offline with fake Deps, plus replay `ChatCompletionDeps` where it applies. E4: a fake completion that blocks until its context ends, plus a fake process. E5: scripted stdin. E6: a fake tool that sets a session field through a lens.
- Done means: `go generate ./...`, `runCodeActions.bat` (PowerShell), `go vet ./...` and `go test ./...` pass. README.md is updated and the PLAN.md checkbox ticked in the same PR. One story is one PR, with an emoji PR title and plain conventional commits.

## Technical Decisions

- **Session API in place:** `session.Continue(text)` is an `Endomorphism[Session]` that resets the per-turn iteration counter. `Steer()` appends polled texts and keeps the counter. `SteeringDeps.GetPoll()` is embedded in `SessionDeps`. `ChannelSteering(queue)` drains a channel without blocking, for the REPL. The one-shot CLI uses `NoSteering()`.
- **Persistence:** `Save` is a `Kleisli[TaskStoreDeps, Pair[TaskID, Session], Void]` and `Load` is a `Kleisli[TaskStoreDeps, TaskID, Session]`. Tasks are JSON files under `~/.fp-go-harness/tasks`, converted through `SessionIso()` and `SessionDTO`. A task id must be a plain file name, and an unknown id fails with `fs.ErrNotExist`. The id belongs to the caller (CLI, ACP `sessionId`, A2A task), not to the session.
- **The REPL loop lives at the edge:** an `IO` loop through `tailrec` in `main.go` / `cli`, not inside an effect. The CLI wires Ctrl-C through `signal.NotifyContext`.
- **Read-only inside effects:** `Ap`, `Concat` and `Traverse*` run concurrently, and CI runs `go test -race`. Mutable state, such as the background process table and cancel functions, lives at the edge behind a `sync.Mutex`, never behind a `Deps` getter. Test fakes keep no state.
- **One tool type for all wrappers:** the E6 tool type must be the single type that every tool-call wrapper works on. Today that is `Truncate` (C11) and `SelectTools` (D5). Later it includes I1's approval wrapper, F2's event `Tap`, the G3 MCP and K7 A2A bridges, H3 `update_todo_list`, H5 `switch_mode`, and J6 ACP `set_mode`.
- **fp-go conventions apply** (AGENTS.md, the `fp-go` and `go-conventions` skills): Deps as getter interfaces, the `effect` package's own combinators, readers instead of closures, generated lenses, and the `A.Kleisli` / `A.Operator` aliases. Check signatures with `go doc` or the fp-go MCP server.

## Cross-Story Dependencies

- Merge order: E4 changes `Run`, which E3 also touched. E3 is now merged, so E4 can start.
- E5 needs E1 and E2 (done), E3 (steering), and E4 (Ctrl-C stops the turn, not the process). E5 also benefits from F2's event sink (Epic 6).
- The E4 result shape has named consumers, and its spec must say what each needs:
  - E5 continues the partial session.
  - J1 returns it.
  - J3 maps it to `cancelled`.
  - J5 keeps it in the ACP session table.
  - K3 and K5 map it to `canceled`.
  - G6 ends a cancelled MCP request with it.
- J1 (turn as a value) comes after E4. F2, I1 and G3 come after E6, because they wrap the tool call that E6 changes.
- H3 and H5 are built on E6. H4 (`ask_followup_question`) needs a front end that can answer, and E5 is the first.
