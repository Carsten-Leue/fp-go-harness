---
name: fp-go
description: Use this skill whenever writing, generating, changing, refactoring or reviewing Go code in this repository, and always when a Go file imports github.com/IBM/fp-go/v2. It sets the four rules every code change follows - fp-go best practices, monadic over imperative style, point-free style, and the fp-go MCP server as the primary source of truth - with the workflow and the exceptions, the rules for composing Effect[C, A] (effect combinators Asks/Map/Ap/Chain/FromThunk, Local for reuse, Deps getter interfaces, effect.TailRec loops, Provide/RunSync at the boundary), and explains how to set up and start that MCP server.
---
# fp-go — the rules for every code change, and the MCP server behind them

This repository is written in functional Go on top of `github.com/IBM/fp-go/v2`. Every
code change, generated or reviewed, small or large, follows the four rules below. They
are not style preferences: a change that breaks one of them is not done, and a review
reports it as a violation of a documented standard.

| # | Rule | In one line |
|---|---|---|
| 1 | **The fp-go MCP server is the primary source of truth** | Look up every fp-go signature, pattern and best practice there before writing or judging code. Never rely on memory. |
| 2 | **Follow fp-go best practices** | v2 imports, data-last, the simplest monad that fits, `Result` over `Either[error, A]`, lifting with `Eitherize`, lenses, `Tap*` for logging, no hidden mutation. |
| 3 | **Monadic over imperative, wherever possible** | Model failure, absence, effects and iteration with `Result`, `Option`, `thunk` / `Effect`, `Chain`, `Traverse`, not with `if err != nil` chains, `nil` checks and `for` loops. |
| 4 | **Point-free wherever possible** | Compose named functions, lens getters and combinators with `F.Flow` / `F.Pipe`. Lambdas only at the leaves. |

Repository conventions (aliases, header constants, doc comments, tests) are in
[`go-conventions`](../go-conventions/SKILL.md). Load it together with this
skill.

## Why these rules

- **The MCP server ships with the fp-go version pinned in `go.mod`.** Its skills and
  examples match the code in this repository exactly. fp-go is rare in training data, so
  names and type-parameter orders "from memory" are often wrong or belong to v1:
  `Chain` vs `FlatMap`, `Ap[B, A]`, `Provide[A, C]`, `option.GetOrElse(func() A)` vs
  `result.GetOrElse(func(error) A)`. Looking things up first is cheaper than compiling,
  failing and guessing again.
- **Monadic code makes failure and effects part of the type.** A
  `thunk.ReaderIOResult[User]` says that it needs a context, does I/O and can fail. An
  `if err != nil` chain says that only in its body, and every early `return` is a branch
  that has to be tested. `Chain` short-circuits on the first error, and `TraverseArray`
  replaces the loop, the accumulator and the error check with one operator.
- **Point-free code composes.** A pipeline of named steps reads top to bottom as what
  happens, each step can be tested alone, and no lambda can capture and change outer
  state by accident. Code built this way stays composable: the next change adds a
  step and leaves the rest alone.

## Rule 1 — the fp-go MCP server is the primary source of truth

### Order of authority

| For … | Ask, in this order | Never |
|---|---|---|
| fp-go signatures, type-parameter order, which combinator exists | 1. MCP `get_example` / `search_examples` · 2. `go doc github.com/IBM/fp-go/v2/<pkg> <Symbol>` (same pinned version) · 3. existing code in this repository | memory, blog posts, v1 docs |
| fp-go patterns and best practices | 1. MCP `use_skill` (topic skills) · 2. this skill | memory |
| repository conventions (aliases, headers, tests, doc style) | [`AGENTS.md`](../../../AGENTS.md), [`go-conventions`](../go-conventions/SKILL.md) | the MCP server: it doesn't know this repo |

- If the MCP server and this repository's skill files disagree about **fp-go itself**,
  the MCP server wins. Report the outdated skill text so it can be fixed.
