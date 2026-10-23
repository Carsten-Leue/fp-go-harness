# PLAN: from `fp-go-harness` to a complete coding-agent harness

This plan was worked out from the recorded agent sessions
(7 JSON-lines logs, 2026-09-24 … 2026-10-06). Each phase is a list of small steps, and each
step is meant to fit in one PR with its own test.

---

## 0. What the recordings show

What a recorded session does, taken from the `HarnessProvider.payload`, `ChatManager` and `Gateway` log records:

| Aspect | What the recordings show |
|---|---|
| Request | `model`, `max_tokens: 20000`, `tool_choice: "auto"`, `stream: true`, `stream_options.include_usage: true`, ~30 tool definitions |
| System prompt | ~65 KB of tagged sections: `role_definition`, `investigate_before_answering`, `engineering_discipline`, `tool_use`, `markdown_rules`, `auto_appended_context`, `base_rules`, `available_skills`/`skill`*, `user_custom_instructions`, `project_rules`/`agents_md`/`rule`, `environment_info`, `available_modes` |
| User turn | User text plus an `<environment_details>` block: `current_datetime`, `current_mode`, `active_file`, `git_status_snapshot`, `external_changes` |
| Loop | `Task setup complete` → `Starting agent loop` → *n* × (request → `Assembled chat response` with `finishReason: tool-calls` → `Tool invoked`) → `Agent loop completed` |
| Steering | `Steering agent`: the user adds a message while the loop is still running |
| Multi-turn | A finished task is resumed later with a new user message on the same history (`taskId` stays the same; the task is persisted in `TaskStore`) |
| Tool usage (count) | `execute_command` 49, `grep` 9, `read_file` 8, `search_and_replace` 4, `write_file` 3, `use_skill` 3, `apply_diff` 1 |
| Tool errors | Tool errors go back to the model as plain tool-message text (e.g. `path must be an absolute path …`). The loop never fails because of a tool error. |
| Background processes | `execute_command` with `background: true` returns a pid and a log path. The model then reads the log with `read_file`. |
| MCP | MCP tools are named `mcp__<server>__<tool>` (e.g. `mcp__fp-go__get_example`). There is a stdio transport, servers can be reconnected, and a server that fails to start is skipped. |
| Usage | Input, output, cache-read and reasoning tokens for each request |
| Gateway | Every HTTP request is logged with its elapsed time. On a 401 the token is refreshed and the request retried once. |

What the harness has today:

- [`openai.ChatCompletion`](openai/request.go): one non-streaming call, as a `Kleisli[ChatCompletionDeps, Params, *ChatCompletion]`.
- [`openai.MakeDeepSeekChatCompletionDeps`](openai/deepseek.go): provider wiring (env + HTTP).
- [`tools.HandleToolCalls`](tools/tools.go): runs tool calls through a `ToolCaller` registry. Errors become tool messages.
- [`session.Next`](session/session.go): one step of the loop as a `Trampoline`, with history, usage monoid and iteration counter.
- [`main.go`](main.go) is empty. There are no real tools, the loop is never driven to the end, and there is no system prompt assembly, persistence or streaming.

---

## 1. fp-go rules for every step

These apply to every phase. Before writing a combinator, check it with the `fp-go` MCP server
(`mcp__fp-go__search_examples`, `mcp__fp-go__get_example`, `mcp__fp-go__use_skill`).
Don't write signatures from memory.

1. **Dependencies are interfaces with getters.** Follow the `XxxDeps` / `MakeXxxDeps` / `AsXxxDeps[R XxxDeps](r R) XxxDeps` pattern
   (see [env/env.go](env/env.go), [http/http.go](http/http.go)). Bigger dependency sets embed smaller ones
   (see `SessionDeps`, `DeepSeekDeps`).
2. **Effectful operations are `effect.Kleisli[Deps, In, Out]`.** Build them with `effect.Asks`, `Map`, `Ap`, `Chain`,
   `FromThunk` and `ChainThunkK` (see [AGENTS.md](AGENTS.md)). To reuse an existing Kleisli in a wider
   environment, use `effect.Local[A](AsXxxDeps[Wider])`. Don't rebuild the double-`Reader` shape by hand.
3. **Use point-free `F.Flow`/`F.Pipe` with named intermediate values**, as in `session.Next`.
   No `if err != nil`, no imperative loops, no mutation outside lenses.
