---
name: fp-go
description: Use this skill whenever writing, generating, changing, refactoring or reviewing Go code in this repository, and always when a Go file imports github.com/IBM/fp-go/v2. It sets the five rules every code change follows - fp-go best practices, monadic over imperative style, point-free style, every function nullary (parameters moved into the returned Reader/Kleisli, inner functions hoisted), and the fp-go MCP server as the primary source of truth - with the workflow and the exceptions, readers instead of closures over a pipeline's input (reader.Ap/Chain/TraverseArray), the combinator that matches the operation (ApS without a setter is Ap, Map+Flatten is Chain, Fold over Left/Right is FromPredicate, empty fallback is Alt), the rules for composing Effect[C, A] (effect combinators Asks/Map/Ap/Chain/FromThunk, Local for reuse, Deps getter interfaces, effect.TailRec loops, Provide/RunSync at the boundary), and explains how to set up and start that MCP server.
---
# fp-go — the rules for every code change, and the MCP server behind them

This repository is written in functional Go on top of `github.com/IBM/fp-go/v2`. Every
code change, generated or reviewed, small or large, follows the five rules below. They
are not style preferences: a change that breaks one of them is not done, and a review
reports it as a violation of a documented standard.

| # | Rule | In one line |
|---|---|---|
| 1 | **The fp-go MCP server is the primary source of truth** | Look up every fp-go signature, pattern and best practice there before writing or judging code. Never rely on memory. |
| 2 | **Follow fp-go best practices** | v2 imports, data-last, the simplest monad that fits, `Result` over `Either[error, A]`, lifting with `Eitherize`, lenses, `Tap*` for logging, no hidden mutation. |
| 3 | **Monadic over imperative, wherever possible** | Model failure, absence, effects and iteration with `Result`, `Option`, `thunk` / `Effect`, `Chain`, `Traverse`, not with `if err != nil` chains, `nil` checks and `for` loops. |
| 4 | **Point-free wherever possible** | Compose named functions, lens getters and combinators with `F.Flow` / `F.Pipe`. Lambdas only at the leaves. |
| 5 | **Every function is nullary** | `func f() Reader[A, B]`, not `func f(a A) B`: parameters move into the returned `Reader` / `Kleisli`, inner steps are hoisted into named nullary functions. Only leaves, wiring and tests take parameters. |

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
2. Judge the diff against the five rules.
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
  I/O sink. A hand-written accessor is a review finding whenever the lens generator can
  produce the lens, third-party types included (`--type`); keep one only where the
  generator can't (function-typed fields), with a comment. See
  [`go-conventions`, *Generated lenses*](../go-conventions/SKILL.md#generated-lenses-instead-of-hand-written-field-accessors). Every `Map`, `Chain`, `Bind`, `Filter`, `Fold` argument above them is a named
  function, a combinator or an `F.FlowN` of those.
- **Combinators instead of small lambdas:** `N.MoreThan(18)`, `N.Mul(2)`, `S.IsEmpty`,
  `S.IsNonEmpty`, `P.Not`, `P.And`, `F.Constant1[E](v)`, `F.Bind2nd`, `LZ.Of(v)`.
- **Return the pipeline, don't take its argument:** `func fetchAll() thunk.Kleisli[[]int,
  []User] { return thunk.TraverseArray(fetchUser) }`, not `func fetchAll(ids []int) …`.
- **Configuration first, data last:** `func(Config) func(Data) B` so the inner function
  slots into `Flow`.
- **`F.Flow2(lens.Get, kleisli)`** feeds one field of the state into the next step.
- **Lenses in local variables:** bind every lens a function uses to a local variable at
  the top of the function (`textLens := MakeInsertContentArgsTextLens()`) and use
  `textLens.Get` / `textLens.Set` in the pipeline. Never inline `MakeXxxLens().Get`, even
  when the lens is used only once.

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

## Readers instead of closures over the input

A pipeline that needs its input in more than one place is usually written as a closure
over the argument. That closure is not a leaf: `reader.Reader[R, A]` **is** `func(R) A`
(an alias, `reader/types.go`), so the value the closure captures is already the
environment of a reader. Build the pipeline with the reader combinators instead and it
stays point-free, with the same signature.

| Shape inside the closure | Reader combinator |
|---|---|
| two steps read the same input, independently | `reader.Ap[B](fa)` applied to a `Reader[R, func(A) B]` |
| a later step reads the input again | `reader.Chain(k)` with `k: A -> Reader[R, B]` |
| every element of a slice becomes a value that reads the input | `reader.TraverseArray(k)` with `k: Kleisli[R, A, B]` |
| the environment is already named | `reader.Read[A](r)` — a sign the pipeline should have been a reader |

Worked example: [replay/task.go](../../../replay/task.go) `chunkAt` splits a slice into
one chunk per element that matches a predicate. The boundaries (the matching indices,
terminated by the size) and the slicing both read the same slice.

```go
// AVOID: a closure, because `as` is read three times
func chunkAt[T any](pred Predicate[T]) func([]T) [][]T {
	starts := A.FilterMapWithIndex(indexWhen(pred))
	ranges := consecutiveRanges()

	return func(as []T) [][]T {
		return F.Pipe4(
			as,
			starts,
			F.Bind2nd(A.Append[int], A.Size(as)),
			ranges,
			A.Map(F.Flow2(pair.Paired(A.Slice[T]), reader.Read[[]T](as))),
		)
	}
}

