# 🧩 fp-go-harness

[![CI](https://github.com/Carsten-Leue/fp-go-harness/actions/workflows/build.yml/badge.svg)](https://github.com/Carsten-Leue/fp-go-harness/actions/workflows/build.yml)

An agentic coding harness in Go, built entirely from the functional effects of
[fp-go](https://github.com/IBM/fp-go). It sends a prompt to an LLM, runs the tools the model
asks for, feeds the results back, and repeats until the model answers.

Every effectful step is an `effect.Kleisli[Deps, In, Out]`, dependencies are interfaces read
through getters, state changes go through generated lenses, and the agent loop is a
stack-safe trampoline. So the whole harness can run against a real API, a fake, or a
replayed recording without code changes.

The goal is a complete coding-agent harness. [PLAN.md](PLAN.md) derives the steps from
recorded agent sessions.

## ✅ What works today

- **Agent loop**: `session.Run` drives the conversation until the model stops calling
  tools, or until the iteration limit (25 by default in the CLI) is reached. It accumulates token usage
  with a monoid. Tool failures go back to the model as tool messages and never stop the loop.
- **Multi-turn sessions**: the session `Run` returns holds the whole conversation, including the
  model's last answer. `session.Continue()(text)` appends the next user message and resets the
  per-turn iteration counter; history and usage carry over, so the next `Run` continues the
  conversation.
- **Persistence**: `session.Save()` stores a `Task` (a task ID and its session) as a JSON file
  `<id>.json` in a tasks directory (`session.DefaultTasksDir()` is `~/.fp-go-harness/tasks`),
  and `session.Load()` reads it back with its whole history, request, usage and iteration count,
  ready for `Continue`. The session is encoded through an `Iso` to its exported `SessionDTO`.
  The CLI doesn't save sessions yet.
- **Steering**: before each request, `session.Run` polls `SteeringDeps.GetPoll()` for the user
  messages queued while the turn runs and appends them (`session.Steer()`), so the user can
  redirect the agent without waiting for its answer. `session.ChannelSteering(queue)` takes them
  from a channel without blocking; the one-shot CLI uses `session.NoSteering()`.
  `session.RunPrompt()` is `Run` for a turn that a prompt started: its first request carries
  the prompt only, and messages queued before it wait for the second request or the next
  prompt.
- **Cancellation and the partial session**: `session.Run` never fails. It returns a
  `TurnResult`, the session as far as the turn got paired with its `Outcome`: the final
  completion, or the error that ended the turn (a cancellation, a failed request or poll, the
  iteration limit). The session keeps every round trip whose request returned, the usage so far
  and the steered messages already taken from the queue, so a cancelled turn can be continued.
  `session.IsCancelled()` matches a cancellation. `tools.StopProcesses()` kills the background
  processes of the process table (the shell process only; its children may survive).
- **CLI (agent, plan and ask mode)**: `go run . [--mode agent|plan|ask] "<prompt>"` runs one
  prompt with DeepSeek in the workspace (the current directory unless `--workspace` says
  otherwise) and prints the answer and the token usage. The command is built on
  [urfave/cli](https://github.com/urfave/cli) (`go run . --help`). Its configuration
  (`MODE`, `WORKSPACE`, `MAX_ITERATIONS`, `MAX_TOOL_OUTPUT`) is decoded by an fp-go codec
  from `.env`, the process environment and the flags, the later source winning; each
  variable has a flag of the same name in kebab case (`--max-iterations`).
  A turn that does not answer prints the error, the last assistant text of the session and
  the token usage on stderr. Ctrl-C stops the running turn and the background processes it
  started, and the CLI exits with 130. A second Ctrl-C, while the turn stops, exits at once,
  without the partial-session report and without stopping the background processes. Other
  errors exit with 1.
  - **agent** carries out a task with every tool, including `write_file` and
    `execute_command`. These run without approval until PLAN.md I1.
  - **plan** investigates the workspace with the read-only tools (`read_file`,
    `list_files`, `glob`, `grep`) and answers with a plan.
  - **ask** (the default) answers questions with the read-only tools.
  - **custom modes** are mode files in `~/.bob/modes/` and `<workspace>/.bob/modes/`. A custom
    mode with the id of another one replaces it: the workspace wins over the home directory,
    both over the built-in modes.
- **REPL**: without a prompt, or with `--interactive`, the CLI holds a conversation over
  stdin. Each line is the next turn of one session (`Continue` → `RunPrompt`), reported like a
  one-shot turn; `> ` on stderr marks the prompt. Lines typed while a turn runs steer it: they
  are sent as plain user messages with its next request, after the tool results. A line that
  arrives after the turn's last poll, that is, while its last request runs, is the next turn's
  line. A failed turn is reported and the next line continues its partial session. The prompt
  given with `--interactive` is the first line. The REPL ends with exit code 0 at the end of
  the input or on Ctrl-C at the prompt, even after a failed turn; Ctrl-C during a turn reports
  it, stops its background processes and exits with 130.
- **Modes and system prompt**: `mode.Modes` is the table of modes, each with its role and
  the tools it allows. A mode is a Markdown file in [mode/modes/](mode/modes/): id, name,
  description, order and tools in its YAML front matter, the role prompt as its body. A new
  file adds a mode; unknown keys and unknown tools are errors. `mode.ReadModeDirs` merges the
  custom modes of the home directory and the workspace over the built-in ones by id (two files
  with the same id in one directory are an error), and `mode.Lookup` finds a mode in that
  merged table. `mode.Request` assembles the first request of a mode: the system
  prompt with the mode's `role_definition`, the static sections, `project_rules` and
  `environment_info`, and the definitions of the mode's tools.
- **Tools**: `read_file`, `list_files`, `glob`, `grep`, `write_file`, `search_and_replace`,
  `apply_diff`, `insert_content` and `execute_command`, each with its definition for the model, confined to the workspace
  root. `write_file` replaces a whole file, creates missing parent directories and rejects
  content that is shorter than its announced `line_count`. `search_and_replace` replaces
  literal text or a regular expression, optionally within a line range, and fails when
  nothing matches. `apply_diff` applies `SEARCH`/`REPLACE` blocks in order, each to the
  occurrence nearest its `:start_line:`, and leaves the file unchanged when a block doesn't
  match. `insert_content` inserts lines before a given line, or appends them for line 0.
  `execute_command` waits
  for a command, or starts it in the background and returns its process id and log file
  under `.fp-go-harness/logs` in the workspace. The CLI cuts every tool result, including
  error text, to 50,000 bytes (`MAX_TOOL_OUTPUT`) and adds a note that the output was truncated.
- **Replay**: reads recorded session logs (JSON lines), redacts sensitive values, extracts
  the chat request and response payloads, and groups the records into tasks and turns.
  A grouped task replays through `session.Run` without a network: replay
  `ChatCompletionDeps` answer each request with the next recorded response.
  `replay.SystemPrompts` picks the system prompt out of each recorded request, and
  `prompt.CompareSections` compares it with an assembled one by the tags of their top-level
  sections: which are missing, which are extra, and whether the shared ones are in the same order.
- **Prompt sections**: `prompt.RenderSections` renders tagged sections as
  `<tag>\nbody\n</tag>` and joins them with a blank line through a string monoid.
  `prompt.StaticSections` holds the embedded `tool_use`, `markdown_rules` and `base_rules`
  sections, without the rules that name a tool the mode doesn't allow, `prompt.RoleDefinition` wraps the role of a mode, and `prompt.EnvironmentInfo`
  builds the `environment_info` section (workspace root, OS, architecture, shell, date) as
  an `Effect` over `EnvInfoDeps`. `prompt.ProjectRules` reads `AGENTS.md`, the files in
  `.bob/rules/` and those in the mode's `.bob/rules-<mode>/` from the workspace into the
  `project_rules` section; without rules there is no section. `prompt.EnvironmentDetails`
  appends an `<environment_details>` block to the user's message (current date and time, mode,
  `git status --porcelain --branch` of the workspace); the CLI sends its prompt with it.

## 🚀 Quick start

Requires Go 1.27 or later and a DeepSeek API key.

```sh
cp .env.example .env        # then set DEEPSEEK_API_KEY
go run . "What is a Kleisli arrow?"
go run . --mode plan "How would I add a retry to the HTTP client?"
go run . --mode agent "Add a doc comment to every exported function in env/env.go"
go run .                    # REPL: one turn per line, Ctrl-D (Ctrl-Z on Windows) ends it
go run . --interactive "Summarize README.md"   # REPL with a first line
```

Variables set in the process environment take precedence over `.env`, and flags over
both.

[cli/example_test.go](cli/example_test.go) runs the CLI use cases (a plain question, reading,
listing, globbing and grepping files, running a command, a failing tool call) end to end
against DeepSeek in a temporary workspace. They are skipped without `DEEPSEEK_API_KEY`;
`go test -v -run TestExample ./cli` prints what the CLI answers for each prompt.
[.env.example](.env.example) lists all variables, including the optional ones for the
replay tests.

## 📦 Packages

| Package | What it does |
|---|---|
| [`main`](main.go) | Runs the CLI command, stops the turn on Ctrl-C and maps the outcome to the exit code (0, 1, 130) |
| [`cli`](cli/) | The urfave/cli command: decodes the `Config` with a codec from `.env`, the environment and the flags, builds the mode's request and tools, wires the DeepSeek dependencies, runs the loop |
| [`session`](session/) | `Session` state, one loop step (`Next`), the driver (`Run`) and its `TurnResult`, the next turn (`Continue`, `RunPrompt`), steering (`Steer`), usage monoid, iteration limit, the task store (`Save`, `Load`) |
| [`openai`](openai/) | `ChatCompletion` as a Kleisli over the OpenAI-compatible API, DeepSeek provider wiring |
| [`mode`](mode/) | The modes (Agent, Plan, Ask and the custom modes of `~/.bob/modes/` and `<workspace>/.bob/modes/`), read from Markdown files with YAML front matter, with their roles and allowed tools, and the request each one starts with: system prompt and tool definitions |
| [`tools`](tools/) | Tool registry, tool-call dispatch, workspace path resolution and the tools themselves |
| [`prompt`](prompt/) | Tagged system prompt sections, their monoid, the embedded static sections, `environment_info`, `project_rules` and the per-turn `environment_details`, and the section comparison with a recorded prompt |
| [`replay`](replay/) | Reads, redacts, groups and replays recorded session logs for tests, and extracts their system prompts |
| [`env`](env/) | Environment variables from the process and `.env` |
| [`http`](http/) | HTTP client dependency |

## 🗺️ Roadmap

[PLAN.md](PLAN.md) has the full plan, one PR per step.

| Phase | Topic | Status |
|---|---|---|
| A | Close the loop | ✅ done |
| B | Replay fixtures from the recordings | ✅ B1–B4 done, B5 skipped |
| C | Core tools | ✅ done |
| D | Prompt assembly | ✅ done |
| E | Session lifecycle (multi-turn, persistence, steering, cancellation, REPL) | ⏳ E1–E4 done, E5 in progress (E5a REPL done) |
| F | Streaming and observability | ⏳ open |
| G | MCP client | ⏳ open |
| H | Skills, todos, subtasks | ⏳ open |
| I | Safety and approval | ⏳ open |
| J | Agent Client Protocol (run as an ACP agent in editors) | ⏳ open |
| K | Agent2Agent protocol (serve tasks to other agents, call remote agents) | ⏳ open |

## 🛠️ Development

```sh
go test ./...      # live and replay tests are skipped without their .env variables
go generate ./...  # regenerates the lenses (directives in each package's doc.go)
```

[CI](.github/workflows/build.yml) checks `go mod tidy`, runs `go vet` and the tests with the
race detector on the latest Go release. Every push to `main` runs semantic release, which
derives the version and release notes from the conventional commit messages. When it tags a
release, [GoReleaser](.goreleaser.yaml) builds the binaries for Linux, macOS and Windows
(amd64, arm64) and attaches them to the GitHub release. They are built with
`-tags urfave_cli_no_template -trimpath -ldflags="-s -w"`, about 40 MB instead of 87 MB:
the tag replaces urfave/cli's `text/template` help printer, whose method lookup by name
keeps the linker from dropping unused methods. To build the same binary locally:

```sh
go build -tags urfave_cli_no_template -trimpath -ldflags="-s -w" .
```

The coding rules (fp-go composition patterns, generated lenses, naming, PR conventions) are
in [AGENTS.md](AGENTS.md) and the [repository skills](.bob/skills/).

Planning and implementation workflows use the [BMAD Method](https://github.com/bmad-code-org/BMAD-METHOD):
the install and its team overrides are in [_bmad/](_bmad/) (the overrides in [_bmad/custom/](_bmad/custom/)
make `bmad-build` and `bmad-code-review` apply the fp-go rules), the skills in
[.claude/skills/](.claude/skills/), and the epics, sprint status and deferred work in
[_bmad-output/](_bmad-output/).
