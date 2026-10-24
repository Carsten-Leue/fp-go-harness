# AGENTS.md

## README

[README.md](README.md) is the overview of what this repository does: what works today, the
packages and the roadmap status. Keep it up to date in the same PR as the change. Update it
when a PR adds or removes a package, a tool or a CLI capability, wires something into
[main.go](main.go), finishes a [PLAN.md](PLAN.md) step, or changes how to build, run or test.
Tick the step's checkbox in PLAN.md in the same PR.

## Pull requests and issues

PR and issue titles start with an emoji that matches the change, followed by the
conventional-commit title: `✨ feat: add grep tool (C4)`.

| Emoji | Type | Use for |
|---|---|---|
| ✨ | `feat` | a new feature, tool or plan step |
| 🐛 | `fix` | a bug fix |
| ♻️ | `refactor` | a refactoring without a behavior change |
| 📝 | `docs` | README, AGENTS.md, PLAN.md, skills, doc comments |
| ✅ | `test` | tests and fixtures only |
| 👷 | `ci` | GitHub Actions and release configuration |
| ⬆️ | `build` | dependency upgrades, `go.mod` |
| 🔧 | `chore` | tooling and configuration |
| ⚡️ | `perf` | performance |
| 💥 | | breaking change (in addition to the type's emoji) |

The PR body uses emoji section headings:

```markdown
## 🎯 What
## 🧩 How
## 🧪 Tests
## 📚 Docs
```

Issues use `🐛 Bug`, `💡 Idea` or `🗺️ Plan step` in the title, and the same headings where
they fit. Use emojis in headings and titles, not in every sentence.

**Commit messages stay plain conventional commits, without emojis.** Semantic release parses
them, and a leading emoji hides the type. PRs are merged with merge commits, so the emoji in
the PR title never reaches the parsed history. If a PR is ever squash-merged, remove the emoji
from the squash commit title.

## Dependencies

- **YAML is `go.yaml.in/yaml/v3`** (the maintained successor of `gopkg.in/yaml.v3`), used by
  [mode/mode.go](mode/mode.go) `decodeFrontMatter`. `go.yaml.in/yaml/v4` is still a release
  candidate (`v4.0.0-rc.6` when this was written). **Reminder:** when you touch `go.mod`, check
  `go list -m -versions go.yaml.in/yaml/v4`; once there is a `v4.0.0` without a pre-release
  suffix, open an issue to switch. v4 loads with `yaml.Load(data, &v, yaml.WithKnownFields())`
  and needs no `io.EOF` case for an empty front matter.
- Front matter is split by [mode/frontmatter.go](mode/frontmatter.go) `SplitFrontMatter`, not by
  a library: the format is a `---` line, YAML, a `---` line, and the body.

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
`FromThunk`, `FromReaderIOResult`, `Local`, `TailRec`), not by rebuilding its internal double-`Reader`
shape from `reader` / `context/readerioresult`. Use `Asks` + `Map` to build an effect from a pure
`Deps` getter, `FromReaderIOResult` / `FromReaderIO` / `FromReaderThunk` (fp-go ≥ v2.11.0) for a
getter that does I/O, and `Local` to reuse an existing `Effect` / `Kleisli` under a different environment. The full rules
and examples are in the [`fp-go` skill, section *Composing `Effect[C, A]`*](.bob/skills/fp-go/SKILL.md#composing-effectc-a);
[openai/request.go](openai/request.go) `ChatCompletion` is the worked example.

### Everything is read-only: `Ap` runs concurrently

**`Ap` of the IO-based monads runs its two sides on two goroutines.** `io`, `ioresult`,
`context/readerioresult` and `effect` all dispatch `Ap` to `io.MonadApPar`, and so does
everything built on `Ap`: `ApS`, `ApplicativeMonoid(…).Concat`, `TraverseArray` and the
other `Traverse*` / `Sequence*`. Only `Chain`, `Bind`, `Tap*` and the `…Seq` variants run
in order. So any code that runs
inside an effect may run concurrently with another part of the same effect, and it must
not share mutable state:

- **Dependencies, getters and captured values are read-only.** A `Deps` struct is built
  once by its `Make…Deps` wiring and never changed. A getter returns the same value or
  function every time. No caches, counters, `append` to a captured slice, or map writes
  behind a getter, a `Kleisli` or a closure.
- **Values passing through a pipeline are immutable.** Build a new value (`A.Map`, a lens
  setter that assigns) instead of changing an input slice, map or struct in place.
- **Test fakes are read-only too.** A fake answers from fixed data (a `map` lookup, a
  constant) and the test asserts on the effect's *output*. If a test really has to record
  calls, guard the record with a `sync.Mutex` and compare it with `assert.ElementsMatch`,
  because the order is not fixed. [prompt/projectrules_test.go](prompt/projectrules_test.go)
  `TestProjectRulesReadsWorkspacePaths` is the example.
- **A data race is a bug in the shared state, not in `Ap`.** Never switch to `ApSeq` or
  `Chain` to silence the race detector; make the state read-only.

CI runs `go test -race`. It can't run on this laptop (no cgo), so a race only shows up in
CI: check every fake and dependency against these rules before you push.

### Every function is nullary and point-free

This is the goal for **every** function, not a preference: `func f() Reader[A, B]`, and
the value it returns is one composition of named functions, generated lens getters and
combinators. A nullary function that returns `func(a A) B { … }` has only moved its
parameter and is a review finding. A branch on a field (`if step.Landed`) is a
`predicate.Fold` over the field's generated lens, third-party and generic types included;
better still, build the target type where the value is constructed so no converter is
needed (`session.Next` lands a `TurnResult` directly). The only exceptions are leaves (a
multi-field formatter, a raw I/O sink, a `select`), wiring, tests and `main`, each named
as such in its doc comment with the reason. The rules and the worked example are in the
[`fp-go` skill, Rules 4 and 5](.bob/skills/fp-go/SKILL.md#rule-4--every-function-is-point-free).

### Closures over a pipeline's input

A pipeline that reads its input in more than one place is a `reader.Reader[R, A]`, not a
closure: `reader.Ap` for two independent reads, `reader.Chain` for a later one, and
`reader.TraverseArray` to hand the environment to each element's reader (`reader.Read`
belongs at the edge only). `reader.Reader[R, A]` is an alias for `func(R) A`, so the
return type can be widened to it without touching a single call site. The rules and the
worked example are in the [`fp-go` skill, section *Readers instead of closures over the
input*](.bob/skills/fp-go/SKILL.md#readers-instead-of-closures-over-the-input);
[replay/task.go](replay/task.go) `chunkAt` is that example.

### Several readers over one environment

A value whose fields are each read from the same input does not need an accumulator struct
with generated lenses and do-notation. Curry the constructor (`F.Curry3`) and apply one
reader per field with `readerresult.Ap`, naming the partial applications as type *aliases*
(`=`) because `Ap` cannot infer them. Independent fields are applicative; a field computed
from an earlier one is monadic and *does* want `Bind` and a lens. The rules are in the
[`fp-go` skill, section *Several readers over one environment*](.bob/skills/fp-go/SKILL.md#several-readers-over-one-environment);
[replay/task.go](replay/task.go) `turnOf` is the worked example.

### Pure core, effect in the reader

An operation that needs all of its input before it can produce anything is a pure function
over a slice, not a `SeqResult` consumer returning an `IOResult`. Keep the effect in the
reader that produces the slice. [replay/record.go](replay/record.go) is the pattern: the
`Read*` functions stream (`SeqResult[Record]`, so an operator such as `Requests` never
materializes the log) and the `Load*` functions materialize (`IOResult[[]Record]`), while
[replay/task.go](replay/task.go) `GroupTasks` is a `result.Kleisli[[]Record, []Task]` that
composes with either through `ioresult.ChainResultK`.

### Composition as a value is a `reader` combinator

A function `func(R) A` *is* a `Reader[R, A]`, so whenever function composition has to be
passed around as a value (into `reader.Map`, `reader.Ap`, a `Flow`), use the named
combinator instead of rebuilding it from `F.Flow2` with explicit type arguments:

| Hand-built | Concept | Use instead |
|---|---|---|
| `F.Bind2nd(F.Flow2[…], g)` — `f ↦ g ∘ f` | post-compose (functor map) | `reader.Map[R](g)` |
| `F.Flip(F.Curry2(F.Flow2[…]))` — `g ↦ f ↦ g ∘ f` | the same, curried | `reader.Map[R, A, B]` |
| `F.Curry2(F.Flow2[…])` — `f ↦ g ↦ g ∘ f` | the same, in data-flow order | `reader.Compose[C, R, B]` |
| `F.Bind1st(F.Flow2[…], g)` — `f ↦ f ∘ g` | pre-compose (contramap) | `reader.Local[A](g)` |

Direct `F.Flow2(f, g)` over two known functions stays as it is; the combinators only pay
off when the composition itself is the argument.

**Two stages, each read from the same environment**, are composed one way in this
repository: in data-flow order, the stage that runs first comes first, `reader.Compose`
is mapped over it and the later stage is applied:

```go
F.Pipe2(
	first,                                   // Reader[E, func(A) B]
	reader.Map[E](reader.Compose[C, A, B]),  // Reader[E, func(func(B) C) func(A) C]
	reader.Ap[func(A) C](second),            // second: Reader[E, func(B) C]
)
```

Don't swap the stages to make `reader.Map[…]` fit instead; it reads backwards.
[tools/workspace.go](tools/workspace.go) `ResolvePath` and
[tools/listfiles.go](tools/listfiles.go) `listingOf` are the worked examples; `listingOf`'s
first stage also post-composes with `reader.Map[[]FileEntry](joinLines())`.
[tools/glob.go](tools/glob.go) `globListing` shows `reader.Local` for pre-composition.

**When both stages are endomorphisms** (`Reader[E, Endomorphism[T]]`), their composition
is a monoid: `reader.ApplicativeMonoid[E](endomorphism.Monoid[T]()).Concat(second, first)`
replaces the `Map` / `Compose` / `Ap` triple. `Concat` runs right to left, so the stage
that runs first is the *last* argument; comment it when the order matters.
[tools/truncate.go](tools/truncate.go) `truncateThunk` and [tools/grep.go](tools/grep.go)
`compilePattern` are the examples.

### Slice-valued functions are `array.Kleisli` / `array.Operator`

Write a function type that returns a slice by its fp-go alias, never spelled out:
`func(Mode) []string` is `A.Kleisli[Mode, string]`, and a function from a slice to a slice,
`func([]Record[string, Mode]) []Mode`, is `A.Operator[Record[string, Mode], Mode]`. This
applies to return types, type aliases and explicit type arguments alike; generated `gen.go`
files are left as the generator writes them. The aliases are identical types, so switching
never breaks a call site. Details in the [`fp-go` skill, Rule 2](.bob/skills/fp-go/SKILL.md#rule-2--fp-go-best-practices).

### Combinators that don't exist, and what to use instead

Each of these was searched for and confirmed absent in fp-go v2 with `go doc`:

| Wanted | Doesn't exist | Use instead |
|---|---|---|
| split a slice into runs at a predicate | `array.Chunk`, `Split`, `GroupBy`, anything in `iter` (`A.Partition` exists but yields two groups, not consecutive runs) | build it as a reader (see *Closures over a pipeline's input*) |
| `TakeWhile` / `DropWhile` | in `array` | `A.Slice(low, high)`, `A.SliceRight(n)` over indices |
| distinct, first occurrence wins | `A.Distinct` | `A.Uniq(F.Identity[K])` |
| drop the `None`s of a `[]Option[A]` | `A.Compact` | `option.CompactArray` |
| pair each element with its successor | `A.Pairwise` | `A.Zip` over the slice and `A.SliceRight(1)` of it |
| add context to a `Result`'s error | | `result.MapLeft[A](ER.OnError("task %q", id))`, which wraps with `%w` |
| `Option[A]` to `Result[A]` | | `result.FromOption[A](ER.OnNone("..."))` |
| map value and error of a `ReaderIOResult` at once | `BiMap` in `context/readerioresult` (its package doc lists it) | `thunk.Map` and `thunk.MapLeft`, composed as endomorphisms (see *Composition as a value*) |
| `if p(x) { return f(x) }; return x` | `F.Ternary` is deprecated | `P.Fold(F.Identity[T], f)(p)` |
| the first `n` bytes of a string | a slicing combinator in `string` | a leaf (`prefix` in `tools/truncate.go`), then `F.Bind2nd(strings.ToValidUTF8, "")` |
| `liftA2` over two readers of one environment | `reader.LiftA2` | `reader.Map[R](F.Curry2(g))` + `reader.Ap[C](fb)`; it is applicative, not `Chain` |
| lift a `Reader[C, IOResult[A]]` / `Reader[C, IO[A]]` / `Reader[C, Thunk[A]]` (a deps getter) into `Effect` | before fp-go v2.11.0 | `effect.FromReaderIOResult` / `FromReaderIO` / `FromReaderThunk`, never `effect.Asks` + `ChainIOResultK(F.Identity)` (or `ChainIOK` / `ChainThunkK`), nor `reader.Map[C](thunk.FromIOResult)`, which rebuilds `Effect`'s shape by hand. For a getter returning an `ioresult.Kleisli`, as a Kleisli over `x`: `F.Flow2(F.Flip(getter), effect.FromReaderIOResult[C, A])`, as in `session/store.go` `readTaskFile` and `env.LookupEnvThunk`; for a fixed `x`, `effect.FromReaderIOResult(F.Flow2(getter, reader.Read[IOResult[A]](x)))`, as in `tools/readfile.go` `readBytes` |
| flatten an `Effect[C, IOResult[A]]` / `IO` / `Thunk` / `Effect` | before fp-go v2.11.0 | `effect.FlattenIOResult` / `FlattenIO` / `FlattenThunk` / `Flatten`, not `effect.Chain(effect.FromThunk)` or `ChainXxxK(F.Identity)`, as in `openai/request.go` `ChatCompletion` |
| run an `IOResult` inside `Effect` (`ChainIOResultK`, `TapIOResultK`, `FromIOResult`) | before fp-go v2.6.0 | `effect.ChainIOResultK(f)` instead of `effect.ChainThunkK(F.Flow2(f, thunk.FromIOResult))` |
| recover from, or rename, one kind of error only (`errors.Is` / `errors.As`) | before fp-go v2.8.0 (`effect`, `thunk`) and v2.9.0 (`result`, `ioresult`, `readerresult`, `readerioresult`) | `CatchIs(target, f)` of the monad at hand, by type `CatchAs` / `MapErrorAs` / `TapErrorAs`, as in `prompt/projectrules.go` `readOptional` (`effect`), `tools/executecommand.go` `timeoutError` (`thunk`) and `env.ReadDotEnv` (`ioresult`). Never a hand-written `errors.Is` predicate in a `P.Fold`, nor `ChainLeft` over `result.ErrorIs` |
| a zero test (`x == 0`) | an `eq` shortcut | `P.IsZero[int]()` / `P.IsNonZero[int]()` from `predicate`, not `eq.Equals(eq.FromStrictEquals[int]())(0)` |
| apply an effect of a function to a plain value (`Flap`), or `MonadAp` | in `effect` | `effect.Of[C, F](f)` + `effect.Ap(fa)`, as in `cli/run.go` `ask` |
| membership in a slice (`A.Elem`, `A.Contains`) | in `array` | `F.Bind1st(slices.Contains[[]string], names)`, as in `mode/mode.go` `isTool`; as a reader over the slice `F.Curry2(slices.Contains[[]string])`, as in `prompt/compare.go` `containedIn` |
| pair a value with something computed from it (fan-out, `&&&`) | in `pair` | `F.Flow2(pair.Of[T], pair.MapTail[T](g))`; to update an existing pair instead, one `MapHead` / `MapTail` endomorphism per side, as in `prompt/compare.go` `enter` |
| a fold over a slice that keeps state (`A.Scan`, `A.Unfold`) | in `array` (`iter.Scan` yields every state) | `A.Reduce` over a `Pair` state, with a nullary step as `F.Uncurry2(F.Flip(lineStep()))`, as in `prompt/compare.go` `SectionTags` (see the [`fp-go` skill](.bob/skills/fp-go/SKILL.md#a-scan-with-state-areduce-over-a-pair)) |
| `[]byte` to `string` | a `string` combinator | `B.ToString` from `github.com/IBM/fp-go/v2/bytes` |
| a total case split (`Fold`, `Map` over the landed value) of a `tailrec.Trampoline` | in fp-go v2.13.0 (only the partial `tailrec.IsLand` / `GetLand` / `GetBounce` and `prism.FromLand` / `FromBounce`) | build the needed `Trampoline` type where it is constructed, as `session.Next` lands a `TurnResult`; read one case in a test with `option.Unwrap(prism.FromLand[B, L]().GetOption(step))` |
| split Markdown at its YAML front matter | a front matter parser in fp-go (and we don't use `adrg/frontmatter`) | `mode.SplitFrontMatter`, then decode the head with `go.yaml.in/yaml/v3` and `KnownFields(true)` |

Before concluding that a combinator is missing, `go doc` the **whole** package: the ones
that return an `Operator` sit in an indented block that `grep "^func"` skips.

### Two build breaks worth avoiding

- **A type parameter named `A`, `F`, `P`, `S`, `J` or `ER` shadows the package alias** for
  the whole function body, so `func chunkAt[A any](…)` turns every `A.Map` inside it into
  a compile error. Name type parameters `T`.
- **A named slice type doesn't compose generically.** `json.RawMessage` is assignable to
  `[]byte` in a direct call, but not through `F.Flow2` / `option.Chain`, where inference
  sees two different types. Convert in a named leaf (`rawBytes` in
  [replay/task.go](replay/task.go)).

### Tooling

- `mcp__fp-go__search_examples` fails with an FTS5 syntax error on any query containing a
  comma, and has no entry for most combinators. For a signature,
  `go doc github.com/IBM/fp-go/v2/<pkg>` on the pinned module is faster and authoritative.
- `gofmt -l` lists nearly every file in the repository, including untouched ones: the
  working tree is CRLF and the repository stores LF. Never run `gofmt -w` — it rewrites
  every line of every file. Judge formatting from `gofmt -d` on the file you changed.
- `runCodeActions.bat <dir>` (the cleanup in [PLAN.md](PLAN.md) rule 10) runs from
  PowerShell, not from the Bash tool.
