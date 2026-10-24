---
status: final
stepsCompleted: [1, 2, 3, 4]
inputDocuments:
  - PLAN.md
  - AGENTS.md
  - .bob/skills/fp-go/SKILL.md
  - .bob/skills/go-conventions/SKILL.md
  - README.md
  - _bmad-output/implementation-artifacts/deferred-work.md
---

# fp-go-harness - Epic Breakdown

## Overview

This document breaks the roadmap of fp-go-harness into epics and stories. There is no separate PRD or
architecture document: [PLAN.md](../../PLAN.md) holds both the requirements (its open steps) and the design
decisions, and the coding rules live in [AGENTS.md](../../AGENTS.md) and the repository skills
[`fp-go`](../../.bob/skills/fp-go/SKILL.md) and [`go-conventions`](../../.bob/skills/go-conventions/SKILL.md).
Those rules are referenced here, not copied.

Requirement IDs keep the PLAN.md step IDs (`FR-E4` is step E4), so stories, PRs and the old plan stay traceable.

## Requirements Inventory

### Functional Requirements

**Core tools and prompt assembly, follow-ups (Phases C and D)**

- FR-C12: `grep` remainders of C4. Call `rg` when it is installed (through `ProcessDeps`, same arguments and output as the built-in search, which stays the fallback), and honour git's `core.excludesFile` alongside `.git/info/exclude` and the `.gitignore` files.
- FR-D9: Static sections missing from D7: `investigate_before_answering` and `engineering_discipline`, at their recorded positions; texts written without a recording say so.
- FR-D10: `user_custom_instructions` from a user-level file (left out when missing) and `auto_appended_context` (content and source decided in the story spec, left out when empty), so the D7 comparison only lacks the sections of H1 (`available_skills`) and H5 (`available_modes`).

**Session lifecycle (Phase E)**

- FR-E4: Cancellation. The `context.Context` of the `Thunk` reaches the request and the running tools; Ctrl-C cancels both, stops background processes started by the turn (open since C6), and the partial session comes back as a `Land` result instead of being lost (also keeps steered messages drained before a failed request, see deferred work of E3).
- FR-E5: Interactive REPL. `main.go` loops over stdin turns (`Continue` → `Run` → print) as an `IO` loop through `tailrec` at the edge; wires `ChannelSteering` (E3) and resumes a saved task (E2) from the command line.
- FR-E6: Session-changing tools. A second kind of tool returns text and an `Endomorphism[Session]` that `Next` applies after the tool messages are appended; plain tools lift into it with the identity.

**Streaming and observability (Phase F)**