4. **Change state through optics.** Add `// fp-go:Lens` to new structs and run `go generate`.
   Reuse the lenses generated for the openai types (extend the `//go:generate` lists in
   [openai/doc.go](openai/doc.go) and [tools/doc.go](tools/doc.go) as needed).
5. **Model tool failures as data.** A failed tool returns a tool message. It never fails the `Effect`
   (this is how [`tools.MakeToolCall`](tools/tools.go) works today). Only infrastructure errors
   (network, auth, cancellation) use the `Result` error channel.
6. **Combine values with monoids**: usage, endomorphisms over `ChatCompletionNewParams`, and prompt sections.
7. **Loops are stack-safe.** Use `tailrec.Trampoline` (`Bounce`/`Land`). Never use recursion inside `Effect`.
8. **Wrap side effects at the edge.** Use `ioresult.Eitherize*`, `result.TryCatchError` and `thunk.FromIOResult`.
   Wrap each OS/SDK call once, behind a `Deps` getter, so tests can replace it.
9. **Type aliases go in each package's `types.go`**, like the existing packages.
10. **Clean up afterwards.** Run `runCodeActions.bat` to remove unneeded type arguments, then `go vet ./...` and `go test ./...`.

---

## 2. Phases and steps

### Phase A: Close the loop (smallest working agent)

- [x] **A1 Clean up TODOs.** Move `headPrism` ([session/session.go](session/session.go)) and
  `effectFromReaderResult` ([tools/tools.go](tools/tools.go)) to a small `internal/fpx` package.
  First check whether fp-go already has them (`A.Head` prism, `effect.FromReaderResult`) and use
  those if it does. *Test:* the existing tests pass.
- [x] **A2 Loop driver.** Add `session.Run() effect.Kleisli[SessionDeps, Session, FinalResult]`, which drives
  `Next` to `Land`. Use an effect-level tailrec combinator if fp-go has one; otherwise write one helper in `internal/fpx`.
  *Test:* a fake `ChatCompletionDeps` that returns `tool_calls` twice and then `stop` → 3 iterations,
  history length 2, usage summed.
- [x] **A3 Iteration limit.** Add a `MaxIterations` option to `SessionDeps` (getter). When it is reached, `Next` lands
  with a typed error result. *Test:* a fake that always returns tool calls stops at N.
- [x] **A4 Tool definitions in the request.** `ToolCaller` is a lookup only, so the model never sees the tools.
  Add `ToolRegistry` (name → `{definition openai.ChatCompletionToolUnionParam, call ToolCall}`). Derive
  `ToolCaller` from it, and add an endomorphism that puts `Tools` into `ChatCompletionNewParams`.
  *Test:* the registry serialises to the expected JSON schema.
- [x] **A5 `main.go` CLI.** Read a prompt from the arguments, use ask mode (`openai.ForAskMode`) with
  DeepSeek, run `session.Run` and print the final message and usage. Compose all wiring as
  `Effect`s and run them only once, in `main`. *Test:* a manual smoke test with `DEEPSEEK_API_KEY`.

### Phase B: Replay fixtures from the recordings

These come before the real tools so that every later step can be tested against real traffic.

- [x] **B1 Log reader.** Add a `replay` package that streams JSONL records (`ts`, `level`, `module`, `msg`, `taskId`, `data`)
  as an `IOResult` sequence. *Test:* it parses the small 2026-09-24 log.
- [x] **B2 Payload extraction.** Use a prism to pick out `HTTP request body for /chat/completions:` and
  `Assembled chat response:` records and decode them into `openai.ChatCompletionNewParams` and a
  response DTO. *Test:* the payload count matches the counts above (126 requests, 71 responses).
- [x] **B3 Task grouping.** Group the records by `taskId` into `[]Turn` (request, response, invoked tools).
- [x] **B4 Replay `ChatCompletionDeps`.** Make a fake that answers with the recorded responses in order.
  Then `session.Run` replays a recorded task end to end without a network.
- [ ] ~~**B5 Copy the fixtures.**~~ *Skipped:* the recordings are not
  available. Copy 2–3 redacted tasks into `testdata/` (no tokens or internal URLs).
  Remove secrets with an endomorphism over the parsed records.

### Phase C: Core tools (in order of recorded use)

Each tool is its own `tools/<name>` file. It provides a `ToolRegistry` entry, a typed args struct
(decoded with `J.Unmarshal` into `Result`), its own `XxxDeps` (filesystem/process access behind getters)
and a text result. Validation errors (such as "path must be absolute") come back as tool text.