// PREFER: the slice is the environment of a reader
func chunkBounds[T any](pred Predicate[T]) reader.Reader[[]T, []int] {
	return F.Pipe1(
		F.Flow2(A.Size[T], A.Push[int]),                         // Reader[[]T, Operator[int, int]]
		reader.Ap[[]int](A.FilterMapWithIndex(indexWhen(pred))),
	)
}

func chunkAt[T any](pred Predicate[T]) reader.Reader[[]T, [][]T] {
	return F.Pipe1(
		chunkBounds(pred),
		reader.Chain(F.Flow2(
			consecutiveRanges(),
			reader.TraverseArray(pair.Paired(A.Slice[T])),        // Kleisli[[]T, Pair[int, int], []T]
		)),
	)
}
```

What makes it work:

- **An `Operator[A, B]` is a `Reader[[]A, []B]`.** `pair.Paired(A.Slice[T])` is therefore
  already a `Kleisli[[]T, Pair[int, int], []T]`, and `reader.TraverseArray` hands it the
  environment, so the `A.Map(F.Flow2(..., reader.Read(as)))` step disappears.
- **`A.Push(a)` is the curried `A.Append(as, a)`.** Prefer it over
  `F.Bind2nd(A.Append[A], a)`; look for the curried sibling before reaching for
  `F.Bind1st` / `F.Bind2nd`.
- **`reader.Reader[R, A]` is an alias**, so widening a return type from `func([]T) [][]T`
  to `reader.Reader[[]T, [][]T]` changes no call site: `F.FlowN` and the `*.TraverseArray`
  combinators keep inferring.
- `consecutiveRanges` in the same file is the degenerate case of the applicative shape
  (`reader.Ap` with `F.Identity` as the second reader); `chunkBounds` is the same shape
  with a real one.
- **`reader.Map(k)` followed by `reader.Ap(reader.Ask[R]())` is `reader.Chain(k)`.** When
  a step produces a curried `func(A) func(R) B` and its result is fed back to the same
  environment, that `func(A) func(R) B` already is a `reader.Kleisli[R, A, B]`: chain it
  instead of mapping and re-applying `Ask`. Spotted by an explicit `reader.Ask` in an
  `Ap`, or by `reader.Map[R]` / `reader.Ap[B]` needing type arguments that `Chain` infers.
  [tools/grep.go](../../../tools/grep.go) `ancestorIgnore` is the example: the directory
  is read from the segments, and the segments are read again as the `Domain`:

  ```go
  // AVOID: map to func([]string) IgnoreFile, then apply it to the same segments
  reader.Map[[]string](F.Flow2(file.Join(gitignoreFile), F.Curry2(makeIgnoreFile))),
  reader.Ap[IgnoreFile](reader.Ask[[]string]()),

  // PREFER: the curried constructor is a Kleisli[[]string, string, IgnoreFile]
  reader.Chain(F.Flow2(file.Join(gitignoreFile), F.Curry2(makeIgnoreFile))),
  ```
- **The converse: `reader.Chain(reader.Local(fa))` is `reader.Ap(fa)`.** Ask whether the
  later read *needs the earlier result*. If it doesn't, the reads are independent and
  `Ap` is both the right concept and the shortest form. "Monadic" is not automatically
  simpler: `Chain(Local(fa))(fab)` reduces to `r ↦ fab(r)(fa(r))`, which is the
  definition of `Ap(fa)`. [tools/insertcontent.go](../../../tools/insertcontent.go)
  `writeInsertedWith` is the example. The path picks the writer and the bytes are what it
  writes; neither read depends on the other:

  ```go
  // AVOID: Ap spelled out by hand
  reader.Local[ioresult.Kleisli[[]byte, []byte]](pathLens.Get),
  reader.Chain(reader.Local[ioresult.IOResult[[]byte]](F.Flow2(fileText(), S.ToBytes))),

  // PREFER: two independent reads
  reader.Local[ioresult.Kleisli[[]byte, []byte]](pathLens.Get),
  reader.Ap[ioresult.IOResult[[]byte]](F.Flow2(fileText(), S.ToBytes)),
  ```

  `Chain` is right when a value is computed first and the later step needs it, as in
  `insertedText`, where the index is computed from the call and then used to insert.
- **`reader.Map(g)` + `reader.Ap(fb)` is `liftA2(g)(fa, fb)`, not a hidden `Chain`.** Two
  readers of the same environment combined by a binary function are applicative. `reader`
  has no `LiftA2` (fp-go v2, checked with `go doc`), and `SequenceT2` + `Map(Tupled…)` is
  longer, so `F.Pipe2(fa, reader.Map[R](F.Curry2(g)), reader.Ap[C](fb))` is the form.
  [tools/truncate.go](../../../tools/truncate.go) `truncateError` is the example: the
  shortened error and the length test are both read from the limit, then `P.Fold`ed.
  When `g` is composition of two endomorphisms, use the monoid instead (see
  *The combinator that matches the operation*).

Watch the empty case: `reader.TraverseArray` over an empty slice yields `nil` where
`A.Map` yielded an empty slice, so tests comparing with `assert.Equal` expect `nil`.

## Rule 5 — nullary functions, with the inner functions hoisted

**Implement every function as a nullary function** that returns its pipeline: a
`Reader`, a `Kleisli`, an `Operator`, a `Predicate` or an `Endomorphism`. This applies to
new code and to every function a change touches. Parameters don't go away, they move
into the returned value's type:

- **The data a function works on** becomes the environment of the returned reader:
  `insertedText(a Args) string` → `insertedText() Reader[Args, string]`.
- **A configuration parameter** becomes the outer level of a curried reader:
  `insertAt(index int) func(string) …` → `insertAt() Reader[int, Reader[string, …]]`.
  Rule 4's "configuration first, data last" is this order.
- **A function-typed parameter** (a writer, a callback) is a reader environment too:
  `writeInsertedWith(write …)` → `writeInsertedWith() Reader[func(string) …, …]`.

Call sites change from `f` to `f()`, tests included. A function that needs the same
value twice is rewritten with the combinators below, not left parameterized.

The only exceptions, each named as such in its doc comment:

- **Leaves** in the sense of Rule 4: a multi-field formatter (`insertedAtLine(a)`), a
  named predicate over several fields, a constructor that `F.CurryN` lifts
  (`completeTurn`), a raw I/O sink.
- **Wiring:** dependency constructors and casts (`MakeInsertContentDeps`,
  `AsInsertContentDeps`), interface methods, and getters such as
  `insertContentDefinition(root)` that build a plain value for a tool definition.
- **A generic I/O leaf** whose type parameter is the dependency set, such as
  `readBytes[R](path)`, because Go can't infer `R` through a nullary call.
- **Tests and `main`.**

How to get there:

1. **A value derived from the input and used more than once is a `reader.Chain`.**
   Compute it with its own reader and chain it into a hoisted `Kleisli` that receives it.
2. **Hoist each inner step** into its own named, nullary, package-level function with a
   doc comment, instead of nesting lambdas or local variables in one body. Hoisted names
   share the package namespace: `go build` the package before you pick a name such as
   `truncatedError`, which `writefile.go` already uses.
3. **Then make the hoisted `Kleisli` nullary too.** Its parameter disappears with the
   combinators below.

Worked example: [tools/insertcontent.go](../../../tools/insertcontent.go) `insertedText`.
The index is computed from the call and used as both ends of `replaceLines`. The
inserted lines and the file's lines are read from the same call:

```go
// AVOID: a leaf that reads the call three times and the index twice
func insertedText(a InsertContentArgs) string {
	index := insertIndex()(a)
	return replaceLines(index, index, F.Constant1[string](insertedLines()(a)))(splitAfterLines()(terminated()(a.Text)))
}

