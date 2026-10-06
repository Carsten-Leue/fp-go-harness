# AGENTS.md

## fp-go

This project is built on [github.com/IBM/fp-go](https://github.com/IBM/fp-go) (functional programming
primitives for Go: Reader, IOResult, ReaderIOResult, Effect, Kleisli composition, etc.).

- Before writing or reviewing fp-go code, use the `fp-go` MCP server (configured in
  [.mcp.json](.mcp.json), started via `go tool gen mcp`) to load its skills and examples.
  Prefer it over guessing combinator signatures or behavior from memory.
- Load the repository skills [`fp-go`](.bob/skills/fp-go/SKILL.md) and
  [`go-conventions`](.bob/skills/go-conventions/SKILL.md) for every Go change.
- Field access goes through generated lenses (`go generate`, directives in each package's
  `doc.go`), also for third-party types; no hand-written getters where the generator works.
  The generator's pitfalls are in
  [`go-conventions`](.bob/skills/go-conventions/SKILL.md#generator-rules-and-pitfalls-go-tool-gen-lens-fp-go-v240).

### Composing `Effect[C, A]`

Build `Effect`s with the `effect` package's own combinators (`Asks`, `Map`, `Ap`, `Chain`,
`FromThunk`, `Local`, `TailRec`), not by rebuilding its internal double-`Reader` shape from
`reader` / `context/readerioresult`. Use `Asks` + `Map` to build an effect from a `Deps` getter,
and `Local` to reuse an existing `Effect` / `Kleisli` under a different environment. The full rules
and examples are in the [`fp-go` skill, section *Composing `Effect[C, A]`*](.bob/skills/fp-go/SKILL.md#composing-effectc-a);
[openai/request.go](openai/request.go) `ChatCompletion` is the worked example.