- FR-F1: Streaming request. `openai.ChatCompletionStream()` is a Kleisli with the same signature as `ChatCompletion`, folding chunks with `openai.ChatCompletionAccumulator`; the two are swappable through `ChatCompletionDeps`.
- FR-F2: Event sink. `EventDeps.Emit(Event)` in `SessionDeps` reports assistant text with tool calls, tool call started (id, name, arguments), tool call finished (id, success, output) and, with F1, text and reasoning deltas. Tool events are a `Tap` in `MakeToolCall`; the CLI wires a no-op sink.
- FR-F3: Structured logging. `LogDeps` writes JSONL in the recorded record shape (`module`, `msg`, `taskId`, `data`) for `Starting agent loop`, `Tool invoked`, `Agent loop completed` and the request/response payloads, so Phase B replays the harness's own logs.
- FR-F4: Usage details. `MakeUsageMonoid` adds cache-read tokens; the cost per turn is printed.
- FR-B5: Replay fixtures (issue #26). Record 2–3 tasks of the harness itself with F3's logs on this repository, redact them with `replay.Redact`, commit them under `testdata/` and replay each with `session.Run`; replaces the original B5, whose recordings can't be committed.
- FR-F5: HTTP retry. A `RoundTripper` decorator in `http.HttpDeps` retries 429/5xx with backoff (fp-go `retry` if it fits).

**MCP client and server (Phase G)**

- FR-G1: MCP config. Parse `.mcp.json` and `~/.bob/settings/mcp.json`, workspace wins, precedence as a record monoid.
- FR-G2: stdio session. `MCPDeps` on `github.com/modelcontextprotocol/go-sdk`, `Connect: Kleisli[ServerConfig, Session]`; a failed start becomes `option.None` and is logged, then skipped.
- FR-G3: Tool bridge. `tools/list` becomes `ToolRegistry` entries `mcp__<server>__<tool>` calling `tools/call`, merged with the core registry by a registry monoid (core tools win on clashes).
- FR-G4: Dogfood. The harness connects this repository's `fp-go` server from `.mcp.json` and uses its tools while working on itself.
- FR-G5: Reconnect. Retry with backoff and a maximum number of attempts.
- FR-G6: Harness as an MCP server. `fp-go-harness mcp` serves one tool `run_task` (prompt, optional mode; ask by default, agent only with `--allow-agent-mode`) over stdio, returning the final answer, iteration and token counts; fixed `--workspace`; later a `session_id` (J1), progress notifications (F2) and cancellation (E4).

**Skills, todos, subtasks (Phase H)**

- FR-H1: Skill discovery. Scan `~/.bob/skills`, `~/.claude/skills`, `<ws>/.bob/skills`, … for `SKILL.md` front matter and render `<available_skills>`.
- FR-H2: `use_skill` tool returning the body of the skill file.
- FR-H3: `update_todo_list` keeps the todo list in `Session` (lens) and renders it into the next `environment_details`; a session-changing tool (E6).
- FR-H4: `ask_followup_question` through the event sink (F2) and an `InputDeps` that waits for user input inside the thunk; needs a front end that can answer (E5, J2 or K8).
- FR-H5: `switch_mode` as an endomorphism on `Session` swapping system prompt and tool set; shared with ACP `session/set_mode` (J6); also renders the `available_modes` section.
- FR-H6: `spawn_subagent` / `start_subtask` runs a nested `session.Run` with a fresh `Session` and narrower deps (`effect.Local`), returns its final text, reports through the same event sink.

**Safety and approval (Phase I)**

- FR-I1: Approval hook. `ApprovalDeps.Approve: Kleisli[ToolInvocation, bool]` wraps the write and exec tools (`tools.MakeTools` registers them unapproved today); a denial is a tool message.
- FR-I2: Allow and deny lists for commands (prefix match) and for paths outside the workspace.
- FR-I3: External-change detection. Snapshot file mtimes after each turn and report the diff as `<external_changes>` in `environment_details`.

**Agent Client Protocol (Phase J)**

- FR-J1: Turn as a value. The wiring of `cli/run.go` becomes a Kleisli over (workspace, mode, `Option[Session]`, prompt) → `FinalResult`, with tool and mode deps built per session; the CLI is one caller.
- FR-J2: `acp` subcommand serving ACP (`github.com/coder/acp-go-sdk`) on stdin/stdout: `initialize` (protocol 1, no `loadSession`, text and embedded resources), `session/new` (resolves `cwd`), `session/prompt` (one turn, one `agent_message_chunk`, `end_turn`).
- FR-J3: Stop reasons. `stop` → `end_turn`, `length` → `max_tokens`, `content_filter` → `refusal`, `MaxIterationsError` → `max_turn_requests`, cancelled → `cancelled`; other errors are JSON-RPC errors.
- FR-J4: Tool-call updates. An event sink sending `tool_call` (with `kind` by tool name and file `locations`) and `tool_call_update` (`completed`/`failed`, output); assistant text with tool calls as message chunks.
- FR-J5: ACP cancel. `session/cancel` calls the running turn's `CancelFunc`; the prompt returns `cancelled`, the partial session is kept.
- FR-J6: ACP modes. `session/new` returns `availableModes` (D8) and the current mode; `session/set_mode` uses the H5 endomorphism and sends `current_mode_update`.
- FR-J7: ACP permissions. An `ApprovalDeps` backed by `RequestPermission` (`allow_once`, `allow_always`, `reject_once`, `reject_always`); "always" remembered per session.
- FR-J8: ACP streaming. F2 deltas become `agent_message_chunk`s, reasoning `agent_thought_chunk`s.
- FR-J9: Editor-backed files and terminal. With client capabilities, `FileDeps` from `fs/read_text_file` / `fs/write_text_file` and `execute_command` through `terminal/create`; the tools don't change.
- FR-J10: `session/load` of tasks saved with E2.
- FR-J11: the `mcpServers` of `session/new`, connected and bridged with Phase G.
- FR-J12: `plan` updates from the todo list of H3.

**Agent2Agent protocol (Phase K)**

- FR-K1: `a2a` subcommand (`github.com/a2aproject/a2a-go/v2`) serving the agent card (modes as `skills`, `text/plain`, `streaming` once J8 is done) and JSON-RPC on `/`; `--addr`, `--workspace`.
- FR-K2: Executor. Text parts are the prompt, metadata may name the mode (ask by default); a turn (J1) on the session of the `contextId`; emits `submitted`, `working`, the answer as artifact, `completed`.
- FR-K3: Outcome mapping. `end_turn` → `completed`, `refusal` → `rejected`, `cancelled` → `canceled`, `MaxIterationsError` and infrastructure errors → `failed` with the error text.
- FR-K4: Progress. F2 events become `working` status updates through a channel bridge; with F1, deltas become artifact updates with `append`.
- FR-K5: A2A cancel. `Cancel` calls the running turn's `CancelFunc`.
- FR-K6: Auth and safety. Bearer token from the environment, declared in `securitySchemes`, checked by a `CallInterceptor`; no token → loopback only; agent mode only with `--allow-agent-mode`, write and exec under the I2 policy.
- FR-K7: Client tools. Remote agents from a config file become tools `a2a__<agent>` (text in, final artifact text out, one `contextId` per harness session), merged into the registry like MCP tools; the card's description is the tool's.
- FR-K8: `input-required` for approvals and `ask_followup_question`, with the task suspended and persisted (E2).
- FR-K9: push notifications of task state changes.
- FR-K10: a task store that survives restarts, built on E2.

### NonFunctional Requirements

- NFR1: Every Go change follows the rules of [AGENTS.md](../../AGENTS.md), [`fp-go`](../../.bob/skills/fp-go/SKILL.md) and [`go-conventions`](../../.bob/skills/go-conventions/SKILL.md): Deps as getter interfaces, `effect` combinators, readers over closures, point-free and nullary style, generated lenses, `A.Kleisli`/`A.Operator` aliases, combinators checked with `go doc` or the fp-go MCP server, never from memory.
- NFR2: Everything inside an effect is read-only, because `Ap`, `Concat` and `Traverse*` run concurrently; CI runs `go test -race`. Mutable shared state (ACP and A2A session tables, cancel functions, the background process table) lives at the edge, guarded by a `sync.Mutex`, never behind a `Deps` getter.
- NFR3: Tool failures are data (a tool message), never a failed `Effect`; only infrastructure errors (network, auth, cancellation) use the `Result` error channel.
- NFR4: Loops are stack-safe (`effect.TailRec` / `tailrec`), never recursion inside an effect.
- NFR5: Tests run offline: each story has at least one unit test with fake Deps, and a replay test against recorded `ChatCompletionDeps` where it applies; protocol tests use in-process transports (`io.Pipe`, `mcp.NewInMemoryTransports`, `httptest.Server`).
- NFR6: Wire formats follow the recordings where they are known: tool names and result texts, `mcp__<server>__<tool>`, the JSONL log record shape. Formats written without a recording say so in the story (the recordings of B5 are not available).
- NFR7: In the `acp` and `mcp` subcommands stdout is the JSON-RPC channel; logs and errors go to stderr.
- NFR8: Remote front ends are safe by default: read-only modes unless `--allow-agent-mode`, A2A bound to loopback unless a token is configured, write and exec tools behind approval (I1) or policy (I2).
- NFR9: Definition of done per story: `go generate ./...`, `runCodeActions.bat` (PowerShell), `go vet ./...` and `go test ./...` pass; README.md is updated in the same PR; one story is one PR with an emoji title per AGENTS.md; commits stay plain conventional commits.
- NFR10: Dependencies: YAML through `go.yaml.in/yaml/v3`; new protocol libraries at the versions PLAN.md names (`acp-go-sdk` v0.13.5, `a2a-go/v2` v2.7.0 without `a2agrpc`, `modelcontextprotocol/go-sdk`); check `go.yaml.in/yaml/v4` for a final release whenever `go.mod` changes.

### Additional Requirements

Design decisions from PLAN.md that cut across stories:

- One turn function for every front end: J1's Kleisli is called by the CLI, the REPL, ACP, the MCP server and A2A; none of them rebuild the wiring.
- One event sink for every observer: F2 feeds the CLI, the REPL, ACP (J4, J8), the MCP server's progress (G6), A2A (K4) and subagents (H6).
- `ChatCompletion` and `ChatCompletionStream` share one Kleisli signature behind `ChatCompletionDeps`.
- Session-changing tools (E6) are the base of `update_todo_list` (H3) and `switch_mode` (H5); the mode-swap endomorphism is shared by H5 and ACP `set_mode` (J6).
- Registries merge as a monoid: core tools win over MCP (G3) and A2A (K7) tools on name clashes. Config precedence (MCP config, modes) is a monoid where the workspace wins.
- A2A: a context is a session, a task is a turn; one lock per context; the workspace is fixed per server; the pull-based `Execute` stream is bridged from the push-based sink by a channel at the edge.
- Deferred from the E3 review: steered messages drained before a failed request are lost; E4's partial-session result fixes it.
- Open remainders of done stories: `rg` fallback and `core.excludesFile` for `grep` (C4 → FR-C12); stopping background processes (C6 → FR-E4); the `active_file` part of `environment_details` (D6, needs an editor: FR-J9); the prompt sections that D7 found missing (→ FR-D9, FR-D10).

### Delivered Work and GitHub Traceability

Synced with [Carsten-Leue/fp-go-harness](https://github.com/Carsten-Leue/fp-go-harness) on 2026-10-24
(issues and PRs, all states). `#n` is an issue, `!n` a PR. The done epics below list these stories in one line each.

| Step | Issue | PR | State |
|---|---|---|---|
| A1–A5 Close the loop | #1 | !2 | done |
| B1 Log reader | – | !3 | done |
| B2 Payload extraction | – | !4 | done |
| B3 Task grouping | #5 | !6 | done |
| B4 Replay `ChatCompletionDeps` | – | !27 | done |
| B5 Replay fixtures | #26 (open) | – | **open**: moved after F3 (record the harness's own sessions, as #26 proposes) |
| C1 Workspace deps | #7 | !8 | done |
| C2 `read_file` | #9 | !10 | done |
| C3 `list_files`, `glob` | #11 | !12 | done |
| C4 `grep` | #13 | !14 | done (`rg` fallback, `core.excludesFile` open) |
| C5 `execute_command` foreground | #15 | !16 | done |
| C6 `execute_command` background | #28 | !29 | done (stopping processes → E4) |
| C7 `write_file` | #30 | !31 | done |
| C8 `search_and_replace` | – | !36 | done |
| C9 `apply_diff` | #37 | !38 | done |
| C10 `insert_content` | #39 | !40 | done |
| C11 Output limits | #41 | !42 | done |
| D1 Section monoid | #43 | !44 | done |
| D2 Static sections | #45 | !46 | done |
| D3 `environment_info` | #47 | !48 | done |
| D4 `project_rules` | #49 | !50 | done |
| D5 Mode definitions | #32, #53, #55 | !33, !52, !54, !56 | done |
| D6 `environment_details` | #59 | !60 | done |
| D7 Replay comparison | – | !61 | done |
| D8 Custom modes | #62 | !63 | done |
| E1 Multi-turn | – | !66 | done |
| E2 Persistence | #67 | !68 | done |
| E3 Steering | – | – | **review**: in the working tree (`session/steering.go`), reviewed, not committed or merged |

Work outside the plan steps: #17 / !18 matching combinators, #19 / !20 CI and semantic release, !21 README and
conventions, #22 / !23 read-only tools in the CLI, #24 / !25 README wording, #34 / !35 CLI examples, #57 / !58
urfave/cli configuration, !51 fp-go v2.6.0, #64 / !65 the ACP, A2A and MCP server plan.

No open step (E4 onward) has an issue yet.

### UX Design Requirements

None. The harness is a CLI and a protocol server; it has no UI of its own.

### FR Coverage Map

Done steps (A1–A5, B1–B4, C1–C11, D1–D8, E1–E3) map to the epic of their phase and are listed as done stories.

- FR-C12: Epic 3 - `grep` with `rg` and `core.excludesFile`
- FR-D9: Epic 4 - investigation and engineering-discipline sections
- FR-D10: Epic 4 - custom instructions and auto-appended context
- FR-E4: Epic 5 - cancellation and the partial session
- FR-E5: Epic 5 - interactive REPL
- FR-E6: Epic 5 - session-changing tools
- FR-F1: Epic 6 - streaming request
- FR-F2: Epic 6 - event sink
- FR-F3: Epic 6 - JSONL logging
- FR-F4: Epic 6 - usage details and cost
- FR-F5: Epic 6 - HTTP retry
- FR-B5: Epic 6 - replay fixtures from the harness's own logs (#26)
- FR-G1: Epic 7 - MCP config
- FR-G2: Epic 7 - stdio session
- FR-G3: Epic 7 - tool bridge
- FR-G4: Epic 7 - dogfood the fp-go server
- FR-G5: Epic 7 - reconnect
- FR-G6: Epic 7 - harness as an MCP server
- FR-H1: Epic 8 - skill discovery
- FR-H2: Epic 8 - `use_skill`
- FR-H3: Epic 8 - `update_todo_list`
- FR-H4: Epic 8 - `ask_followup_question`
- FR-H5: Epic 8 - `switch_mode` and `available_modes`
- FR-H6: Epic 8 - subagents
- FR-I1: Epic 9 - approval hook
- FR-I2: Epic 9 - allow and deny lists
- FR-I3: Epic 9 - external changes
- FR-J1: Epic 10 - turn as a value
- FR-J2: Epic 10 - `acp` subcommand
- FR-J3: Epic 10 - stop reasons
- FR-J4: Epic 10 - tool-call updates
- FR-J5: Epic 10 - ACP cancel
- FR-J6: Epic 10 - ACP modes
- FR-J7: Epic 10 - ACP permissions
- FR-J8: Epic 10 - ACP streaming
- FR-J9: Epic 10 - editor-backed files, terminal and `active_file`
- FR-J10: Epic 10 - `session/load`
- FR-J11: Epic 10 - MCP servers from the client
- FR-J12: Epic 10 - plan updates
- FR-K1: Epic 11 - `a2a` subcommand and agent card
- FR-K2: Epic 11 - executor
- FR-K3: Epic 11 - outcome mapping
- FR-K4: Epic 11 - progress
- FR-K5: Epic 11 - A2A cancel
- FR-K6: Epic 11 - auth and safety
- FR-K7: Epic 11 - A2A client tools
- FR-K8: Epic 11 - `input-required`
- FR-K9: Epic 11 - push notifications
- FR-K10: Epic 11 - persistent task store

## Epic List

Epics follow the phases of PLAN.md (Epic 1 = Phase A … Epic 11 = Phase K); story titles start with the step ID.

### Epic 1: Hello agent (Phase A, M1) — done
A CLI that answers questions with tools and stops instead of hanging.
**FRs covered:** A1–A5

### Epic 2: Replayable (Phase B, M2) — done
Recorded tasks replay offline in `go test`. B5 moved to Epic 6.
**FRs covered:** B1–B4

### Epic 3: Can code (Phase C, M3)
The agent reads, searches and edits files and runs commands; reopened for the `grep` remainders.
**FRs covered:** C1–C11 (done), FR-C12

### Epic 4: Tagged system prompt (Phase D, M3)
The system prompt and per-turn details match the recorded sessions, with built-in and custom modes; reopened for the missing sections.
**FRs covered:** D1–D8 (done), FR-D9, FR-D10

### Epic 5: Conversational (Phase E, M4)
A user holds a conversation: multiple turns, saved tasks, steering while it runs, Ctrl-C that stops the turn and not the work, an interactive REPL, and tools that change the session.
**FRs covered:** E1–E2 (done), E3 (review), FR-E4, FR-E5, FR-E6

### Epic 6: Observable (Phase F, M4 / M5)
A user sees what a turn does while it runs: streamed text, tool events, JSONL logs, token cost, retried requests, and fixtures recorded from the harness's own sessions.
**FRs covered:** FR-F1, FR-F2, FR-F3, FR-F4, FR-F5, FR-B5

### Epic 7: MCP both ways (Phase G, M5)
The agent uses the tools of MCP servers, the fp-go server included, and MCP hosts can call the harness as an agent.
**FRs covered:** FR-G1, FR-G2, FR-G3, FR-G4, FR-G5, FR-G6

### Epic 8: Skills, todos and subagents (Phase H, M5)
The agent uses skills, keeps a todo list, asks the user questions, switches modes and delegates subtasks.
**FRs covered:** FR-H1, FR-H2, FR-H3, FR-H4, FR-H5, FR-H6

### Epic 9: Safe to let loose (Phase I, M5)
The user approves writes and commands, limits them by allow and deny lists, and the agent notices files changed outside it.
**FRs covered:** FR-I1, FR-I2, FR-I3

### Epic 10: In the editor (Phase J, M6)
Zed, JetBrains and other ACP clients run the harness as their agent, with tool calls, cancellation, modes, permissions and streaming.
**FRs covered:** FR-J1, FR-J2, FR-J3, FR-J4, FR-J5, FR-J6, FR-J7, FR-J8, FR-J9, FR-J10, FR-J11, FR-J12

### Epic 11: On the network (Phase K, M7)
Other agents send coding tasks to the harness over A2A, and the harness delegates to remote A2A agents as tools.
**FRs covered:** FR-K1, FR-K2, FR-K3, FR-K4, FR-K5, FR-K6, FR-K7, FR-K8, FR-K9, FR-K10

### Delivery order

The numbered epic and story order is a valid serial order: every hard dependency of PLAN.md §3 points to an
earlier epic or an earlier story of the same epic. The waves only show what can run in parallel.

- **First:** merge E3 (in review); E4 changes `Run`, which E3 also touches.
- **Wave 1:** E4 and E6 first, then F1–F5, G1, H1, H6, I1, K1, C12, D9, D10. F2, I1 and G3 come after E6, because they wrap `ToolCall`, which E6 changes.
- **Wave 2:** E5, J1 (after E4, whose partial session changes `Run`'s result), I3, G2, H2, H3, H5, I2, B5 (after F3).
- **Wave 3:** J2, G3, G5, G6, K2, H4.
- **Wave 4:** J3–J9, G4, K3–K7.
- **Wave 5:** J10–J12, K8–K10.
- **Shortest path into an editor:** E4 → J1 → J2 → J3, with E6 → F2 → J4.

**Hub stories** — E4, E6, F2 and J1 — carry an acceptance criterion that names their later consumers and what each
needs, so the shape is right the first time.

## Epic 1: Hello agent

A CLI that answers questions with tools and stops instead of hanging. Done (M1).

### Story 1.1: A1 Clean up TODOs

Delivered: #1 / !2.

### Story 1.2: A2 Loop driver

Delivered: #1 / !2.

### Story 1.3: A3 Iteration limit

Delivered: #1 / !2.

### Story 1.4: A4 Tool definitions in the request

Delivered: #1 / !2.

### Story 1.5: A5 main.go CLI

Delivered: #1 / !2.

## Epic 2: Replayable

Recorded tasks replay offline in `go test`. Done (M2); B5 moved to Story 6.6.

### Story 2.1: B1 Log reader

Delivered: !3.

### Story 2.2: B2 Payload extraction

Delivered: !4.

### Story 2.3: B3 Task grouping

Delivered: #5 / !6.

### Story 2.4: B4 Replay ChatCompletionDeps

Delivered: !27.

## Epic 3: Can code

The agent reads, searches and edits files and runs commands (M3). Reopened for the `grep` remainders of C4.

### Story 3.1: C1 Workspace dependency

Delivered: #7 / !8.

### Story 3.2: C2 read_file

Delivered: #9 / !10.

### Story 3.3: C3 list_files and glob

Delivered: #11 / !12.

### Story 3.4: C4 grep

Delivered: #13 / !14.

### Story 3.5: C5 execute_command foreground

Delivered: #15 / !16.

### Story 3.6: C6 execute_command background

Delivered: #28 / !29.

### Story 3.7: C7 write_file

Delivered: #30 / !31.

### Story 3.8: C8 search_and_replace

Delivered: !36.

### Story 3.9: C9 apply_diff

Delivered: #37 / !38.

### Story 3.10: C10 insert_content

Delivered: #39 / !40.

### Story 3.11: C11 Output limits

Delivered: #41 / !42.

### Story 3.12: C12 grep with ripgrep and core.excludesFile

As a user of the agent on a large repository,
I want `grep` to use ripgrep when it is installed and to skip what my global git ignore file excludes,
So that searches are fast and never return files git itself ignores.

**Acceptance Criteria:**

**Given** `rg` is found on the `PATH` (a lookup getter in `ProcessDeps`, faked in tests)
**When** the model calls `grep` with `include`, `ignore_case`, `invert_match`, `word_regexp` or `files_with_matches`
**Then** the search runs `rg` through `ProcessDeps.RunProcess` with the matching flags (`--glob`, `-i`, `-v`, `-w`, `-l`)
**And** the result text is the same as the built-in search's for the same matches: grouped by file, paths relative to the workspace, capped at 100

**Given** `rg` is not installed, or exits with an error other than "no matches"
**When** the model calls `grep`
**Then** the built-in `regexp` search answers, as today (a failed `rg` start is not a tool error)

**Given** git's `core.excludesFile` is set (or its default `$XDG_CONFIG_HOME/git/ignore` / `~/.config/git/ignore` exists)
**When** the built-in search walks the workspace
**Then** files matched by its patterns are skipped, alongside `.git/info/exclude` and the `.gitignore` files
**And** a missing excludes file means no extra patterns, not an error

**Given** the fakes of the existing grep tests
**When** the tests run
**Then** one test covers the `rg` path with a fake process answer, one the fallback, and one a pattern from `core.excludesFile`

## Epic 4: Tagged system prompt

The system prompt and per-turn details match the recorded sessions, with built-in and custom modes (M3). Reopened
for the sections D7 found missing.

### Story 4.1: D1 PromptSection monoid

Delivered: #43 / !44.

### Story 4.2: D2 Static sections

Delivered: #45 / !46.

### Story 4.3: D3 environment_info

Delivered: #47 / !48.

### Story 4.4: D4 project_rules

Delivered: #49 / !50.

### Story 4.5: D5 Mode definitions

Delivered: #32, #53, #55 / !33, !52, !54, !56.

### Story 4.6: D6 environment_details per turn

Delivered: #59 / !60.

### Story 4.7: D7 Replay comparison

Delivered: !61.

### Story 4.8: D8 Custom modes

Delivered: #62 / !63.

### Story 4.9: D9 Investigation and engineering-discipline sections

As a user of the agent,
I want the system prompt to carry the `investigate_before_answering` and `engineering_discipline` sections of the recorded sessions,
So that the model investigates before it answers and works with the discipline of the recorded agent.

**Acceptance Criteria:**

**Given** the Agent mode
**When** `mode.Request()` assembles the system prompt
**Then** it contains `investigate_before_answering` and `engineering_discipline` as static sections (embedded files in `prompt/prompts/`, like D2) at their recorded positions
**And** the texts say in the story spec that they were written without a recording

**Given** the Plan or Ask mode
**When** the prompt is assembled
**Then** both sections are present without the rules that name a tool the mode doesn't allow (as D5 filters today)

**Given** the synthetic recording of D7 (`mode/testdata/recorded-system-prompt.log`)
**When** `prompt.CompareSections` compares it with the assembled Agent prompt
**Then** the two tags are no longer missing, nothing is extra, and the order matches

### Story 4.10: D10 Custom instructions and auto-appended context

As a user of the agent,
I want my personal instructions and the automatically added context to reach the model as their own sections,
So that the agent follows my preferences in every workspace.

**Acceptance Criteria:**

**Given** the user-level instructions file exists (the story spec decides its path, e.g. under `~/.bob/`, and records that it was chosen without a recording)
**When** the prompt is assembled
**Then** its text is the `user_custom_instructions` section, read through `tools.FileDeps` from the home directory
**And** a missing or empty file gives no section (`option.None`, via `CatchIs(fs.ErrNotExist, …)`), and other read errors fail the effect

**Given** the story spec defines what `auto_appended_context` holds and where it comes from (not known from a recording)
**When** there is no such context
**Then** the section is left out

**Given** the synthetic recording of D7
**When** `prompt.CompareSections` compares it with the assembled Agent prompt
**Then** the only missing tags are `available_skills` (H1) and `available_modes` (H5), and the order matches

## Epic 5: Conversational

A user holds a conversation: multiple turns, saved tasks, steering while it runs, Ctrl-C that stops the turn and
not the work, an interactive REPL, and tools that change the session (M4).

### Story 5.1: E1 Multi-turn

Delivered: !66.

### Story 5.2: E2 Persistence

Delivered: #67 / !68.

### Story 5.3: E3 Steering

As a user watching a running turn,
I want to add a message while the agent is still working,
So that I can correct its course without waiting for the turn to end.

Status: implemented and reviewed in the working tree (`session/steering.go`), not merged. The review's deferred
finding (steered messages lost when the next request fails) is fixed by Story 5.4.

**Acceptance Criteria:**

**Given** `SteeringDeps.GetPoll()` (embedded in `SessionDeps`) returns queued texts
**When** `Run` is about to send the next request
**Then** `Steer()` appends one user message per text and keeps the iteration counter
**And** no poll happens once the iteration limit is reached; texts queued after the last request stay queued for the next `Run`

**Given** `ChannelSteering(queue)`
**When** it is polled
**Then** it drains the channel without blocking (`io.TailRec`, one non-blocking receive per step)
**And** the one-shot CLI wires `NoSteering()`

**Given** a stateless fake server that queues a message while it answers the first request with a tool call
**When** the turn runs
**Then** the second request carries the message after the tool result

### Story 5.4: E4 Cancellation and the partial session

As a user of the CLI,
I want Ctrl-C to stop the running request and tools and keep what the turn has done so far,
So that an aborted turn costs me nothing and I can continue from where it stopped.

**Acceptance Criteria:**

**Given** a running turn
**When** the `context.Context` of its `Thunk` is cancelled (Ctrl-C through `signal.NotifyContext` in the CLI)
**Then** the HTTP request in flight is aborted and running foreground commands are killed (`exec.CommandContext`)
**And** background processes started by the session are stopped and leave the process table (open since C6)

**Given** a turn that is cancelled, or whose request fails with an infrastructure error
**When** `Run` ends
**Then** the caller gets the partial session: every completed step, the steered messages already drained (closes the E3 deferred finding) and the usage so far, with the outcome (cancelled or the error) as a value the caller can match on
**And** a cancellation is an outcome, not a failed `Effect`; the deferred-work entry of E3 is marked resolved

**Given** the CLI receives Ctrl-C
**When** the turn has stopped
**Then** it prints the partial answer and usage to stderr and exits with code 130; a second Ctrl-C exits at once

**Given** the later consumers of this result shape
**When** the story spec designs it
**Then** it names what each needs: E5 continues the partial session, J1 returns it, J3 maps it to `cancelled`, J5 keeps it in the ACP session table, K3 / K5 map it to `canceled`, and G6 ends a cancelled MCP request with it

**Given** a fake `ChatCompletionDeps` that blocks until its context is done, and a fake process
**When** the test cancels the context after one completed tool round trip
**Then** the result holds that round trip in `history`, the outcome is cancelled, and the fake process saw its context end

### Story 5.5: E5 Interactive REPL

As a user,
I want an interactive session in the terminal,
So that I can talk to the agent over many turns, steer it, interrupt it and pick a task up later.

**Acceptance Criteria:**

**Given** `fp-go-harness` started without a prompt (or with `--interactive`)
**When** I type a line
**Then** it is one turn: the first from the mode's request, the next ones through `session.Continue` → `Run`, each answer and its usage printed
**And** the loop is an `IO` loop through `tailrec` at the edge of the program

**Given** a turn is running
**When** I type another line
**Then** it is queued through `ChannelSteering` (E3) and reaches the running turn

**Given** a turn is running
**When** I press Ctrl-C
**Then** only that turn stops (E4) and the REPL continues on the partial session
**And** Ctrl-C at an empty prompt or end of input exits

**Given** `--task <id>`
**When** the REPL starts
**Then** it loads the saved task (E2) and continues it; after every turn the session is saved under the id, and without `--task` a new id is created and printed

**Given** a scripted stdin and replay `ChatCompletionDeps`
**When** two lines are entered
**Then** the second request carries the first answer, and a restart with the printed `--task` id continues the history

### Story 5.6: E6 Session-changing tools

As a developer of the harness,
I want tools whose result can change the session,
So that `update_todo_list`, `switch_mode` and ACP's `set_mode` are tools and not special cases of the loop.

**Acceptance Criteria:**

**Given** a second kind of tool whose result is the text and an `Endomorphism[Session]`
**When** `Next` handles the tool calls of a response
**Then** it appends the tool messages and then applies the endomorphisms in the order of the tool calls, whatever order the concurrent calls finish in
**And** a failed session-changing tool is a tool message with the identity endomorphism

**Given** the existing plain tools
**When** they are registered
**Then** they lift into the new kind with the identity, and every existing tool test passes unchanged

**Given** the wrappers over tool calls
**When** the story spec designs the type
**Then** it names each consumer: `Truncate` (C11) and `SelectTools` (D5) today, and later I1's approval wrapper, F2's `Tap`, the G3 and K7 bridges, H3, H5 and J6, so they wrap one tool type

**Given** a fake tool that sets a session field through a lens
**When** a turn calls it
**Then** the next request is built from the changed session

## Epic 6: Observable

A user sees what a turn does while it runs: streamed text, tool events, JSONL logs, token cost, retried requests,
and fixtures recorded from the harness's own sessions (M4, F5 in M5).

### Story 6.1: F1 Streaming request

As a user,
I want the model's answer to arrive as a stream,
So that long answers start showing at once and the request looks like the recorded ones.

**Acceptance Criteria:**

**Given** `openai.ChatCompletionStream()`
**When** it is called
**Then** it has the same Kleisli signature as `ChatCompletion` and sends `stream: true` with `stream_options.include_usage: true`
**And** it folds the chunks with `openai.ChatCompletionAccumulator` into one `*ChatCompletion`, tool-call deltas and the final usage included

**Given** `ChatCompletionDeps`
**When** the CLI is wired
**Then** streaming or not is chosen in the deps (a configuration option), and `session` code doesn't change

**Given** an `httptest.Server` that answers with SSE chunks (text, then a split tool call, then usage)
**When** the stream is folded
**Then** the result equals the non-streaming answer for the same content, and a broken stream fails the effect

### Story 6.2: F2 Event sink

As a user and as every front end of the harness,
I want one stream of events for what a running turn does,
So that the CLI, the REPL, editors, MCP hosts and A2A callers all see tool calls and text as they happen.

**Acceptance Criteria:**

**Given** `EventDeps` with an `Emit` getter, embedded in `SessionDeps`
**When** a turn runs
**Then** it emits: assistant text that comes with tool calls, a tool call started (id, name, arguments), a tool call finished (id, succeeded or failed, output)
**And** the tool events are a `Tap` around each call in `MakeToolCall`, on the tool type of E6

**Given** a streaming request (F1)
**When** chunks arrive
**Then** text and reasoning deltas are emitted from inside the stream fold's thunk

**Given** tool calls run concurrently (`Ap`)
**When** events are emitted
**Then** a start comes before its finish for the same id, no order is promised across ids, and the sink is the only place that synchronises (at the edge)

**Given** the later consumers of the event type
**When** the story spec designs it
**Then** it names what each needs: J4 (`kind` from the tool name, file `locations` from the arguments, `completed`/`failed`), J8 (text and reasoning deltas), K4 (one status message per tool call, deltas as artifact appends), G6 (progress notifications), H4 (a question event) and H6 (events of a nested run, told apart from the parent's)

**Given** the CLI
**When** it is wired
**Then** it uses a no-op sink, and a test with a recording sink (mutex, `assert.ElementsMatch`) sees one start and one finish per tool call

### Story 6.3: F3 Structured logging

As a developer of the harness,
I want the harness to log its own sessions in the recorded JSONL shape,
So that I can debug a session and replay it with the Phase B tools.

**Acceptance Criteria:**

**Given** `LogDeps` and a log file (a configuration option, off by default)
**When** a turn runs
**Then** it writes JSONL records with `ts`, `level`, `module`, `msg`, `taskId` and `data` for `Starting agent loop`, `Tool invoked` and `Agent loop completed`
**And** each request body is logged as `HTTP request body for /chat/completions:` and each answer as `Assembled chat response:`

**Given** concurrent tool calls
**When** they log
**Then** records are written whole, one per line; the writer is the only synchronised resource (at the edge)

**Given** a log written by the harness
**When** `replay.LoadLogFile` and `GroupTasks` read it
**Then** they yield the task with its turns, and `session.Run` over `MakeTaskChatCompletionDeps` replays it

**Given** the API key and the `Authorization` header
**When** anything is logged
**Then** neither appears in the log

### Story 6.4: F4 Usage details and cost

As a user,
I want to see cache-read and reasoning tokens and the cost of each turn,
So that I know what a session costs and whether caching works.

**Acceptance Criteria:**

**Given** `MakeUsageMonoid`
**When** usages are combined
**Then** cache-read (`prompt_tokens_details.cached_tokens`) and reasoning (`completion_tokens_details.reasoning_tokens`) tokens are summed with the others

**Given** prices per million tokens for input, cached input and output of the model (configuration)
**When** a turn ends
**Then** the CLI prints its tokens and its cost; without a price only the tokens are printed

**Given** fixed usages
**When** the cost is computed
**Then** a pure function gives the expected amount (a table test)

### Story 6.5: F5 HTTP retry

As a user,
I want rate limits and server errors to be retried,
So that a long session doesn't die on one 429 or 503.

**Acceptance Criteria:**

**Given** an `http.RoundTripper` decorator in `http.HttpDeps`
**When** a request gets 429 or 5xx, or a network error before a response
**Then** it is retried with exponential backoff and jitter up to a maximum number of attempts, and `Retry-After` is honoured
**And** fp-go's `retry` package is used if `go doc` shows it fits; the request body is replayable for each attempt

**Given** a cancelled context (E4) or a 4xx other than 429
**When** the request fails
**Then** it is not retried

**Given** an `httptest.Server` that answers 503 twice and then 200
**When** the request runs with a fake clock or zero delays
**Then** it succeeds on the third attempt, and with a limit of two it fails with the last status

### Story 6.6: B5 Replay fixtures from the harness's own logs

As a developer of the harness,
I want a few real recorded sessions in `testdata/`,
So that replay tests run against real traffic and not only synthetic logs (closes #26).

**Acceptance Criteria:**

**Given** F3's logging
**When** 2–3 tasks are recorded against this repository (a manual step with a real API key)
**Then** they are redacted with `replay.Redact` and committed under `testdata/`

**Given** the committed fixtures
**When** a test scans them
**Then** they contain no API keys, tokens, internal URLs or personal paths

**Given** each fixture
**When** it is replayed with `LoadLogFile`, `GroupTasks` and `session.Run` over `MakeTaskChatCompletionDeps`
**Then** the iterations, the summed usage and the final message match the recording

## Epic 7: MCP both ways

The agent uses the tools of MCP servers, the fp-go server included, and MCP hosts can call the harness as an
agent (M5).

### Story 7.1: G1 MCP config

As a user,
I want the harness to read my MCP server configuration from the usual files,
So that the servers I already use in other tools are available to it.

**Acceptance Criteria:**

**Given** `<ws>/.mcp.json` and `~/.bob/settings/mcp.json` in the `mcpServers` shape (`type`, `command`, `args`, `env`, as in this repository's `.mcp.json`)
**When** the configuration is read
**Then** the servers of both are merged by name with a record monoid in which the workspace wins
**And** a missing file is no servers; an invalid one fails with the file name in the error

**Given** a server of a type other than `stdio`
**When** the configuration is read
**Then** it is kept in the table but marked unsupported (only stdio is connected, G2)

**Given** an `fstest.MapFS` per directory
**When** the tests run
**Then** they cover a workspace override, a server only in the home file, and an invalid file

### Story 7.2: G2 stdio session

As a user,
I want the harness to start my MCP servers and skip the ones that fail,
So that one broken server doesn't stop a session.

**Acceptance Criteria:**

**Given** `MCPDeps` on `github.com/modelcontextprotocol/go-sdk` (a direct dependency from now on)
**When** `Connect` (a `Kleisli[ServerConfig, Option[Session]]`) starts a stdio server
**Then** a server that starts gives `Some(session)`, one that fails to start gives `None` and a `Session failed to start` log entry (F3)
**And** the sessions are closed when the harness ends (and on cancellation, E4)

**Given** an in-process server (`mcp.NewInMemoryTransports`) and a command that doesn't exist
**When** both are connected
**Then** the first yields a session and the second `None`, without failing the effect

### Story 7.3: G3 Tool bridge

As a user,
I want the tools of my MCP servers to be tools of the agent,
So that the model can call them like the built-in ones.

**Acceptance Criteria:**

**Given** a connected session
**When** its `tools/list` is read
**Then** each tool becomes a `ToolRegistry` entry `mcp__<server>__<tool>` with the server's description and input schema, and its call is `tools/call`
**And** a tool result with `isError` or a failed call is a tool message, not a failed `Effect`; text content is joined, other content types are named

**Given** the core registry and the MCP entries
**When** they are merged with the registry monoid
**Then** core tools win on name clashes, and the merged registry is limited by `Truncate` (C11) and filtered by the mode (`SelectTools`) like the core tools

**Given** an in-memory MCP server with two tools
**When** a turn with replay `ChatCompletionDeps` calls one of them
**Then** the tool message carries the server's answer

### Story 7.4: G4 Dogfood the fp-go server

As the developer of this repository,
I want the harness to use the `fp-go` MCP server from `.mcp.json` while it works on itself,
So that it looks up fp-go combinators the way the coding rules require.

**Acceptance Criteria:**

**Given** this repository as the workspace
**When** the CLI starts in agent mode
**Then** `mcp__fp-go__search_examples`, `mcp__fp-go__get_example`, `mcp__fp-go__use_skill` and `mcp__fp-go__list_skills` are in the request's tools

**Given** a smoke test (manual, or a live example like those of !35, skipped without an API key)
**When** the agent is asked about an fp-go combinator
**Then** it calls an `mcp__fp-go__` tool and answers from its result

### Story 7.5: G5 Reconnect

As a user,
I want a crashed MCP server to be restarted a few times,
So that a session survives a server that dies once.

**Acceptance Criteria:**

**Given** a session whose server process exits or whose transport breaks
**When** the next call to one of its tools is made
**Then** the harness reconnects with backoff (`Scheduling reconnect attempt`) up to a maximum number of attempts (`Max reconnect attempts reached`), both logged
**And** while it is down, calls to its tools return a tool message saying the server is unavailable

**Given** a fake transport that fails twice and then connects
**When** a tool is called
**Then** the third attempt succeeds; with a limit of two, the tool message reports the server as unavailable

### Story 7.6: G6 Harness as an MCP server

As a user of an MCP host (Claude Code, Bob, …),
I want to call fp-go-harness as a tool,
So that another agent can hand it a task.

**Acceptance Criteria:**

**Given** `fp-go-harness mcp --workspace <dir>`
**When** an MCP host connects over stdio
**Then** it lists one tool, `run_task`, taking `prompt` and an optional `mode` (ask by default; agent only with `--allow-agent-mode`)
**And** stdout carries only JSON-RPC; logs and errors go to stderr

**Given** a `run_task` call
**When** the turn ends
**Then** the result is the final answer as text with the iteration and token counts; each call is one turn on a fresh session (a `session_id` comes with J1)

**Given** the host sends a progress token
**When** tool calls run
**Then** each is reported as a progress notification from the F2 event sink, and a cancelled request cancels the turn (E4)

**Given** an in-memory MCP client (`mcp.NewInMemoryTransports`) and replay `ChatCompletionDeps`
**When** it calls `run_task`
**Then** it gets the replayed answer, and `mode: agent` without `--allow-agent-mode` is refused

## Epic 8: Skills, todos and subagents

The agent uses skills, keeps a todo list, asks the user questions, switches modes and delegates subtasks (M5).

### Story 8.1: H1 Skill discovery

As a user,
I want the agent to know the skills installed for me and for the workspace,
So that it picks the right one for a task.

**Acceptance Criteria:**

**Given** the skill directories `~/.bob/skills`, `~/.claude/skills`, `<ws>/.bob/skills` and `<ws>/.claude/skills`
**When** the prompt is assembled
**Then** every `<dir>/<skill>/SKILL.md` is read, its front matter split by `mode.SplitFrontMatter` and decoded with `go.yaml.in/yaml/v3` (`name`, `description`)
**And** the skills are rendered as the `<available_skills>` section, merged by name with the workspace winning (a monoid, as in D8)

**Given** a `SKILL.md` without valid front matter
**When** the skills are read
**Then** it is left out and logged; the prompt is still assembled

**Given** an `fstest.MapFS` per directory and the D7 recording
**When** the tests run
**Then** an override, a broken file and the section are covered, and `available_skills` is no longer missing in the comparison

### Story 8.2: H2 use_skill

As a user,
I want the agent to load a skill when it needs it,
So that the skill's instructions guide the rest of the task.

**Acceptance Criteria:**

**Given** a skill found by H1
**When** the model calls `use_skill` with its name
**Then** the tool result is the text of its `SKILL.md`, as recorded

**Given** an unknown name
**When** the model calls `use_skill`
**Then** the tool message names the available skills

**Given** any mode
**When** its tools are selected
**Then** `use_skill` is included (it only reads)

### Story 8.3: H3 update_todo_list

As a user,
I want the agent to keep a visible todo list for a longer task,
So that I and the model can see what is done and what is next.

**Acceptance Criteria:**

**Given** the session-changing tool kind of E6
**When** the model calls `update_todo_list` with a Markdown checklist (`[ ]`, `[x]`, `[-]`)
**Then** the list replaces the session's todo list through a lens, and an invalid list is a tool message

**Given** a session with a todo list
**When** the next user message is wrapped by `environment_details` (D6)
**Then** it shows the list; an empty list shows nothing

**Given** a saved task (E2)
**When** it is loaded
**Then** the todo list is restored (`SessionDTO` carries it)

### Story 8.4: H4 ask_followup_question

As a user in the REPL,
I want the agent to ask me a question when it is stuck,
So that it doesn't guess when only I know the answer.

**Acceptance Criteria:**

**Given** a front end with `InputDeps` (the REPL of E5)
**When** the model calls `ask_followup_question` with a question and optional suggested answers
**Then** a question event is emitted (F2), the thunk waits for the answer, and the answer is the tool result
**And** a cancelled turn (E4) ends the wait

**Given** a front end without `InputDeps` (the one-shot CLI, the MCP server)
**When** the tools are selected
**Then** `ask_followup_question` is not offered

**Given** a fake `InputDeps` with a fixed answer
**When** a turn with replay `ChatCompletionDeps` calls the tool
**Then** the next request carries the answer as the tool message

### Story 8.5: H5 switch_mode and available_modes

As a user,
I want the agent to switch to the mode that fits the task,
So that it plans in Plan mode and only edits in a mode that allows it.

**Acceptance Criteria:**

**Given** the mode table (D8)
**When** the prompt is assembled
**Then** it contains the `<available_modes>` section (id, name, description of each mode), and the D7 comparison misses no tag

**Given** the session-changing tool kind of E6
**When** the model calls `switch_mode` with a mode id and a reason
**Then** one exported endomorphism swaps the system message and the tools of `session.current` to that mode's (D5), keeping the conversation; J6 reuses it
**And** an unknown id is a tool message naming the modes

**Given** the session started with a set of allowed tools
**When** the model switches to a mode with tools outside that set
**Then** the switch is refused with a tool message (approval of such a switch can come with I1)

**Given** replay `ChatCompletionDeps` that switch from Agent to Plan
**When** the turn runs
**Then** the next request has Plan's system prompt and tools

### Story 8.6: H6 Subagents

As a user,
I want the agent to hand a self-contained subtask to a subagent,
So that the main conversation stays short and the subtask gets a fresh context.

**Acceptance Criteria:**

**Given** a `spawn_subagent` tool (task text, optional mode)
**When** the model calls it
**Then** a nested `session.Run` runs on a fresh `Session` with narrower deps (`effect.Local`), and its final text is the tool result
**And** its usage is added to the parent's, and its own iteration limit applies

**Given** the nested run
**When** it emits events (F2)
**Then** they carry the parent's tool call id, so front ends can tell them apart

**Given** a subagent
**When** its tools are selected
**Then** `spawn_subagent` is not among them (one level only), nor tools outside the parent's allowed set

**Given** a cancelled parent turn (E4)
**When** a subagent is running
**Then** it stops too

**Given** replay `ChatCompletionDeps` for parent and child
**When** the parent calls `spawn_subagent`
**Then** the parent's next request carries the child's final answer

## Epic 9: Safe to let loose

The user approves writes and commands, limits them by allow and deny lists, and the agent notices files changed
outside it (M5).

### Story 9.1: I1 Approval hook

As a user,
I want to approve every file change and command before it runs,
So that the agent can't damage my workspace without my consent.

**Acceptance Criteria:**

**Given** `ApprovalDeps` with an `Approve` getter (a `Kleisli[ToolInvocation, Decision]`; the invocation carries the tool name, its arguments and, where it has them, the path or command)
**When** `write_file`, `search_and_replace`, `apply_diff`, `insert_content`, `execute_command` or an MCP tool without a read-only hint is called
**Then** the call runs only when approved; a denial is a tool message saying the user declined, never a failed `Effect`
**And** the wrapper is applied in `tools.MakeTools` on the tool type of E6, so MCP tools (G3) and later A2A tools (K7) are wrapped the same way

**Given** the REPL (E5)
**When** approval is needed
**Then** it asks on the terminal with allow once, allow always for this tool, deny; "always" is remembered for the session in state at the edge (mutex), never behind a getter

**Given** the one-shot CLI, which can't ask
**When** a write or exec tool is called
**Then** it is denied unless `--auto-approve` is given (a change of today's behaviour, noted in README)

**Given** a `switch_mode` to a mode with more tools (H5)
**When** the front end can ask
**Then** the switch goes through the same approval

**Given** a fake `ApprovalDeps` that denies `execute_command` and allows `write_file`
**When** a replay turn calls both
**Then** the command's tool message reports the denial, the file is written, and the fake process was never started

### Story 9.2: I2 Allow and deny lists

As a user,
I want to allow safe commands without being asked and forbid dangerous ones outright,
So that approval is quick for routine work and unattended runs stay safe.

**Acceptance Criteria:**

**Given** allowed and denied command prefixes in the configuration (workspace over home, as in G1)
**When** `execute_command` is called
**Then** a denied prefix is refused with a tool message, an allowed one runs without asking, anything else goes to `Approve` (I1); deny wins over allow
**And** a command chained with `&&`, `||`, `;` or `|` is allowed only if every part is

**Given** a path outside the workspace in a file tool
**When** the tool is called
**Then** it is refused unless the path lies under a directory the configuration allows

**Given** the policy
**When** it is composed
**Then** it is a decorator over `ApprovalDeps` (policy first, then approval), so K6 can run with policy only

**Given** table tests over prefixes, chains and paths
**When** they run
**Then** each case gives the expected allow, deny or ask

### Story 9.3: I3 External-change detection

As a user who edits files while the agent works,
I want the agent to be told which files changed outside it,
So that it rereads them instead of overwriting my changes.

**Acceptance Criteria:**

**Given** a turn ends
**When** the session is updated
**Then** a snapshot of the modification times of the workspace's files (skipping what `.gitignore` excludes, as `grep` does) is kept in the session as a value

**Given** a snapshot and the next user message
**When** `environment_details` (D6) wraps the message
**Then** files added, changed or deleted since the snapshot are listed as `<external_changes>`, capped with a count of the rest; no changes means no section

**Given** a `fstest.MapFS` with fixed modification times before and after
**When** the diff is computed
**Then** a pure function gives the expected added, changed and deleted files

## Epic 10: In the editor

Zed, JetBrains and other ACP clients run the harness as their agent, with tool calls, cancellation, modes,
permissions and streaming (M6). Protocol layer: `github.com/coder/acp-go-sdk`; check its current version and the
protocol fit before the first story (PLAN.md named v0.13.5). The session table (id → workspace, mode, `Session`,
`CancelFunc`, remembered permissions) is a field of the `acp.Agent` struct behind a `sync.Mutex`, never a `Deps`
getter; stdout is the JSON-RPC channel.

### Story 10.1: J1 Turn as a value

As a developer of the harness,
I want one function that runs a turn for any front end,
So that the CLI, the REPL, ACP, the MCP server and A2A don't each rebuild the wiring.

**Acceptance Criteria:**

**Given** the wiring of `cli/run.go`
**When** it is moved into a Kleisli over (workspace, mode, `Option[Session]`, prompt) → the turn's result (E4's partial-session outcome)
**Then** the tool and mode deps are built from the input's workspace, per session, instead of from `Config`
**And** `None` starts from the mode's request (D5, D6), `Some` continues with `session.Continue` (E1)

**Given** the CLI and the REPL (E5)
**When** they run a turn
**Then** they call this Kleisli, and the existing CLI tests pass

**Given** the later consumers
**When** the story spec designs the input and result
**Then** it names what each needs: J2 (a turn per `session/prompt`), J3 (finish reason and outcome for the stop reason), J5 (a cancellable context), J6 (the session's mode), J9 (replaceable `FileDeps` and process deps per session), K2 (a turn per A2A task) and G6 (a `session_id` to continue)

**Given** replay `ChatCompletionDeps`
**When** two turns run on one session
**Then** the second request carries the first answer

### Story 10.2: J2 acp subcommand

As a user of Zed or JetBrains,
I want to add fp-go-harness as an agent server,
So that I can chat with it in my editor.

**Acceptance Criteria:**

**Given** `fp-go-harness acp`
**When** a client sends `initialize`
**Then** the agent answers protocol version 1, no `loadSession`, and text and embedded-resource prompts

**Given** `session/new` with a `cwd`
**When** it is handled
**Then** the `cwd` is resolved as the workspace and a session is created in the table (its `mcpServers` are ignored until J11)

**Given** `session/prompt`
**When** it is handled
**Then** the text and `resource` blocks are joined into the user message, a turn runs (J1), the answer is sent as one `agent_message_chunk`, and the prompt returns `end_turn`
**And** the API key and limits come from `.env` and the environment, as in the CLI; nothing but JSON-RPC is written to stdout

**Given** an `acp.ClientSideConnection` over `io.Pipe` and replay `ChatCompletionDeps`
**When** the test initializes, creates a session and prompts twice
**Then** it receives both answers and the second turn keeps the history, without a network

### Story 10.3: J3 Stop reasons

As an editor user,
I want the editor to show why a turn ended,
So that I can tell a finished answer from a cut-off one.

**Acceptance Criteria:**

**Given** the outcome of a turn
**When** `session/prompt` returns
**Then** finish reason `stop` → `end_turn`, `length` → `max_tokens`, `content_filter` → `refusal`, `MaxIterationsError` → `max_turn_requests`, cancelled (E4) → `cancelled`
**And** other errors are JSON-RPC errors

**Given** one fake completion per finish reason, one over the iteration limit, and one cancelled
**When** the pipe client prompts
**Then** each returns the expected stop reason

### Story 10.4: J4 Tool-call updates

As an editor user,
I want to see each tool call and its result in the editor,
So that I can follow what the agent reads, edits and runs.

**Acceptance Criteria:**

**Given** an event sink (F2) that sends `session/update` notifications
**When** a tool call starts
**Then** a `tool_call` is sent with its `kind` from the tool name (`read_file`, `list_files` → `read`; `grep`, `glob` → `search`; the edit tools → `edit`; `execute_command` → `execute`; others → `other`) and the file `locations` from its arguments

**Given** a tool call finishes
**When** the event arrives
**Then** a `tool_call_update` with `completed` or `failed` and the output is sent

**Given** assistant text that comes with tool calls
**When** it is emitted
**Then** it is sent as an `agent_message_chunk`

**Given** the pipe client of J2
**When** a replayed turn makes two tool calls
**Then** it receives one `tool_call` and one `tool_call_update` for each

### Story 10.5: J5 ACP cancel

As an editor user,
I want the stop button to stop the agent,
So that a wrong turn doesn't run on.

**Acceptance Criteria:**

**Given** a running prompt
**When** the client sends `session/cancel`
**Then** the turn's `CancelFunc` from the session table is called, the request and tools stop (E4), and the prompt returns `cancelled`
**And** the partial session is kept in the table for the next prompt

**Given** a fake completion that blocks until cancelled
**When** the pipe client cancels
**Then** the prompt returns `cancelled` and the next prompt's request carries the completed steps

### Story 10.6: J6 ACP modes

As an editor user,
I want to pick the agent's mode in the editor,
So that I can ask, plan or let it code.

**Acceptance Criteria:**

**Given** `session/new`
**When** it answers
**Then** it returns the `availableModes` of the mode table (D8) and the current mode

**Given** `session/set_mode`
**When** it is handled
**Then** the system message and tools of the session are swapped with H5's endomorphism and a `current_mode_update` is sent; an unknown mode is a JSON-RPC error

**Given** the pipe client
**When** it switches to Plan and prompts
**Then** the request has Plan's system prompt and tools

### Story 10.7: J7 ACP permissions

As an editor user,
I want the editor to ask me before the agent writes or runs something,
So that I approve changes where I see them.

**Acceptance Criteria:**

**Given** an `ApprovalDeps` (I1) backed by `RequestPermission`
**When** a tool that needs approval is called
**Then** the client is asked with `allow_once`, `allow_always`, `reject_once` and `reject_always`
**And** a rejection is a tool message; "always" is remembered per session in the table

**Given** the I2 policy
**When** a command is allowed or denied by it
**Then** the client is not asked

**Given** a pipe client that answers `reject_once` and then `allow_always`
**When** a replayed turn calls `write_file` three times
**Then** the first is rejected, the second asks and is allowed, the third runs without asking

### Story 10.8: J8 ACP streaming

As an editor user,
I want the answer and the model's reasoning to appear as they are generated,
So that I don't wait for the whole answer.

**Acceptance Criteria:**

**Given** streaming requests (F1) and the delta events of F2
**When** text deltas arrive
**Then** each is sent as an `agent_message_chunk`, and reasoning deltas as `agent_thought_chunk`s
**And** the final answer is not sent a second time

**Given** an SSE fake server and the pipe client
**When** a turn streams three text deltas
**Then** the client receives three chunks whose text joins to the answer

### Story 10.9: J9 Editor-backed files and terminal

As an editor user,
I want the agent to read my unsaved buffers, show its edits as diffs and run commands in the editor's terminal,
So that its work lives where I work.

**Acceptance Criteria:**

**Given** a client that advertises `fs.readTextFile` / `fs.writeTextFile`
**When** a session is created
**Then** its `FileDeps` read and write through `fs/read_text_file` / `fs/write_text_file`, and the tools don't change

**Given** a client that advertises `terminal`
**When** `execute_command` runs in the foreground
**Then** it runs through `terminal/create`, waits for exit and returns the same result text as a local process

**Given** the editor's active file (D6's `active_file`, if the protocol provides it)
**When** the user message is wrapped
**Then** it is included in `environment_details`

**Given** a pipe client with fake file and terminal handlers
**When** a replayed turn reads, writes and runs a command
**Then** the handlers see the calls, and no local file or process is touched

### Story 10.10: J10 session/load

As an editor user,
I want to reopen an earlier conversation with the agent,
So that I can continue it after restarting the editor.

**Acceptance Criteria:**

**Given** sessions saved with E2 under their ACP session id
**When** `initialize` answers
**Then** it advertises `loadSession`

**Given** `session/load` with a saved id
**When** it is handled
**Then** the session is loaded into the table and its conversation is replayed to the client as `session/update` notifications; an unknown id is a JSON-RPC error

### Story 10.11: J11 MCP servers from the client

As an editor user,
I want the MCP servers configured in my editor to be available to the agent,
So that I configure them once.

**Acceptance Criteria:**

**Given** `session/new` with `mcpServers`
**When** the session is created
**Then** the stdio servers are connected (G2), their tools bridged (G3) and merged with those of the workspace configuration (G1), the client's winning on name clashes

**Given** an in-memory MCP server passed by the pipe client
**When** a replayed turn calls its tool
**Then** the tool message carries its answer

### Story 10.12: J12 Plan updates from the todo list

As an editor user,
I want the agent's todo list shown as the editor's plan,
So that I see its progress at a glance.

**Acceptance Criteria:**

**Given** `update_todo_list` (H3)
**When** the list changes
**Then** a `plan` session update with one entry per item and its status (`pending`, `in_progress`, `completed`) is sent

**Given** the pipe client
**When** a replayed turn updates the list twice
**Then** it receives two `plan` updates with the expected entries

## Epic 11: On the network

Other agents send coding tasks to the harness over A2A, and the harness delegates to remote A2A agents as tools
(M7). Protocol layer: `github.com/a2aproject/a2a-go/v2` without `a2agrpc`; check its current version before the
first story (PLAN.md named v2.7.0). A context is a session and a task is a turn; the session table (context id →
`Session`, `CancelFunc`, a lock per context) lives at the edge behind a `sync.Mutex`; the workspace is fixed per
server.

### Story 11.1: K1 a2a subcommand and agent card

As the operator of another agent,
I want to discover fp-go-harness over HTTP,
So that my agent knows what it can do and how to call it.

**Acceptance Criteria:**

**Given** `fp-go-harness a2a --addr 127.0.0.1:8080 --workspace <dir>`
**When** it starts
**Then** it serves the agent card at `/.well-known/agent-card.json` (name, description, version; the modes as `skills`; `text/plain` in and out; `streaming` only once K4 streams) and JSON-RPC on `/`
**And** it binds to loopback by default

**Given** an `a2aclient` against an `httptest.Server`
**When** it reads the card
**Then** the card lists the modes of the mode table (D8)

### Story 11.2: K2 Executor

As the operator of another agent,
I want to send the harness a coding task and get its answer,
So that my agent can delegate work to it.

**Acceptance Criteria:**

**Given** a message with text parts and optional metadata `mode` (ask by default; agent only with `--allow-agent-mode`)
**When** the `a2asrv.AgentExecutor` handles it
**Then** a turn (J1) runs on the session of the `contextId` and the executor emits `submitted`, `working`, the answer as an artifact, then `completed`
**And** two tasks of one context run one after the other (the lock per context)

**Given** replay `ChatCompletionDeps`
**When** two messages are sent in one context
**Then** the second turn keeps the history of the first

### Story 11.3: K3 Outcome mapping

As the operator of another agent,
I want the task state to say how the turn ended,
So that my agent can react to failures and refusals.

**Acceptance Criteria:**

**Given** the outcome of a turn
**When** the task ends
**Then** `end_turn` → `completed`, `refusal` → `rejected`, cancelled (E4) → `canceled`, `MaxIterationsError` and infrastructure errors → `failed` with the error text as the status message
**And** the mapping shares its outcome cases with J3

**Given** one fake completion per outcome
**When** each task runs
**Then** the final status is the expected state

### Story 11.4: K4 Progress

As the operator of another agent,
I want to follow a task while it runs,
So that a long task doesn't look stuck.

**Acceptance Criteria:**

**Given** the event sink (F2) and a channel bridge at the edge (the turn on a goroutine, `Emit` writing to the channel, the executor's `iter.Seq2` reading from it)
**When** tool calls run
**Then** each yields a `working` status update with one message naming the tool

**Given** streaming requests (F1)
**When** text deltas arrive
**Then** they become artifact updates with `append`, and the card advertises `streaming`

**Given** an SSE client against an `httptest.Server`
**When** a replayed turn makes two tool calls and streams its answer
**Then** the client receives two `working` updates and the appended artifact joins to the answer

### Story 11.5: K5 A2A cancel

As the operator of another agent,
I want to cancel a task,
So that my agent can stop work it no longer needs.

**Acceptance Criteria:**

**Given** a running task
**When** `Cancel` is called
**Then** the turn's `CancelFunc` stops it (E4), the task ends `canceled`, and the partial session stays in the context's table entry

**Given** a fake completion that blocks until cancelled
**When** the client cancels
**Then** the task state is `canceled`

### Story 11.6: K6 Auth and safety

As the owner of the machine,
I want the A2A server locked down by default,
So that nobody on the network can run commands in my workspace.

**Acceptance Criteria:**

**Given** a bearer token in the environment
**When** the server starts
**Then** the card declares it in `securitySchemes` and a `CallInterceptor` rejects calls without it

**Given** no token
**When** `--addr` is not a loopback address
**Then** the server refuses to start

**Given** `--allow-agent-mode`
**When** a task asks for agent mode
**Then** its write and exec tools run under the I2 policy only (nobody can approve); without the flag the mode is refused

**Given** `httptest` servers with and without a token
**When** clients call them
**Then** the cases above hold

### Story 11.7: K7 A2A client tools

As a user,
I want remote A2A agents to be tools of my agent,
So that it can delegate to specialised agents.

**Acceptance Criteria:**

**Given** remote agents in a configuration file (workspace over home, as in G1)
**When** a session starts
**Then** each becomes a tool `a2a__<agent>` with the agent card's description: text in, the final artifact's text out
**And** they are merged into the registry like MCP tools (G3; core tools win) and go through approval (I1)

**Given** a harness session
**When** it calls the same remote agent twice
**Then** both calls use one `contextId`, so the remote conversation continues

**Given** the K1 server in an `httptest.Server` as the remote agent
**When** a replayed turn calls `a2a__<agent>`
**Then** the tool message carries the remote answer, and an unreachable agent is a tool message, not a failed `Effect`

### Story 11.8: K8 input-required

As the operator of another agent,
I want the harness to ask my agent for approval or an answer instead of failing,
So that agent mode and follow-up questions work over A2A.

**Acceptance Criteria:**

**Given** a tool that needs approval (I1) or an `ask_followup_question` (H4)
**When** it is called in an A2A task
**Then** the task is persisted (E2) and ends in `input-required` with the question

**Given** the next message in that context
**When** it arrives
**Then** the task resumes with it as the approval or the answer

**Given** replay `ChatCompletionDeps`
**When** a task asks and the client answers
**Then** the resumed turn's next request carries the answer

### Story 11.9: K9 Push notifications

As the operator of another agent,
I want to be notified when a long task changes state,
So that my agent doesn't have to poll.

**Acceptance Criteria:**

**Given** a task with a push notification config
**When** its state changes
**Then** the server posts the update to the configured URL with the configured token

**Given** an `httptest.Server` as the receiver
**When** a replayed task runs to `completed`
**Then** the receiver sees the state changes in order

### Story 11.10: K10 Persistent task store

As the operator of another agent,
I want tasks and contexts to survive a restart of the harness,
So that a restart doesn't lose work in progress.

**Acceptance Criteria:**

**Given** the task store of E2
**When** the server restarts
**Then** contexts, their sessions and finished tasks are restored from it instead of the SDK's in-memory store

**Given** a server that is stopped after one task and started again
**When** a client sends a second message in the same context
**Then** the turn keeps the history of the first