// PREFER: monadic over the index, every step hoisted and nullary
func insertAt() Reader[int, Reader[string, Reader[[]string, string]]] {
	return F.Pipe2(
		F.Curry3(replaceLines),
		reader.Flatten,                                    // index ↦ replaceLines(index, index)
		reader.Map[int](reader.Local[Reader[[]string, string]](F.Constant1[string, string])),
	)
}

func insertedAt() reader.Kleisli[InsertContentArgs, int, string] {
	return F.Flow3(
		insertAt(),
		reader.Local[Reader[[]string, string]](insertedLines()),
		reader.Ap[string](fileLines()),
	)
}

func insertedText() Reader[InsertContentArgs, string] {
	return F.Pipe1(insertIndex(), reader.Chain(insertedAt()))
}
```

How a parameter disappears:

| Parameterized shape | Nullary form |
|---|---|
| `func h(x X) …` uses `x` twice: `g(x, x)` | `F.Pipe1(F.Curry2(g), reader.Flatten)`; with a third argument `F.Curry3(g)` + `reader.Flatten`, giving `Reader[X, func(C) D]` |
| `func h(k func(A) B) …` returns `F.Flow2(getter, k)` | `reader.Local[B](getter)`: `k ↦ k ∘ getter` |
| `func h(x X) …` returns `F.Flow2(f, k(x))` | `F.Flow2(k, reader.Local[…](f))` |
| `func h(x X) …` returns `F.Pipe1(r(x), reader.Ap[B](fa))` | `F.Flow2(r, reader.Ap[B](fa))` |
| `func h(x X) …` returns `F.Flow2(F.Constant1[A], g(x))` | `reader.Map[X](reader.Local[…](F.Constant1[A, A]))` after the reader that yields `g(x)` |
| `func h(n N, s S) …` returns `leaf(g(n, s), s)`: `s` read again by a two-argument leaf | `F.Flow2(gReader, reader.Chain(F.Curry2(leaf)))` with `gReader: Reader[N, Reader[S, A]]`; `F.Curry2(leaf)` is already a `reader.Kleisli[S, A, B]` |
| `func h(n N) …` returns `F.Ternary(p(n), f(n), F.Identity)` / `if p(s) { return f(s) }; return s` | `F.Pipe2(fReader, reader.Map[N](orKeep[T]()), reader.Ap[Endomorphism[T]](pReader))` with `orKeep[T]() = F.Curry2(P.Fold[T, T])(F.Identity[T])`. `F.Ternary` is deprecated in favour of `P.Fold` |
| `func h(f Endomorphism[B]) Endomorphism[S]` returns `t ↦ Make(get1(t), f(get2(t)))` | a lens `Modify` in disguise: `fieldLens.Modify`. Without a generated lens, build one with `L.MakeLens` (see [`go-conventions`](../go-conventions/SKILL.md#generator-rules-and-pitfalls-go-tool-gen-lens-fp-go-v240), `tools.MakeToolCallLens`, used in `TruncateTools`) |
| `func h(n N) Endomorphism[T]` returns `F.Flow2(f(n), g(n))`: two endomorphisms of the same `n`, composed | `reader.ApplicativeMonoid[N](endomorphism.Monoid[T]()).Concat(g, f)`. `Concat(g, f)` applies `f` first (right to left); when the order matters, say so in a comment |

`F.Curry2(g)` + `reader.Flatten` is the duplicating ("W") combinator: `Flatten` of a
`Reader[X, Reader[X, A]]` passes the same `x` to both levels. It is not the `Map` +
`Flatten` pair from *The combinator that matches the operation*. There is no `Map`
before it, so there is no `Chain` to replace it with.

**Type pitfall:** `reader.Kleisli[R, A, B]` is `func(A) Reader[R, B]`, so the *argument*
comes second. A curried `func(int) func(string) X` is `Reader[int, Reader[string, X]]`,
**not** `reader.Kleisli[int, string, X]`, which is `func(string) func(int) X`. The
compiler error reads as if `Reader` and `Kleisli` were distinct defined types, but both
are aliases: the argument order is what doesn't match. Spell nested curried functions as
nested `Reader`s.

## Several readers over one environment

When one value has several fields and each is read from the *same* environment, the
reflex is an accumulator struct — `// fp-go:Lens`, `go generate`, `Do` / `ApS`. For
independent fields that is more machinery than the job needs. Curry the constructor and
apply one reader per field with the applicative; no struct, no lenses, no intermediate
state.

