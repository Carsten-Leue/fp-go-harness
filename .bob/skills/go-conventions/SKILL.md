---
name: go-conventions
description: Use this skill whenever writing, generating, changing, reviewing or testing Go code in this repository. It holds the project's Go conventions with examples - the four mandatory rules (fp-go best practices, monadic over imperative, point-free, the fp-go MCP server as source of truth), pure functions and dependency injection, value semantics, Go doc comment style, HTTP header and MIME constants, lens generation with go generate, testing Result / Validation / Effect values, and the anti-patterns to avoid. Load it before the first line of Go, always together with the fp-go skill.
---
# Go conventions for this repository

The short rules are in [`AGENTS.md`](../../../AGENTS.md). This skill has the
reasoning and the examples for the repository's conventions. Load it **together with the
[`fp-go`](../fp-go/SKILL.md) skill** for every Go change: that skill holds the four
mandatory rules and the workflow with the fp-go MCP server.

## The four rules for every code change

Every Go change, whether you write it, generate it or review it, must:

1. **Follow fp-go best practices**: v2 imports, data-last, the simplest monad that fits,
   `Result` over `Either[error, A]`, `Eitherize` instead of hand-written closures, lenses,
   `Tap*` for logging, no hidden mutation.
2. **Use monadic over imperative style wherever possible**: `Result`, `Option`, `thunk`,
   `Effect`, `Chain`, `TraverseArray` instead of `if err != nil` chains, `nil` checks and
   accumulating `for` loops. Imperative code only at the boundary (`main`, handler glue)
   or in a leaf without an fp-go lift, with a reason.
3. **Use point-free style wherever possible**: compose named functions, lens getters and
   combinators (`N.MoreThan`, `S.IsEmpty`, `P.Not`, `F.Constant1`) with `F.Flow` /
   `F.Pipe`; lambdas only at the leaves.
4. **Use the fp-go MCP server as the primary source of truth**: `use_skill`,
   `get_example` and `search_examples` before writing or judging an fp-go call; `go doc`
   on the pinned module as the fallback; never memory.