- If the MCP server's examples disagree with a **repository convention** (an alias, a
  header constant), the repository convention wins. See [Aliases](#aliases-in-this-repository).
- A signature you couldn't check with the MCP server or `go doc` is **unverified**. Don't
  write it into code or a review suggestion as if it were a fact. Check it, or label it
  "unverified" and say why.

### The tools

| Tool | Use it to |
|---|---|
| `list_skills` | See which topic skills exist. Call it first in a session. |
| `use_skill` | Load a topic skill by name: always `fp-go` (the core), then the topics the task touches. |
| `search_examples` | Search the runnable examples by keyword (an FTS5 query: 1–3 bare words, `OR` to widen, `package_filter` for a package such as `context/readerioresult`; no dots). |
| `get_example` | Fetch the examples of one symbol by exact name (`Eitherize1`, `Type.Method`). |

Which topic skill to load:

| The change touches … | `use_skill` |
|---|---|
| any fp-go code (always) | `fp-go` |
| `Pipe` / `Flow`, point-free refactoring, do-notation (`Bind`, `ApS`, `Let`), `reader` | `fp-go-pipe-flow` |
| lenses, prisms, `// fp-go:Lens`, nested updates | `fp-go-lens` |
| `effect.Effect`, dependencies, `Provide`, `Local`, `Eitherize` into effects | `fp-go-effect` |
| `context.Context`: values, timeouts, cancellation | `fp-go-context` |
| HTTP clients, request builders, header and content constants | `fp-go-http` |
| logging in pipelines (`TapSLog`, `LogEntryExit`) | `fp-go-logging` |
| `switch` / `if`-cascades, routers, fallback chains, type switches | `fp-go-pattern-matching` |
| reviewing a diff or a PR | `fp-go-pr-review` |

### Workflow for generating or changing code

1. `list_skills`, then `use_skill` with `fp-go` and every topic skill from the table above.
2. Design the change as a pipeline first: the types at each step (`A → Result[B]`,
   `Thunk[B] → Thunk[C]`, …) and the monad for each stage (Rule 2, *simplest monad*).
3. For every fp-go function you are about to call, run `get_example` (or
   `search_examples`) and read its signature and usage. If there is no example, run
   `go doc github.com/IBM/fp-go/v2/<pkg> <Symbol>`. Where the server's signature or
   usage differs from what you expected, the server is right.
4. Write the code monadic and point-free (Rules 3 and 4), with the repository's aliases.
5. Build, vet and test: `go build ./...`, `go vet ./...`, `go test ./...`. Fix every
   error; don't present code that hasn't built.

Don't delegate steps 1–3: the signatures and patterns have to be in the context that
writes the code. Step 5 over the whole module prints a lot when it passes and a lot more
when it fails; run it in a **subagent** (`spawn_subagent`) with the brief "From the repository root, run
`go build ./...`, `go vet ./...` and `go test ./...`. Change no file. Return only: per
command pass / fail, and for each failure the package, the test or file:line, and the
first error lines (at most 10 per failure)." Build and test a single package inline.

### Workflow for reviewing code

1. `use_skill` with `fp-go`, `fp-go-pr-review`, `fp-go-pipe-flow` and the topic skills
   the diff touches.
2. Judge the diff against the four rules.
3. **Verify every suggested fix** with `get_example` / `go doc` before you write it into
   the review. A suggestion that doesn't compile costs the author more than no suggestion.
4. A review needs the MCP server. If its tools are missing, the review fails; set the
   server up (see [If the server's tools are missing](#if-the-servers-tools-are-missing))
   instead of reviewing without it.

## Rule 2 — fp-go best practices

The full list is in the MCP server's `fp-go` and `fp-go-pr-review` skills; these are the
rules that come up in nearly every change:

- **v2 import paths only:** `github.com/IBM/fp-go/v2/...`.
- **Data-last, curried form in pipelines:** `result.Map(f)(r)`, not `result.Map(r, f)`;
  `Monad*` forms only when every argument is already at hand.
- **The simplest monad that fits:** none (a plain function) → `Option` → `Result` →
  `IOResult` → `ReaderIOResult` (`thunk`) → `Effect`. Don't wrap a pure computation in
  `thunk` or `Effect`; don't stuff dependencies into `context.Context`.
- **`Result[A]` over `Either[error, A]`**, `ioresult` over `ioeither`, `readerioresult`
  over `readerioeither`.
- **Lift, don't wrap:** `result.Eitherize1(strconv.Atoi)`, `thunk.Eitherize1(repo.Find)`,
  `effect.Eitherize1(...)` instead of hand-written `func(ctx) func() Result` closures.
- **`Map` for `A → B`, `Chain` for `A → M[B]`** (else you get `M[M[B]]`); `ChainResultK`
  to run a `Result` step inside `thunk`; `thunk.FromResult` at the boundary from pure
  `Result` work to context / I/O work.
- **Do-notation for several intermediate values:** `Do`, `Bind` (depends on the state),
  `ApS` (independent: never a `Bind` that ignores its state), `Let`, `LetTo`, with
  generated lenses as setters.
- **Logging with `Tap*`:** `thunk.TapSLog[A](msg)`, `thunk.LogEntryExit[A](name)`; never
  `log.Printf` or `slog.Info` inside `Map` / `Chain`.
- **Context with operators:** `AskValue`, `WithValue`, `WithTimeout`, `WithDeadline`;
  no `ctx.Value(k).(T)`, no discarded `cancel`.
- **No hidden mutation:** closures in `Map` / `Chain` / `Filter` and lens setters never
  change captured variables or shared slices.
- **IO is lazy:** run it with `()`; running a `thunk` gives one `Result[A]`, unwrap it
  with `result.Unwrap` only at the boundary.
- **Pipelines from functions, not package `var`s:** `func parsePorts() result.Kleisli[...]`.
  A `var` is fine for a lens or a single pre-bound helper.

## Rule 3 — monadic over imperative, wherever possible

Write new code, and every function you change, in monadic style. "Wherever possible"
means: whenever an fp-go type and combinator expresses the logic; the MCP server tells
you whether one exists. Imperative code is the exception and needs a reason.

| Imperative shape | Monadic replacement |
|---|---|
| `v, err := f(x); if err != nil { return …, err }` | `result.Eitherize1(f)`, then `result.Chain` / `thunk.Chain` for the next step |
| several `if err != nil` steps in a row | one `F.FlowN` / `F.PipeN` of `Chain` steps; the first `Left` short-circuits |
| `if x == nil { … }` / `if v, ok := m[k]; ok { … }` | `Option`: `option.FromNillable2`, `FromPredicate`, `GetOrElse`, `Fold` |
| `if !valid(x) { return errors.New(…) }` | `result.FromPredicate(valid, onFalse)` |
| `for` loop with an accumulator | `A.Map`, `A.Filter`, `A.FilterMap`, `A.Reduce` |
| `for` loop that calls a fallible or effectful step | `result.TraverseArray(f)`, `thunk.TraverseArray(f)` (`…Seq` / `…Par` to choose the order) |
| `switch` / `if`-cascade choosing a branch | a case list from `fp-go-pattern-matching` (`option.Alt`, `AltAllArray`, `FindFirstMap`) |
| hand-written `func(ctx) func() result.Result[A] { … }` | `thunk.Eitherize1`, `thunk.FromResult`, `thunk.Map`, `thunk.Chain` |
| `log.Printf` between two steps | `thunk.TapSLog[A](msg)` |
| `result.Unwrap` in the middle of a pipeline, then wrapping again | stay inside the monad: `Map` / `Chain`; unwrap once at the boundary |

```go
// AVOID: imperative - three exits, the error handling repeats, the loop accumulates
func parsePorts(raw []string) ([]int, error) {
	ports := make([]int, 0, len(raw))
	for _, s := range raw {
		p, err := strconv.Atoi(s)
		if err != nil {
			return nil, err
		}
		if p <= 0 || p >= 65536 {
			return nil, fmt.Errorf("port %d out of range", p)
		}
		ports = append(ports, p)
	}
	return ports, nil
}

// PREFER: monadic and point-free - the type says it can fail, Chain and
// TraverseArray handle the error path and the loop
func isValidPort() P.Predicate[int] {
	return F.Pipe1(N.MoreThan(0), P.And(N.LessThan(65536)))
}

func parsePort() result.Kleisli[string, int] {
	return F.Flow2(
		result.Eitherize1(strconv.Atoi),
		result.Chain(result.FromPredicate(isValidPort(), ER.OnSome[int]("port %d out of range"))),
	)
}

func parsePorts() result.Kleisli[[]string, []int] {
	return result.TraverseArray(parsePort())
}
```

### When imperative code is acceptable

Only in these places, and kept as small as possible:

- **The boundary:** `main`, the HTTP handler glue that runs a pipeline once
  (`pipeline(ctx)()`, `result.Unwrap`, `effect.RunSync`) and writes the response.
- **Leaves without a lift:** the one function that talks to a raw API with no fp-go
  wrapper, a goroutine that `select`s on `ctx.Done()`, `sync.Once`, a stream callback.
  Wrap the leaf once (`Eitherize`, `thunk.FromIO`, …) and compose on top of it.
- **Measured hot paths:** first try the `idiomatic/` packages; go imperative only with a
  benchmark that justifies it.
- **No combinator exists:** after checking the MCP server and `go doc`. Say why in a
  short comment.

Don't rewrite imperative code you don't otherwise touch (no drive-by refactors).

## Rule 4 — point-free wherever possible

A function is point-free when it is built by composition and never names the value it
works on. Aim for it at every level above the leaves.

- **Lambdas only at the leaves:** a field accessor (better: a generated lens `.Get` or
  prism `.ReverseGet`), a lens setter for `L.MakeLens`, a multi-field formatter, a raw
  I/O sink. Every `Map`, `Chain`, `Bind`, `Filter`, `Fold` argument above them is a named
  function, a combinator or an `F.FlowN` of those.
- **Combinators instead of small lambdas:** `N.MoreThan(18)`, `N.Mul(2)`, `S.IsEmpty`,
  `S.IsNonEmpty`, `P.Not`, `P.And`, `F.Constant1[E](v)`, `F.Bind2nd`, `LZ.Of(v)`.
- **Return the pipeline, don't take its argument:** `func fetchAll() thunk.Kleisli[[]int,
  []User] { return thunk.TraverseArray(fetchUser) }`, not `func fetchAll(ids []int) …`.
- **Configuration first, data last:** `func(Config) func(Data) B` so the inner function
  slots into `Flow`.
- **`F.Flow2(lens.Get, kleisli)`** feeds one field of the state into the next step.

```go
// AVOID: a lambda that hides a predicate and an error constructor
validate := thunk.ChainResultK(func(resp LookupResponse) result.Result[LookupResponse] {
	if S.IsEmpty(resp.ContextArgs) && S.IsEmpty(resp.Result) {
		return result.Left[LookupResponse](errHashNotFound)
	}
	return result.Of(resp)
})

// PREFER: a named predicate (a leaf), composed point-free
func isEmptyLookup(r LookupResponse) bool {
	return S.IsEmpty(r.ContextArgs) && S.IsEmpty(r.Result)
}

func validateLookup() thunk.Operator[LookupResponse, LookupResponse] {
	return thunk.ChainResultK(result.FromPredicate(
		P.Not(isEmptyLookup),
		F.Constant1[LookupResponse](errHashNotFound),
	))
}
```

Point-free is not a goal in itself: if the composed version needs more helper types or a
longer lambda than the plain one, keep the plain one and say why. That case is rare;
check the MCP server for a combinator first.

## Composing `Effect[C, A]`

`Effect[C, A]` (`github.com/IBM/fp-go/v2/effect`) is an alias of
`ReaderReaderIOResult[C, A]`: `func(C) func(context.Context) func() Result[A]`, i.e.
`Reader[C, Thunk[A]]`. Load the MCP `fp-go-effect` skill before you write one.

### Build effects with the `effect` package's own combinators

When a `Kleisli[C, In, Out]` combines several values taken from `C` (a service from the
dependencies, plus options or config also from the dependencies), use `effect`'s
combinators: `Asks`, `Map`, `Ap`, `Chain`, `FromThunk`, `ChainThunkK`, `Local`,
`FromReaderResult`, `TailRec`. Don't reach into `reader` / `context/readerioresult` to
rebuild `Effect`'s double-`Reader` shape by hand. That type-checks, but it depends on
`Effect`'s internal representation rather than its public API, so it is hard to read and
easy to get subtly wrong.

The pattern for "fetch dependency A, fetch dependency B, combine them, run the effectful
call":

| Step | Combinator |
|---|---|
| lift a pure accessor `Reader[C, X]` (a `Deps` getter) into `Effect[C, X]` | `effect.Asks` |
| apply a function that doesn't depend on `C` | `effect.Map` |
| apply a second value from `C`, independently of the first (applicative) | `effect.Ap` |
| flatten a resulting `Thunk` / `ReaderIOResult` into `Effect[C, _]` | `effect.Chain(effect.FromThunk[C, _])`, or `effect.ChainThunkK` |
| lift a `ReaderResult[C, A]` (pure, can fail, reads `C`) | `effect.FromReaderResult` |

[`openai/request.go`](../../../openai/request.go) `ChatCompletion` is the worked example;
[`openai/deepseek.go`](../../../openai/deepseek.go) `MakeDeepSeekChatCompletionDeps` uses
`Asks` + `Ap` to combine a client built from `HttpDeps` with an API key read through
`EnvironmentDeps`.

### `Asks` + `Map` to build, `Local` to reuse

- **Building** an `Effect[C, A]` from scratch out of a plain `Reader` accessor and a pure
  function: use `Asks` + `Map`, not `Local`. Point-free, `Asks` needs its type parameters
  spelled out (`effect.Asks[C, X]`), because Go can't infer a generic function's type
  parameters from the `Flow` slot it is plugged into.
- **Reusing** an existing `Effect[C2, A]` or `Kleisli` unchanged under a different
  environment `C1`: use `effect.Local[A](accessor)`, where `accessor` maps `C1 → C2`
  (usually an `AsXxxDeps[C1]` upcast or a `MakeXxxDeps` constructor). Only `A` has to be
  given; `C1` and `C2` are inferred from the accessor.

```go
// Reuse a Kleisli written for the narrow ToolDeps under the wider SessionDeps
handleToolCalls := F.Flow2(
	tools.HandleToolCalls(),
	effect.Local[Endomorphism[openai.ChatCompletionNewParams]](tools.AsToolDeps[SessionDeps]),
)
```

### Dependencies are interfaces with getters

- One minimal interface per concern, with getters: `XxxDeps { GetFoo() Foo }`, a private
  struct that implements it, `MakeXxxDeps(...) XxxDeps`, and the upcast
  `AsXxxDeps[R XxxDeps](r R) XxxDeps` for use with `Local`.
- Wider environments embed narrower ones (`SessionDeps` embeds `ChatCompletionDeps`,
  `ToolDeps` and `LoopDeps`). A function asks for the narrowest interface it needs.
- Every OS / SDK side effect sits behind a getter, wrapped once
  (`ioresult.Eitherize1`, `result.TryCatchError`, `thunk.FromIOResult`), so tests can
  replace it with a fake.
- Configuration that the loop reads (limits, options) is a getter too, not a parameter
  threaded through by hand: see `LoopDeps.GetMaxIterations`. Read it inside the pipeline
  with `reader.Sequence` / `effect.FromReaderResult`.

### Loops: `effect.TailRec`, never recursion

A loop of effectful steps is a step function `Kleisli[C, S, Trampoline[S, B]]` that returns
`tailrec.Bounce(next)` to continue or `tailrec.Land(result)` to stop, run by
`effect.TailRec` (fp-go v2.4.0 or later). It is stack-safe and checks the
`context.Context` before each step. Never write `Chain` recursion inside `Effect`.

```go
// session.Run: guard the iteration limit, then run one step, until it lands
return F.Pipe1(
	F.Flow2(guard, effect.Chain(Next())),
	effect.TailRec,
)
```

### Errors: data for the model, `Result` for infrastructure

- An expected failure that the caller (or the model) should see and react to is a
  **value**: a failed tool call becomes a tool message (`tools.MakeToolCall` never fails).
- Only infrastructure failures (network, auth, cancellation, a broken invariant such as
  the iteration limit) use the `Result` error channel. Give them a typed error
  (`*MaxIterationsError`) so callers can match it with `errors.As`.

### Running an effect: only at the boundary

Supply the dependencies and run once, in `main` or the handler glue:

```go
final, err := effect.RunSync(F.Pipe1(
	program,
	effect.Provide[Result](deps),
))(ctx)
```

`Provide` needs the result type given explicitly (`Provide[A]`). Inside the code,
dependencies that are themselves built by an effect (an `Effect[D1, D2]`) are threaded
through with `effect.LocalEffectK[A](F.Constant1[D1](buildDeps))`, not run early:

```go
// main.answer: reuse session.Run under DeepSeekDeps
F.Flow5(
	makeRequest(registry),
	session.MakeSession,
	session.Run(),
	effect.Local[session.FinalResult](toSessionDeps),
	effect.LocalEffectK[session.FinalResult](F.Constant1[oai.DeepSeekDeps](oai.MakeDeepSeekChatCompletionDeps())),
)
```

## Aliases in this repository

The MCP server's skills use their own canonical aliases. This repository has established
ones; follow the file you edit and, in new files, the repository's:

| Package | This repository | MCP skills |
|---|---|---|
| `context/readerioresult` | `thunk` | `RIO` |
| `result` | unaliased (`result`) | `R` |
| `effect` | unaliased (`effect`) | `EF` |
| `http/headers` | `HH` | `HD` |
| `http/content` | `HC` (`C` is `urfave/cli`) | `C` |
| `option`, `predicate`, `errors` | mostly unaliased; `O`, `P`, `ER` in some files | `O`, `P`, `ER` |
| `function`, `array`, `number`, `string`, `lazy` | `F`, `A`, `N`, `S`, unaliased `lazy` | `F`, `A`, `N`, `S`, `LZ` |

Translate the MCP examples to these aliases when you copy a pattern.

## If the server's tools are missing

The server runs as a `go tool` of this module (`github.com/IBM/fp-go/gen/v2`, listed in the
`tool` block of `go.mod`). Check the MCP configuration of the client you are running in:

- Bob: [`.bob/mcp.json`](../../mcp.json)
- Claude Code: [`.mcp.json`](../../../.mcp.json) in the repository root

If the `fp-go` entry is missing, add it (merge into `mcpServers`, keep the other servers):

```json
{
  "mcpServers": {
    "fp-go": {
      "type": "stdio",
      "command": "go",
      "args": ["tool", "gen", "mcp", "--verbose"],
      "cwd": "${workspaceFolder}",
      "alwaysAllow": ["get_example", "search_examples", "use_skill", "list_skills"],
      "disabled": false
    }
  }
}
```

`cwd`, `alwaysAllow` and `disabled` are Bob settings; for `.mcp.json` leave them out. If
the entry exists but has `"disabled": true`, set it to `false`.

Then do the warm-up below and ask the user to **restart the session** (or reload the
MCP servers) so the client picks up the server. Don't continue with fp-go code until the
tools are available. Until then, `go doc github.com/IBM/fp-go/v2/<pkg> <Symbol>` is the
fallback for signatures; it reads the same pinned version.

### Warm up the server when it isn't running

The first `go tool gen …` call compiles the generator, which can take longer than the
client waits for the server to start. If the server is configured but not running (its
tools are missing or it failed to start), build it once from the repository root:

```
go tool gen mcp --help
```

This compiles the tool into the Go build cache and exits. The next start is hot: reload the
MCP servers or restart the session and the tools come up quickly.

## Checklist for every change

- [ ] MCP skills loaded (`fp-go` + topics); every fp-go call checked with `get_example` /
      `search_examples` / `go doc`; nothing recalled from memory
- [ ] fp-go best practices (Rule 2): v2, data-last, simplest monad, `Result`, lifted with
      `Eitherize`, lenses, `Tap*` logging, no hidden mutation
- [ ] Monadic (Rule 3): no `if err != nil` chains, `nil` checks or accumulating loops
      where a combinator exists; imperative code only at the boundary or a leaf, with a reason
- [ ] Point-free (Rule 4): lambdas only at the leaves; combinators, lens getters and
      `F.FlowN` above them; pipelines returned from functions
- [ ] Effects built with `effect`'s combinators (`Asks`/`Map`/`Ap`/`Chain`/`FromThunk`),
      reused under another environment with `Local`; dependencies as getter interfaces;
      loops with `effect.TailRec`; run only at the boundary with `Provide` + `RunSync`
- [ ] Repository aliases and conventions from [`go-conventions`](../go-conventions/SKILL.md)
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` clean
