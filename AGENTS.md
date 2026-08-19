# AGENTS.md

## fp-go

This project is built on [github.com/IBM/fp-go](https://github.com/IBM/fp-go) (functional programming
primitives for Go: Reader, IOResult, ReaderIOResult, Effect, Kleisli composition, etc.).

- Before writing or reviewing fp-go code, use the `fp-go` MCP server (configured in
  [.mcp.json](.mcp.json), started via `go tool gen mcp`) to load its skills and examples.
  Prefer it over guessing combinator signatures or behavior from memory.