- [x] **C1 Workspace dependency.** Add `WorkspaceDeps` with `GetWorkspaceRoot()` and path resolution/validation as a
  `ReaderResult[string, string]`.
- [x] **C2 `read_file`.** Read a file with line numbers and an optional `range` (`start-end`). Output format:
  `Contents of file X:\n\n1 | …`, matching the recordings.
- [x] **C3 `list_files`, `glob`.** Implement with `io/fs.WalkDir` behind a getter; return paths relative to the workspace.
- [x] **C4 `grep`.** Search with `regexp` (or call `rg` when it is installed, through `ProcessDeps`). Support
  `include`, `ignore_case`, `invert_match`, `word_regexp`, `files_with_matches`, cap the results at 100 and group them by file.
  Skip what `.gitignore` files exclude (`github.com/go-git/go-git/v5/plumbing/format/gitignore`): `.git/info/exclude`,
  the `.gitignore` files from the workspace root down to the searched directory, and those below it.
  Not done yet: the `rg` fallback (needs `ProcessDeps` from C5) and `core.excludesFile`.
- [x] **C5 `execute_command` (foreground).** Add `ProcessDeps` (`exec.CommandContext` wrapped once).
  Support `cwd` and `timeout_seconds` through `context.WithTimeout` inside the `Thunk`.
  Output format: stdout plus a `Stderr:` section, and the exit code if it is not zero.
  A non-zero exit code fails the call, so the model sees `Exit code: N\n\nStdout:\n…\n\nStderr:\n…`
  as the tool error, as recorded. `background` is not in the definition yet (C6).
- [x] **C6 `execute_command` (background).** Start the process, send its output to a log file and return the pid and path
  (format as in the recordings). Register the process in an `IORef`-backed table owned by `ProcessDeps`.
  The result text was written without comparing it to the recordings. The log directory is a parameter of `MakeDefaultProcessDeps`; it has to lie inside the workspace so that
  `read_file` can read the logs. Processes leave the table when they exit. Not done yet: stopping them (E4).
- [x] **C7 `write_file`.** Write the whole file and create parent directories. Check `line_count` to detect truncated content.
  Content with fewer lines than `line_count` fails as truncated; more lines pass, because models miscount the last line.
  The definition and the result text were written without comparing them to the recordings. Registered in the CLI
  without approval until I1.
- [x] **C8 `search_and_replace`.** Replace literal text or a regex, optionally within a line range. Implement it as a pure
  `Endomorphism[string]` with I/O only around it.
  A search without a match fails, so the model learns that its search text is stale. The file is edited with LF line
  endings and keeps CRLF if it has them, so multi-line searches match what `read_file` shows. The definition and the
  result text were written without comparing them to the recordings. Registered in the CLI without approval until I1.