Worked example: [replay/task.go](../../../replay/task.go) `turnOf` builds a `Turn` out of
three readers over the same chunk of records — the request, the last response and the tool
invocations.

```go
// The partial applications of the curried constructor, in reverse order of
// application. They must be ALIASES (`=`): a defined type is not assignable
// from the function `F.Curry3` returns.
type (
	turnFromTools    = func([]ToolInvocation) Option[Turn]
	turnFromResponse = func(Option[ChatResponse]) turnFromTools
)

func turnOf() ReaderResult[[]Record, Option[Turn]] {
	return F.Pipe3(
		readerresult.Of[[]Record](F.Curry3(completeTurn)),
		readerresult.Ap[turnFromResponse](requestOf()),   // peels the 1st argument
		readerresult.Ap[turnFromTools](responseOf()),     // peels the 2nd
		readerresult.Ap[Option[Turn]](toolsOf()),         // peels the 3rd
	)
}
```

What makes it work:

- **Each `Ap` peels one argument**, and its explicit type argument is what is *left* of the
  curried function. `Ap[B, R, A]` cannot infer `B`, so name those leftovers as aliases;
  spelled out they are unreadable and they change whenever an argument is added.
- **A field that cannot fail still goes through `Ap`**, lifted into the monad at its own
  leaf (`toolsOf` ends in `result.Of`). Don't special-case it.
