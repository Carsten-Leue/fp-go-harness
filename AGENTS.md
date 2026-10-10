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
| lift a `ReaderIOResult[C, A]` (a deps getter returning an `ioresult.Kleisli`) into `Effect` | `effect.FromReaderIOResult` | `effect.Asks(getter)` + `effect.ChainThunkK(F.Flow2(reader.Read(x), thunk.FromIOResult))`, as in `tools/walk.go` `walkDir` |

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