The details, the replacement table for imperative shapes, the accepted exceptions and the
examples are in [`fp-go`](../fp-go/SKILL.md#rule-3--monadic-over-imperative-wherever-possible).
The rest of this skill covers what the MCP server doesn't know: this repository's
conventions.

## Design principles

### Pure functions

Prefer functions that return the same output for the same input and have no side effects.
They are easy to test, safe to run in parallel, and simple to reason about.

Keep these out of pure functions: `context.Context`, file, network and database I/O,
`os.Getenv` / `os.LookupEnv`, channels, `sync` and `sync/atomic`, `time.Now` /
`time.Sleep`, random numbers, package-level mutable state, logging and `fmt.Print`.

```go
// BAD: hidden dependency on the environment
func BuildURL(userID string) string {
    baseURL := os.Getenv("BASE_URL")
    return fmt.Sprintf("%s/%s", baseURL, userID)
}

// GOOD: all inputs are parameters
func BuildURL(baseURL, userID string) string {
    return fmt.Sprintf("%s/%s", baseURL, userID)
}
```

Prefer **total** functions, which return a valid result for every input, over partial ones
that fail or panic for some. Use types that make invalid states unrepresentable, or return
`Option` / `Result`.

### I/O at the boundaries

Read the environment and configuration once, in `main` or a handler, and pass the values
into pure business logic, not `os.Getenv` in the middle of the code.

### Dependencies first, parameters second

- **Dependencies** stay constant across calls: clients, configuration, lenses, codecs.
  They are resolved once at startup.
- **Parameters** change with each call: request data, user input, filters.

```go
// Pattern 1: dependency-first (the default)
func BuildURL(deps Dependencies) func(path string) string {
    return func(path string) string {
        return fmt.Sprintf("%s/%s", deps.GetBaseURL(), path)
    }
}

buildURL := BuildURL(config) // inject once
url1 := buildURL("users")    // vary the parameter

// Pattern 2: Reader - capture the parameter, inject the dependency later
func BuildURLReader(path string) func(Dependencies) string {
    return func(deps Dependencies) string {
        return fmt.Sprintf("%s/%s", deps.GetBaseURL(), path)
    }
}
```

A higher-order function pre-computes its closures once and returns a function that reuses
them:

```go
func BuildHeaders(deps Dependencies) func() string {
    buildURL := BuildURL(deps)
    buildBearer := BuildBearerToken(deps)
    return func() string {
        return fmt.Sprintf("%s\n%s", buildURL("identities"), buildBearer())
    }
}
```

### Minimal interfaces, composed

Each interface holds the smallest set of methods a function needs. Compose bigger ones by
embedding:

```go
type URLBuilder interface{ GetBaseURL() string }
type Authenticator interface{ GetAPIKey() string }

type APIClient interface {
    URLBuilder
    Authenticator
}
```

### Stateless code, value semantics

Keep implementations stateless; if state is needed, pass it in and return it. Pass and
return structs by value: they stay on the stack, avoid GC pressure, and are faster than
pointers even for large structs (up to about 1 KB). Use a pointer only to mutate the
original, to share mutable state, for structs over about 1 KB, or for pointer-receiver
interfaces.

```go
func NewPoint(x, y float64) Point  { return Point{X: x, Y: y} }  // GOOD: stack
func NewPointP(x, y float64) *Point { return &Point{X: x, Y: y} } // AVOID: heap
```

## Anti-patterns

```go
// Imperative error plumbing where a pipeline fits - see the fp-go skill, Rule 3
v, err := strconv.Atoi(s)
if err != nil {
    return 0, err
}
return v * 2, nil
// instead:
F.Flow2(result.Eitherize1(strconv.Atoi), result.Map(N.Mul(2)))

// A lambda above the leaves - see the fp-go skill, Rule 4
A.Filter(func(n int) bool { return n > 0 })
// instead:
A.Filter(N.MoreThan(0))

// Mutable global state - races
var config Config
func UpdateConfig(c Config) { config = c }

// Logging inside business logic - a side effect
func CalculateTotal(items []Item) float64 {
    log.Printf("Calculating total for %d items", len(items))
    return sum(items)
}

// A closure passed to Map that mutates a captured variable - breaks under TraverseArray
var names []string
A.Map(func(u User) User { names = append(names, u.Name); return u })
// instead:
names := F.Pipe1(users, A.Map(getName))

// A lens setter that appends to a shared slice - append may reuse the backing array
func(u User, t []string) User { u.Tags = append(u.Tags, t...); return u }
// instead, assign a freshly built value:
func(u User, t []string) User { u.Tags = t; return u }
```

## Lens code generation

Packages with `// fp-go:Lens`-annotated structs have a `gen_lens.go` produced by
`go tool gen lens`. It holds lens, prism and accessor functions for every field.

**Never hand-write lenses into `gen_lens.go`.** When you add, rename or remove a field of
such a struct, or annotate a new struct:

1. Change only the source Go file.
2. Run the generator for the package, or for everything:

   ```
   go generate ./session/...
   go generate ./...
   ```

3. Commit the regenerated `gen_lens.go` with the struct change.

Hand-written lenses miss the `Ref` / `Prism` / `LensO` variants, drift from the naming
convention, and are overwritten by the next `go generate`. If the generator fails, fix the
struct or type error first; don't patch `gen_lens.go`.

## HTTP header names and MIME types

1. Use fp-go's `github.com/IBM/fp-go/v2/http/headers` constants whenever it defines the
   header (`HH.Accept`, `HH.Authorization`, `HH.ContentType`, `HH.CacheControl`,
   `HH.Origin`, `HH.XRequestID`, the `HH.AccessControl*` names, …). They are lowercase
   since fp-go v2.3.138. Don't repeat them as string literals.
2. For every other header, define a **lowercase** constant of our own with this doc
   comment:

   ```go
   // The name is lowercase because HTTP/2 (RFC 9113, Section 8.2) and HTTP/3
   // (RFC 9114, Section 4.2) require field names to be lowercase on the wire.
   ```

MIME types come from `github.com/IBM/fp-go/v2/http/content`, imported as `HC` (`C` is
taken by `urfave/cli`): `HC.JSON`, `HC.TextEventStream`, `HC.TextPlain`,
`HC.FormEncoded`, …

```go
// GOOD
const HEADER_REQUEST_ID = HH.XRequestID            // fp-go defines it: reuse it
const HEADER_TEST_FILE = "x-test-file"             // fp-go doesn't: our own, lowercase
req.Header.Set(HH.ContentType, HC.JSON)

// BAD
const HEADER_REQUEST_ID = "x-request-id"           // redefines what fp-go provides
const HEADER_TEST_FILE = "X-Test-File"             // our own constant, not lowercase
req.Header.Set("Content-Type", "application/json") // literals for header and MIME type
```

- Before defining a header constant, check fp-go's `http/headers`, then the constants
  already defined in this repository.
- Import fp-go's package with an alias (`HH`; older files use `h`) when the file has a
  local variable named `headers`.
- Access headers only through `http.Header.Get` / `Set` / `Add` / `Values`,
  `gin.Context.GetHeader` / `Header`, or `option.WithHeader`; they ignore case.
- Never index an `http.Header` map with a lowercase name (`req.Header["x-request-id"]`):
  Go stores canonical keys (`X-Request-Id`). Don't build an `http.Header` as a map literal;
  call `Set`.
- fp-go's constants are already lowercase: use them directly as keys of lowercase lookup
  tables, without `strings.ToLower`. Names that arrive at runtime are lowercased before
  the lookup.
- `http.CanonicalHeaderKey` only in tests that build casing variants, derived from the
  constant: `http.CanonicalHeaderKey(HH.Authorization)`, `strings.ToUpper(HH.Authorization)`.
  Made-up fixture headers in tests are lowercase literals (`"x-custom"`).
- These rules cover HTTP header names only. JSON keys such as MCP `_meta` fields are
  case-sensitive and not affected.

## Doc comments

Go doc comments are plain Go doc, never Markdown: `go doc` and pkg.go.dev show `**bold**`,
`# headers` and `[text](url)` literally.

| Don't | Do |
|---|---|
| `# Header`, `## Sub` | a plain-text line as a section header |
| `[text](url)` | the plain URL, or a doc link `[Type]`, `[pkg.Func]`, `[Type.Method]` |
| `**bold**`, `*italic*` | plain text |
| ```` ``` ```` fences | a block indented by a tab or 4 spaces |
| `*` / `+` lists | `-` lists, indented |

Order of a function comment. Include a section only when it adds something the signature
and names don't already say:

1. One-line summary, starting with the function name.
2. Behaviour: edge cases, important details.
3. Parameters, one line each, only when not self-evident.
4. Returns.
5. Example, only when the call pattern is non-obvious.
6. See also, with doc links.

```go
// Close returns the resource to the pool for reuse.
// It never returns an error. The resource is pushed back onto the pool's stack.
func (p *pooledEntry[T]) Close() error {

// MessageCountValidator creates a validator that checks for an exact message count.
//
// It ensures the request has exactly the expected number of messages in the
// conversation history.
//
// Example:
//
//	validator := MessageCountValidator(4)
//	result := validator(params)
//	// Right(VOID) for exactly 4 messages, Left(error) otherwise
func MessageCountValidator(expected int) Validator {
```

Don't:

- start with "This function…" / "This method…",
- restate parameter and return types,
- use banners (`====`, `----`),
- comment self-evident lines (`// increment i`),
- paraphrase the code line by line.

Inline comments explain only a non-obvious *why*:

```go
// Retry 3 times — upstream API returns transient 503s during deploys.
for i := 0; i < 3; i++ {
```

When reviewing documentation, check that every function it names exists with that exact
spelling (singular vs. plural, exported vs. unexported), and write the reference as a doc
link (`[AddChatCompletionToolsFromMCP]`).

## Testing

Pure functions need no setup; call them with test values. For functions with dependencies,
pass a small mock of the minimal interface.

Compare `Result` and `Validation` values as a whole instead of unwrapping them:

```go
// AVOID
res := writer(data)()
require.True(t, result.IsRight(res))
n, err := result.Unwrap(res)
require.NoError(t, err)
assert.Equal(t, 3, n)

// PREFER
assert.Equal(t, result.Of(3), writer(data)())
assert.Equal(t, validation.Success(42), codec.Decode(env))
```

### Mocking Effect and Thunk

`Effect[R, A]` (an alias of `ReaderReaderIOResult[R, A]`) has three layers:

```go
func(r R) func(ctx context.Context) func() result.Result[A]
```

`Thunk[A]` is **not** `func() result.Result[A]`: it is `ReaderIOResult[A]`, i.e.
`func(context.Context) func() result.Result[A]`. A mock must provide every layer:

```go
func mockReadFile(files map[string]string) effect.Effect[string, []byte] {
    return func(path string) func(context.Context) func() result.Result[[]byte] {
        return func(_ context.Context) func() result.Result[[]byte] {
            return func() result.Result[[]byte] {
                if content, ok := files[path]; ok {
                    return result.Of([]byte(content))
                }
                return result.Left[[]byte](os.ErrNotExist)
            }
        }
    }
}

res := mockReadFile(map[string]string{"file.txt": "content"})("file.txt")(context.Background())()
assert.Equal(t, result.Of([]byte("content")), res)
```

"cannot use … as Effect[R, A]" nearly always means a missing layer or the `context.Context`
in the wrong position. Check the expansion with
`go doc github.com/IBM/fp-go/v2/effect Thunk` and
`go doc github.com/IBM/fp-go/v2/context/readerioresult ReaderIOResult`.

## Checklist

- [ ] fp-go MCP server used as the source of truth: topic skills loaded, every fp-go call checked with `get_example` / `search_examples` / `go doc`
- [ ] fp-go best practices; monadic over imperative; point-free, lambdas only at the leaves (the [`fp-go`](../fp-go/SKILL.md) checklist)
- [ ] Pure where possible; I/O at the boundaries; environment read once at startup
- [ ] Dependencies separated from parameters; minimal interfaces; no hidden dependencies
- [ ] Value semantics; pointers only where needed; stateless and thread-safe
- [ ] Total functions; the simplest effect type that fits (Option → Result → IOResult → ReaderIOResult → Effect)
- [ ] No mutation inside `Map` / `Chain` / lens setters
- [ ] Lenses regenerated with `go generate`, not hand-edited
- [ ] Header names and MIME types from fp-go where defined; our own header constants lowercase with the RFC note
- [ ] Go doc comments, no Markdown
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` clean