- **The constructor is the only leaf.** `completeTurn` takes the three decoded parts and
  returns `Option[Turn]`, so "a chunk with no response is not a turn" is expressed once,
  in a total function, instead of as a branch in the pipeline.

Use `Bind` with do-notation and generated lenses **instead** when a later field is computed
*from* an earlier one — that is what the accumulator struct is for. Independent fields are
applicative; dependent fields are monadic.

## The combinator that matches the operation

A pipeline can compute the right value with the wrong concept: `ApS` without a struct,
`Map` followed by `Flatten`, a `Fold` that picks `Left` or `Right`. It compiles and the
tests pass, but the reader has to work out what is really happening, and the next
change builds on the wrong idea. Name each step for what it does. Every row below was
found in this repository and replaced; the rewrites changed no behaviour.

| Shape in the code | What it really is | Use instead |
|---|---|---|
| `reader.ApS(setter, fa)` / `readeroption.ApS(…)` where the "setter" is not a field setter (`A.Prepend`, `A.Push`, `F.Flip(lens.Modify)` composed with `effect.Map`) | applicative application | `F.Pipe1(F.Flow2(fa', setter), reader.Ap[B](fb))`. `ApS` / `Bind` are do-notation and belong with a struct and its lens setters. |
| `reader.Of(x)` + `reader.ApS(A.Push, fa)`, with only one value read from the environment | `Map` over that one reader | plain composition: `F.FlowN(getter, …, F.Bind1st(A.Append[T], A.Of(x)))` |
| `identity.Bind(pair.MapHead, f)` | the pair is read twice: once to compute the update, once to apply it | `F.Pipe1(f, reader.Chain(pair.MapHead[B, A, A]))` |
| `reader.Map(f)` + `reader.Chain(reader.Sequence(g))` | two independent reads of one input | `F.Pipe1(g, reader.Ap[B](f))` |
| `F.Flow2(…, k)` + `reader.Flatten` | `Map` then `Flatten` | `reader.Chain(k)` |
| `reader.Chain(reader.Local[B](fa))` | two independent reads, with `Ap` spelled out by hand | `reader.Ap[B](fa)` |
| `readerresult.Ap(…)` + `readerresult.ChainResultK(F.Identity[Result[A]])` | the applied function can fail, so it is a bind | `readerresult.Chain(k)`; `reader.Local[Result[B]](getter)` turns a `result.Kleisli[X, B]` into the `ReaderResult` that `Chain` needs |
| `F.Flow2(lens.Get, result.Of[T])` as an argument of `readerresult.Ap` | lifting a reader that cannot fail | `readerresult.Asks(lens.Get)` (also `readeroption.Asks`, `effect.Asks`) |
| `P.Fold(F.Constant1(fail), F.Constant1(result.Of))(pred)` + `reader.Ap(text)` | validation | `F.Flow2(result.FromPredicate(pred, F.Flow2(text, errors.New)), result.Map(text))` |
| `effect.Alt(empty)` + `Filter` + `Chain(P.Fold(…)(A.IsNonEmpty))` | "try this, otherwise that" | an effect that fails when the first case doesn't apply (`A.FindFirst` + `result.FromOption`), then `effect.Alt(lazy.Of(second))` |
| `reader.Local(f)(g)` over two known functions | composition | `F.Flow2(f, g)` |
| `reader.Map[N](reader.Compose[…])` + `reader.Ap(second)` where both stages are `Reader[N, Endomorphism[T]]` | composing endomorphisms, pointwise over the reader: a monoid | `reader.ApplicativeMonoid[N](endomorphism.Monoid[T]()).Concat(second, first)` (right to left) |
| `ChainLeft` / `OrElse` / `MapLeft` of a `P.Fold` over a hand-written `errors.Is` predicate | recovering from, or renaming, one kind of error | `CatchIs(target, f)` of the monad at hand (`effect`, `thunk` since fp-go v2.8.0; `result`, `ioresult`, `readerresult`, `readerioresult` since v2.9.0); by type `CatchAs`, `MapErrorAs`, `TapErrorAs` |

