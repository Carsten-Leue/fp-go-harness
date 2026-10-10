# 🧩 fp-go-harness

[![CI](https://github.com/Carsten-Leue/fp-go-harness/actions/workflows/build.yml/badge.svg)](https://github.com/Carsten-Leue/fp-go-harness/actions/workflows/build.yml)

An agentic coding harness in Go, built entirely from the functional effects of
[fp-go](https://github.com/IBM/fp-go). It sends a prompt to an LLM, runs the tools the model
asks for, feeds the results back, and repeats until the model answers.

Every effectful step is an `effect.Kleisli[Deps, In, Out]`, dependencies are interfaces read
through getters, state changes go through generated lenses, and the agent loop is a
stack-safe trampoline. So the whole harness can run against a real API, a fake, or a
replayed recording without code changes.

The goal is a harness in the style of the Bob coding agent. [PLAN.md](PLAN.md) derives the
steps from recorded Bob sessions.

## ✅ What works today

- **Agent loop**: `session.Run` drives the conversation until the model stops calling
  tools, or until the iteration limit (25 in the CLI) is reached. It accumulates token usage
  with a monoid. Tool failures go back to the model as tool messages and never stop the loop.
- **CLI (ask mode)**: `go run . "<prompt>"` answers one prompt with DeepSeek and the
  ask-mode system prompt. The model can read the current directory with the read-only
  tools. The CLI prints the answer and the token usage.
- **Tools**: `read_file`, `list_files`, `glob`, `grep` and `execute_command` (foreground),
  each with its definition for the model, confined to the workspace root. The CLI
  registers the read-only ones (`read_file`, `list_files`, `glob`, `grep`) for the
  current directory. `execute_command` waits for command approval (PLAN.md I1).
- **Replay**: reads recorded session logs (JSON lines), redacts sensitive values, extracts
  the chat request and response payloads, and groups the records into tasks and turns.

## 🚀 Quick start

Requires Go 1.27 or later and a DeepSeek API key.

```sh
cp .env.example .env        # then set DEEPSEEK_API_KEY
go run . "What is a Kleisli arrow?"
```

Variables set in the process environment take precedence over `.env`.
[.env.example](.env.example) lists all variables, including the optional ones for the
replay tests.

## 📦 Packages

| Package | What it does |
|---|---|
| [`main`](main.go) | CLI: reads the prompt from the arguments, wires the DeepSeek dependencies, runs the loop |
| [`session`](session/) | `Session` state, one loop step (`Next`), the driver (`Run`), usage monoid, iteration limit |
| [`openai`](openai/) | `ChatCompletion` as a Kleisli over the OpenAI-compatible API, DeepSeek provider wiring, modes and system prompts |
| [`tools`](tools/) | Tool registry, tool-call dispatch, workspace path resolution and the tools themselves |
| [`replay`](replay/) | Reads, redacts and groups recorded session logs for tests |
| [`env`](env/) | Environment variables from the process and `.env` |
| [`http`](http/) | HTTP client dependency |

## 🗺️ Roadmap

[PLAN.md](PLAN.md) has the full plan, one PR per step.

| Phase | Topic | Status |
|---|---|---|
| A | Close the loop | ✅ done |
| B | Replay fixtures from the recordings | 🚧 B1–B3 done, B4–B5 open |
| C | Core tools | 🚧 C1–C5 done, C6–C11 open |
| D | Prompt assembly | ⏳ open |
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
in [AGENTS.md](AGENTS.md) and the skills under [.bob/skills](.bob/skills/).
