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
- **CLI (ask and agent mode)**: `go run . [--mode ask|agent] "<prompt>"` runs one prompt
  with DeepSeek in the workspace (the current directory unless `--workspace` says
  otherwise) and prints the answer and the token usage. The command is built on
  [urfave/cli](https://github.com/urfave/cli) (`go run . --help`). Its configuration
  (`MODE`, `WORKSPACE`, `MAX_ITERATIONS`, `MAX_TOOL_OUTPUT`) is decoded by an fp-go codec
  from `.env`, the process environment and the flags, the later source winning; each
  variable has a flag of the same name in kebab case (`--max-iterations`).
  - **ask** (the default) answers questions with the read-only tools (`read_file`,
    `list_files`, `glob`, `grep`).
  - **agent** carries out a task with every tool, including `write_file` and
    `execute_command`. These run without approval until PLAN.md I1.
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
- **Prompt sections**: `prompt.RenderSections` renders tagged sections as
  `<tag>\nbody\n</tag>` and joins them with a blank line through a string monoid.
  `prompt.StaticSections` holds the embedded `role_definition`, `tool_use`,
  `markdown_rules` and `base_rules` sections, and `prompt.EnvironmentInfo` builds the
  `environment_info` section (workspace root, OS, architecture, shell, date) as an
  `Effect` over `EnvInfoDeps`. `prompt.ProjectRules` reads `AGENTS.md` and the files in
  `.bob/rules/` from the workspace into the `project_rules` section; without rules there
  is no section. They are the base of the system prompt assembly in
  Phase D and are not wired into the CLI yet.

## 🚀 Quick start

Requires Go 1.27 or later and a DeepSeek API key.

```sh
cp .env.example .env        # then set DEEPSEEK_API_KEY
go run . "What is a Kleisli arrow?"
go run . --mode agent "Add a doc comment to every exported function in env/env.go"
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
| [`main`](main.go) | Runs the CLI command until Ctrl-C |
| [`cli`](cli/) | The urfave/cli command: decodes the `Config` with a codec from `.env`, the environment and the flags, picks the mode's system prompt and tools, wires the DeepSeek dependencies, runs the loop |
| [`session`](session/) | `Session` state, one loop step (`Next`), the driver (`Run`), usage monoid, iteration limit |
| [`openai`](openai/) | `ChatCompletion` as a Kleisli over the OpenAI-compatible API, DeepSeek provider wiring, modes and system prompts |
| [`tools`](tools/) | Tool registry, tool-call dispatch, workspace path resolution and the tools themselves |
| [`prompt`](prompt/) | Tagged system prompt sections, their monoid, the embedded static sections, `environment_info` and `project_rules` |
| [`replay`](replay/) | Reads, redacts, groups and replays recorded session logs for tests |
| [`env`](env/) | Environment variables from the process and `.env` |
| [`http`](http/) | HTTP client dependency |

## 🗺️ Roadmap

[PLAN.md](PLAN.md) has the full plan, one PR per step.

| Phase | Topic | Status |
|---|---|---|
| A | Close the loop | ✅ done |
| B | Replay fixtures from the recordings | ✅ B1–B4 done, B5 skipped |
| C | Core tools | ✅ done |
| D | Prompt assembly | ⏳ D1–D4 done |
| E | Session lifecycle (multi-turn, persistence, steering, REPL) | ⏳ open |
| F | Streaming and observability | ⏳ open |
| G | MCP client | ⏳ open |
| H | Skills, todos, subtasks | ⏳ open |
| I | Safety and approval | ⏳ open |

## 🛠️ Development

```sh
go test ./...      # live and replay tests are skipped without their .env variables
go generate ./...  # regenerates the lenses (directives in each package's doc.go)
```

[CI](.github/workflows/build.yml) checks `go mod tidy`, runs `go vet` and the tests with the
race detector on the latest Go release. Every push to `main` runs semantic release, which
derives the version and release notes from the conventional commit messages.

The coding rules (fp-go composition patterns, generated lenses, naming, PR conventions) are
in [AGENTS.md](AGENTS.md) and the [repository skills](.bob/skills/).