Worked examples:

- [session/session.go](../../../session/session.go) `Next`: request and session paired
  with `reader.Ap`; usage, iteration count and history updated with
  `reader.Chain(pair.MapHead…)`; tool messages applied with `reader.Ap` and
  `F.Flip(currentLens.Modify)`; the tool-call branch is a plain `F.Flow3`.
- [tools/tools.go](../../../tools/tools.go) `handleToolCallsForChoice`:
  `readeroption.Asks(prependMessage)` + `readeroption.Ap` instead of `readeroption.ApS`.
- [tools/readfile.go](../../../tools/readfile.go) `renderFile`: `readerresult.Chain`
  with `reader.Local`.
- [tools/executecommand.go](../../../tools/executecommand.go) `renderOutput`:
  `result.FromPredicate` + `result.Map`.
- [tools/grep.go](../../../tools/grep.go) `searchScope`: `fileScope` with `effect.Alt`
  falling back to `dirScope`.
- [replay/task.go](../../../replay/task.go) `GroupTasks`: `reader.Chain` instead of
  `reader.Flatten`.
- [tools/truncate.go](../../../tools/truncate.go) `truncateThunk`: `thunk.Map` and
  `thunk.MapLeft`, both read from the limit, composed with `reader.ApplicativeMonoid`
  over `endomorphism.Monoid` instead of `reader.Compose` + `reader.Ap`. Also
  [tools/grep.go](../../../tools/grep.go) `compilePattern` (order matters, so it is
  commented) and [tools/executecommand.go](../../../tools/executecommand.go).
