# AGENTS.md

## fp-go

This project is built on [github.com/IBM/fp-go](https://github.com/IBM/fp-go) (functional programming
primitives for Go: Reader, IOResult, ReaderIOResult, Effect, Kleisli composition, etc.).

- Before writing or reviewing fp-go code, use the `fp-go` MCP server (configured in
  [.mcp.json](.mcp.json), started via `go tool gen mcp`) to load its skills and examples.
  Prefer it over guessing combinator signatures or behavior from memory.

### Composing `Effect[C, A]`

`Effect[C, A]` (`github.com/IBM/fp-go/v2/effect`) is a type alias over
`Reader[C, ReaderIOResult[A]]`. When building a `Kleisli[C, In, Out]` that combines
several dependency-derived values (e.g. a service pulled off `C` plus some config/options
also pulled off `C`), compose using the `effect` package's own combinators —
`Asks`, `Map`, `Ap`, `Chain`, `FromThunk`, `Local` — rather than reaching into the lower-level
`reader` / `context/readerioresult` packages to manually reconstruct `Effect`'s internal
double-`Reader` shape. The latter type-checks but is much harder to read and easy to get
subtly wrong (it requires knowing `Effect`'s internal representation, not just its public API).

Preferred pattern for "fetch dependency A, fetch dependency B, combine them, run the effectful
call": `Asks` to pull a pure `Reader[C, X]` accessor into `Effect[C, X]`, `Map` to apply a
dependency-independent function over it, `Ap` to apply a second `Effect[C, _]`-derived value
applicatively, `Chain(FromThunk[C, _])` to flatten a resulting `Thunk`/`ReaderIOResult` down to
a plain `Effect[C, _]`. See [openai/request.go](openai/request.go) `ChatCompletion` for a worked
example.

Prefer `Asks` + `Map` over `Local` when *building* an `Effect[C, A]` from scratch out of a plain
`Reader` accessor and a pure function — even though `Asks` then needs its type parameters spelled
out explicitly when used point-free (Go can't infer a generic function's type parameters from the
slot it's plugged into in a `Flow`). `Local` is the right choice when you already have an existing
`Effect[C2, A]` value (e.g. a `Kleisli` defined elsewhere) that you want to reuse unchanged under a
narrower/different environment `C1` — that's exactly what it's for, and it only needs one
explicit type parameter (`A`) since `C1`/`C2` are inferred from the accessor.