- [x] **C9 `apply_diff`.** Parse `SEARCH`/`REPLACE` blocks. Applying them is a pure `func(string) Result[string]`
  (fold over the blocks, fail on the first block that doesn't match).
  Each block replaces one literal occurrence: the one nearest its optional `:start_line:`, else the first.
  The file is edited with LF line endings and keeps CRLF if it has them. The definition and the result text
  were written without comparing them to the recordings. Registered in the CLI without approval until I1.
- [x] **C10 `insert_content`.** Insert lines at a given line, or append when the line is 0.
  The content always ends with a line break, and a file without a final line break gets one, so the
  inserted lines never join a neighbour. A line beyond the end of the file appends. The file is edited
  with LF line endings and keeps CRLF if it has them. The result text was written without comparing it
  to the recordings. Registered in the CLI without approval until I1.
- [x] **C11 Output limits.** Add a monoid-free wrapper `tools.Truncate(n)` that shortens oversized tool output (the recordings
  contain 37 KB results) and adds a note that the output was truncated. Apply it to every tool as a
  `ToolCall` → `ToolCall` endomorphism.
  `tools.TruncateTools()(n)` applies it to a whole registry; the CLI limits every tool to 50,000 bytes.
  The limit counts bytes and never splits a UTF-8 sequence. Error text is cut too, because it is
  the tool message; shorter errors are kept as they are. The note was written without comparing it
  to the recordings.

### Phase D: Prompt assembly

- [x] **D1 `PromptSection` monoid.** Define `type Section = Pair[tag, body]`, render it as `<tag>\nbody\n</tag>` and
  concatenate sections with `A.Fold` and a string monoid.
  In the `prompt` package, `RenderSections` is `A.FoldMap` over `RenderSection` with
  `S.IntersperseMonoid("\n\n")`: a blank line between sections, and an empty rendering adds no
  separator. Not wired into the CLI yet (D2–D5).
- [x] **D2 Static sections.** Embed (`//go:embed`) role_definition, base_rules, tool_use and markdown_rules as files
  under `prompts/`, as `ask.txt` is today.
  `prompt.StaticSections()` returns them in the recorded order (role_definition, tool_use, markdown_rules,
  base_rules) from `prompt/prompts/*.txt`, with LF line endings and no trailing line break. The texts were
  written without comparing them to the recordings. Not wired into the CLI yet (D5).
- [x] **D3 `environment_info`.** Build it as an `Effect[EnvInfoDeps, Section]` from OS, shell, workspace root and date.
  `prompt.EnvironmentInfo()` reads the OS, architecture, shell and workspace root as a `Reader` over
  `EnvInfoDeps` (which embeds `tools.WorkspaceDeps`) and the clock as an `IO` getter, so tests fix the date.
  `MakeDefaultEnvInfoDeps` uses `runtime.GOOS`/`GOARCH` and `io.Now`. The body follows the recorded
  `### Workspace Info` / `### Command Execution Environment` headings; the OS release is not included.
  Not wired into the CLI yet (D5).
- [x] **D4 `project_rules`.** Read `AGENTS.md` (and `.bob/rules*`) from the workspace and wrap it as `<agents_md><rule>…`.
  A missing file gives `option.None`, which yields no section.
  `prompt.ProjectRules()` is an `Effect[ProjectRulesDeps, Option[Section]]` (workspace, `tools.FileDeps`,
  `tools.WalkDeps`). `AGENTS.md` becomes `<agents_md><rule>…</rule></agents_md>`, followed by one `<rule>` per
  file directly in `.bob/rules/`, in lexical order. Missing or empty files and a missing directory are left
  out; other read errors fail. Mode-specific `.bob/rules-<mode>/` directories wait for D5. Not wired into the
  CLI yet (D5).
- [x] **D5 Mode definitions.** Generalise `openai/modes.go` to a `Mode{ID, Name, Prompt, AllowedTools}`
  table (Agent, Plan, Ask). The system prompt and tool list for a mode are
  `Effect[ModeDeps, ChatCompletionNewParams]`.
  The [`mode`](mode/) package replaces `openai/modes.go` and the CLI's own mode table. `mode.Modes()` lists
  Agent (every tool), Plan and Ask (the read-only tools); each is a Markdown file in `mode/modes/`, with id, name, description, order and tools in its
  YAML front matter (split by `mode.SplitFrontMatter`, decoded with `go.yaml.in/yaml/v3`; unknown keys and tools are rejected) and the role
  prompt as its body, so a new mode is a new file (`mode.ReadModes` reads them from any `fs.FS`). Plan's
  was written without comparing it to the recordings. `mode.Request()` is an
  `effect.Kleisli[ModeDeps, Mode, ChatCompletionNewParams]`: the system prompt as the only message, rendered from
  role_definition (the mode's prompt, which replaces the static `role_definition.txt`), the static sections
  (without the rules that name a tool the mode doesn't allow, so Plan and Ask aren't told to write files),
  project_rules and environment_info, plus the definitions of the tools the mode allows (`tools.SelectTools`).
  `prompt.ProjectRules()` now takes the mode's ID and adds the rules in `.bob/rules-<id>/` after those in
  `.bob/rules/`. `ModeDeps` takes the workspace, files and listings from `tools.ToolsDeps`, so the prompt
  reads the workspace the tools work in. The CLI (`--mode agent|plan|ask`) uses it, so D1–D4 are now wired in.
  The `available_modes` section waits for H5.
- [x] **D6 `<environment_details>` per turn.** Add an endomorphism that wraps each user message with datetime, mode
  and git status (`git status --porcelain` through `ProcessDeps`). *Test:* golden file against a recorded user message.
  `prompt.EnvironmentDetails()` is an `effect.Kleisli[EnvDetailsDeps, string, Endomorphism[string]]` over the mode's
  name: the user's text, a blank line, then `<environment_details>` with `current_datetime` (UTC, milliseconds),
  `current_mode` and `git_status_snapshot` (`git status --porcelain --branch` in the workspace root, through the
  `RunProcess` of `tools.ProcessDeps`, in a code fence). A non-zero exit code (no repository) leaves the git
  section out; a git that cannot be started fails the effect. The golden file follows the shape of a current user
  message, since the recordings are not available (B5). The CLI wraps its prompt with it; `active_file` and
  `external_changes` wait for an editor and I3.
- [x] **D7 Replay comparison.** Compare the assembled system prompt section by section with a recorded one (tag order and presence).
  `prompt.SectionTags` lists the tags of the top-level sections of a rendered prompt (a line that is only an opening tag
  starts a section, the line that closes it ends it, nested tags are skipped), and `prompt.CompareSections` compares a
  recorded prompt with an assembled one as a `SectionDiff`: the tags missing and extra, and the shared ones in each
  prompt's order. `replay.SystemPrompt` reads the system message of a recorded request (a string or text parts, joined
  by line breaks) and `replay.SystemPrompts` yields one per request of a log. The recording is synthetic
  (`mode/testdata/recorded-system-prompt.log`, the tags of §0 in their order), since the real ones are not available (B5).
  The test fixes the gap: the Agent prompt lacks investigate_before_answering, engineering_discipline,
  auto_appended_context, available_skills (H1), user_custom_instructions and available_modes (H5), adds nothing,
  and keeps the recorded order.
- [x] **D8 Custom modes.** Read the mode files of `~/.bob/modes/` and `<ws>/.bob/modes/` with `mode.ReadModes` over
  `os.DirFS` (a missing directory is no modes, `ioresult.CatchIs(fs.ErrNotExist, …)`), and merge them with the built-in
  ones: the workspace wins over the home directory, both over the built-ins, by `id` (precedence is a monoid, as in G1).
  Two files with the same `id` in one directory are an error. `Lookup` and `IDs` take the merged table through
  `ModeDeps`. *Test:* `fstest.MapFS` per directory; an override, a new mode and a duplicate.
  `mode.ReadModeDirs()` is a `Reader[func(string) fs.FS, ioresult.Kleisli[[]string, []Mode]]`: the reader's function
  opens a directory (`os.DirFS` in the CLI, a map of `fstest.MapFS` in the tests), the directories are read
  concurrently, lowest precedence first. Each one is indexed by `mode.IndexModes` (a duplicate `id` is an error with the
  directory in it), and `mode.MergeModes` merges the indexes over the built-in modes with `R.UnionLastMonoid`, sorted
  by order, then id. No `CatchIs` was needed: `fs.Glob` already ignores the error of reading a missing directory, so
  it has no modes. `ModeDeps` embeds the new `ModeTableDeps` (`GetModes`), and `Lookup` and `IDs` are readers over it.
  The CLI reads the table from `~/.bob/modes/` and `<ws>/.bob/modes/` before it looks the mode up; `--help` lists the
  built-in modes and names the two directories.

### Phase E: Session lifecycle

- [ ] **E1 Multi-turn.** Add `session.Continue(userText)`, an endomorphism that appends a user message to `current` and
  resets the per-turn iteration counter. History is kept.
- [ ] **E2 Persistence.** Add `TaskStoreDeps` with `Save: Kleisli[Session, Unit]` and `Load: Kleisli[TaskID, Session]`
  (JSON files under `~/.fp-go-harness/tasks`). `Session` needs exported DTO fields or a codec; use an `Iso`
  between `Session` and its DTO.
- [ ] **E3 Steering.** Add `SteeringDeps.Poll(): Thunk[[]string]`. Before each `Next` request, read the queued user messages
  and append them. `Next` stays pure in its loop structure.
- [ ] **E4 Cancellation.** Pass the `context.Context` of the `Thunk` through. Ctrl-C cancels the request and the tools that
  are running. Return the partial session as a `Land` result.
- [ ] **E5 Interactive REPL.** Change `main.go` so it loops over stdin turns (`Continue` → `Run` → print). This loop
  runs at the edge of the program; it can be an `IO` loop through `tailrec`.

### Phase F: Streaming and observability

- [ ] **F1 Streaming request.** Add `openai.ChatCompletionStream()` as a Kleisli that returns a stream of chunks, and
  fold the chunks into a `*ChatCompletion` with `openai.ChatCompletionAccumulator`. Keep the
  Kleisli signature the same as `ChatCompletion` so the two can be swapped through `ChatCompletionDeps`.
- [ ] **F2 Token sink.** Add `OutputDeps.Emit(delta)` so text appears while it streams. The fold calls it as a side
  effect inside the thunk.
- [ ] **F3 Structured logging.** Add `LogDeps` with JSONL output in the recorded record shape
  (`module`, `msg`, `taskId`, `data`), and log `Starting agent loop`, `Tool invoked`,
  `Agent loop completed` and the request/response payloads. Then Phase B can replay the harness's *own* logs.
- [ ] **F4 Usage details.** Extend `MakeUsageMonoid` with cache-read tokens and print the cost per turn.
- [ ] **F5 HTTP retry.** Use a `RoundTripper` decorator in `http.HttpDeps` that retries 429/5xx with backoff.
  Use fp-go `retry` if it exists for `Effect`; check with the MCP server.

### Phase G: MCP client

- [ ] **G1 Config.** Parse `.mcp.json` / `~/.bob/settings/mcp.json` with workspace precedence (precedence is a
  record monoid that prefers the workspace config).
- [ ] **G2 stdio session.** Add `MCPDeps` using `github.com/modelcontextprotocol/go-sdk` (already an indirect dependency).
  `Connect: Kleisli[ServerConfig, Session]`. A failed start turns into `option.None` and is logged, as in the
  recordings (`Session failed to start` → skip).
- [ ] **G3 Tool bridge.** Turn `tools/list` into `ToolRegistry` entries named `mcp__<server>__<tool>`, with
  `call` → `tools/call`. Merge them with the core registry (registry monoid, core tools win on name clashes).
- [ ] **G4 Dogfood.** Connect this repo's `fp-go` server from [.mcp.json](.mcp.json). The harness can then use
  `mcp__fp-go__search_examples` while it works on itself.
- [ ] **G5 Reconnect.** Retry with backoff and a maximum number of attempts (as in `Scheduling reconnect attempt` → `Max reconnect attempts reached`).

### Phase H: Skills, todos, subtasks

- [ ] **H1 Skill discovery.** Scan the skill directories (`~/.bob/skills`, `~/.claude/skills`, `<ws>/.bob/skills`, …)
  for `SKILL.md` front matter and render them into the `<available_skills>` section.
- [ ] **H2 `use_skill`.** Implement it as a tool that returns the body of the skill file (recorded result: the `SKILL.md` text).
- [ ] **H3 `update_todo_list`.** Keep the todo list in `Session` (lens) and render it into the next `environment_details`.
- [ ] **H4 `ask_followup_question`.** Implement it through `OutputDeps`/`InputDeps`: it waits for user input inside the thunk.
- [ ] **H5 `switch_mode`.** Implement it as an endomorphism on `Session` that swaps the system prompt and tool set (Phase D5).
- [ ] **H6 `spawn_subagent` / `start_subtask`.** Run a nested `session.Run` with a fresh `Session` and narrower deps
  (`effect.Local`). Return its final text as the tool result.

### Phase I: Safety and approval

- [ ] **I1 Approval hook.** Add `ApprovalDeps.Approve: Kleisli[ToolInvocation, bool]` and wrap write/exec tools in it.
  A denial goes back to the model as a tool message.
  The CLI already registers `write_file` and `execute_command` (`tools.MakeTools`) without approval: wrap them there.
- [ ] **I2 Allow and deny lists** for commands (prefix match) and paths outside the workspace.
- [ ] **I3 External-change detection.** Take a snapshot of file mtimes after each turn and report the diff as
  `<external_changes>` (Phase D6).

---

## 3. Milestones

| Milestone | Steps | Result |
|---|---|---|
| M1 "Hello agent" | A1–A5 | A CLI that answers questions with tools, without hanging |
| M2 "Replayable" | B1–B5 | Recorded tasks replay offline in `go test` |
| M3 "Can code" | C1–C11, D1–D8 | Reads, searches, edits files and runs commands with a tagged system prompt |
| M4 "Conversational" | E1–E5, F1–F4 | Streaming REPL with multiple turns, steering and persistence |
| M5 "Feature parity" | F5, G1–G5, H1–H6, I1–I3 | MCP, skills, modes, subagents and approvals. Can run a session like the recorded PR-review task |

## 4. Definition of done (every step)

- The new code follows the rules in section 1 (Deps getters, Kleisli/Effect composition, optics, no imperative error handling).
- `go generate ./...`, `runCodeActions.bat`, `go vet ./...` and `go test ./...` pass.
- There is at least one unit test that uses fake Deps, and a replay test (from Phase B on) where it applies.