- [tools/truncate.go](../../../tools/truncate.go) `TruncateTools`: rebuilding a `Tool`
  around a changed call was `MakeToolCallLens().Modify`.
- [openai/deepseek.go](../../../openai/deepseek.go) `MakeDeepSeekChatCompletionDeps`:
  the client options are a plain `F.Flow6`.
- [prompt/projectrules.go](../../../prompt/projectrules.go) `readOptional`: a missing file
  is `None` through `effect.CatchIs(fs.ErrNotExist, …)`;
  [tools/executecommand.go](../../../tools/executecommand.go) `timeoutError` renames the
  deadline error with `thunk.CatchIs`; [env/env.go](../../../env/env.go) `ReadDotEnv` gives
  a missing file no variables with `ioresult.CatchIs`.

How to spot them:

- **`ApS` / `Bind` without a lens setter or a constructor as the first argument.** If
  the "state" is an `Effect`, a slice or a function rather than a struct, it is `Ap` or
  `Chain`.
- **`Sequence`, `Flatten` or `ChainXxxK(F.Identity)` right after a `Map`.** The pair is
  `Chain` or `Ap`.
- **`F.Constant1(result.Of)` / `F.Constant1(result.Left)` inside a `Fold`.** That is
  `FromPredicate`, `FromOption` or `FromError`.
- **A fallback to an empty value followed by a test for emptiness.** That is `Alt`.

Not a finding:

- `effect.Asks(getter)` + `effect.ChainIOResultK(reader.Read(x))` lifts a getter that
  returns a `ReaderIOResult` (`walkDir`, `readBytes`, `env.LookupEnvThunk`). `effect`
  has no `FromReaderIOResult` (fp-go v2.6.0), so this is already the right form.
  `effect.ChainThunkK(F.Flow2(…, thunk.FromIOResult))` *is* a finding since v2.6.0:
  it is `effect.ChainIOResultK`.
- A function over its input that reads the input three or more times, where the reader
  form needs a helper per read (`grep.go` `fileScope`). Keep it, and say why in its doc
  comment (Rule 4).

When you change a function's shape (for example `searchScope(p)` becomes
`searchScope()` returning a `Kleisli`), update its tests too, and run `go test` on the
package before you move on.

## Composing `Effect[C, A]`

`Effect[C, A]` (`github.com/IBM/fp-go/v2/effect`) is an alias of
`ReaderReaderIOResult[C, A]`: `func(C) func(context.Context) func() Result[A]`, i.e.
`Reader[C, Thunk[A]]`. Load the MCP `fp-go-effect` skill before you write one.

### Build effects with the `effect` package's own combinators

When a `Kleisli[C, In, Out]` combines several values taken from `C` (a service from the
dependencies, plus options or config also from the dependencies), use `effect`'s
combinators: `Asks`, `Map`, `Ap`, `Chain`, `FromThunk`, `ChainThunkK`,
`ChainIOResultK`, `Local`, `FromReaderResult`, `TailRec`. Don't reach into `reader` / `context/readerioresult` to
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
| flatten a resulting `IOResult` into `Effect[C, _]` | `effect.ChainIOResultK` (fp-go ≥ v2.6.0), not `ChainThunkK` + `thunk.FromIOResult` |
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

### Everything is read-only: `Ap` runs concurrently

`Ap` of the IO-based monads is **parallel by default**. `io.MonadAp` dispatches to
`io.MonadApPar`, which runs the argument on a second goroutine (`go func() { c <- ma() }()`)
while the calling goroutine runs the function side. `ioresult`, `ioeither`,
`context/readerioresult` and `effect` all go through it, and so does every combinator
built on `Ap`:

| Runs its parts concurrently | Runs its parts in order |
|---|---|
| `Ap`, `ApS`, `ApplicativeMonoid(…).Concat`, `TraverseArray`, and every other `Traverse*` / `Sequence*` built on `Ap` | `Chain`, `Bind`, `Tap*`, `TailRec`, `ApSeq` and the other `…Seq` variants |

So two parts of one effect can run at the same time, and **everything they touch is
read-only**:

- **Dependencies.** A `Deps` struct is built once by `Make…Deps` and never changed. Its
  getters return the same value or function every time. Nothing behind a getter keeps
  state: no cache, counter, `append` to a captured slice or map write. The OS-backed
  dependencies (`file.ReadFile`, `WalkFS`, HTTP clients) are safe to call concurrently;
  keep it that way for every new one.
- **Captured values and closures.** A `Kleisli`, `Reader` or leaf never writes a
  variable it closes over.
- **Values in the pipeline.** Inputs are never changed in place; build new values
  (`A.Map`, `A.Filter`, a lens setter that assigns a freshly built slice).
- **Test fakes.** See *Mocking Effect and Thunk* in
  [`go-conventions`](../go-conventions/SKILL.md#fakes-are-read-only).

The order of the *results* stays fixed: `Concat(a, b)` gives `a`'s result before `b`'s,
and `TraverseArray` keeps the order of its input. Only the order of the *side effects*
is not fixed.

A data race under `Ap` is always a bug in shared mutable state. Make the state
read-only; never switch to `ApSeq` or `Chain` to hide the race. CI runs `go test -race`;
it can't run locally on Windows without cgo, so check these rules before you push.

```go
// BAD: the fake appends to a captured slice; Concat reads AGENTS.md and the
// rules concurrently, and the race detector fails the test
var read []string
tools.MakeFileDeps(func(path string) ioresult.IOResult[[]byte] {
	return func() result.Result[[]byte] {
		read = append(read, path)
		return result.Of([]byte("text"))
	}
})

// GOOD: a read-only fake, and the test asserts on the effect's output
tools.MakeFileDeps(func(path string) ioresult.IOResult[[]byte] {
	return ioresult.Of([]byte("text of " + filepath.Base(path)))
})
```

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

- [`.bob/mcp.json`](../../mcp.json)
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

`cwd`, `alwaysAllow` and `disabled` are settings of the client that reads `.bob/mcp.json`; for `.mcp.json` leave them out. If
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
- [ ] Nullary (Rule 5): every function except leaves, wiring and tests is `func f() …`,
      with its parameters in the returned `Reader` / `Kleisli` and its inner steps hoisted
      into named functions; a value used twice goes through `reader.Chain` or
      `reader.Flatten`; every lens is bound to a local variable
- [ ] Each step uses the combinator that matches the operation: no `ApS`/`Bind`
      without a lens setter, no `Map`+`Flatten`, no `Chain(Local(fa))` for an
      independent read (that is `Ap`), no `Fold` choosing `Left`/`Right`, no
      "empty fallback, then test for empty" (see *The combinator that matches the operation*)
- [ ] Effects built with `effect`'s combinators (`Asks`/`Map`/`Ap`/`Chain`/`FromThunk`),
      reused under another environment with `Local`; dependencies as getter interfaces;
      loops with `effect.TailRec`; run only at the boundary with `Provide` + `RunSync`
- [ ] Everything is read-only (`Ap`, `Concat`, `TraverseArray` run concurrently):
      dependencies, getters, captured variables, pipeline values and test fakes keep no
      mutable state; a recording fake uses a `sync.Mutex` and `assert.ElementsMatch`
- [ ] Repository aliases and conventions from [`go-conventions`](../go-conventions/SKILL.md)
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` clean
