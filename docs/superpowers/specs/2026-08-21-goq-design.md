# goq — LINQ for Go 1.27

**Status:** approved design, not yet implemented
**Module:** `github.com/oleexo/goq`
**Go:** 1.27.0 (hard floor — depends on generic methods)

## 1. Purpose

A LINQ-to-Objects equivalent for Go: a lazy, composable, type-safe query
pipeline over in-memory collections, with first-class support for
concurrent sources and parallel execution.

Go 1.27 allows type parameters on methods. This is the feature that makes
LINQ possible in Go: before it, `Select` could not be a method, because a
method could not introduce the fresh result type `R`. Every prior Go LINQ
attempt therefore degraded into free-function nesting
(`Select(Where(q, p), f)`) or gave up type safety with `any`. Neither is
necessary now.

### Goals

- Fluent, chainable, statically typed query composition.
- Deferred execution; nothing runs until a terminal operator.
- 51 query operators plus 8 source constructors.
- Concurrent sources (channels, ctx cancellation) and a PLINQ-style
  parallel engine.
- Zero external dependencies.

### Non-goals

- **No IQueryable / expression trees.** Go has no expression-tree
  representation of a closure, so SQL translation would require a separate
  builder DSL. Out of scope.
- **No `Cast` / `OfType` / `AsEnumerable`.** Reflection-based element
  casting defeats the type system that motivates the library.
- **No `ToChan` sink.** Deliberately excluded; may be revisited.

## 2. Go 1.27 constraints discovered

These were established empirically against `go1.27.0 darwin/arm64` and
they dictate the architecture. All three are load-bearing.

### 2.1 Generic methods work on concrete types

```go
func (q Query[T]) Select[R any](f func(T) R) Query[R]  // compiles
```

### 2.2 Interfaces cannot declare generic methods

```go
type Enumerable[T any] interface {
    Select[R any](f func(T) R) Enumerable[R]   // interface method must have no type parameters
}
```

**Consequence:** the pipeline type must be a concrete struct. There is no
`IEnumerable[T]` analogue and no way for a third party to implement the
query interface. Extension happens through `FromSeq(iter.Seq[T])`, which
is the real extension point.

### 2.3 Methods whose result element type derives from `T` are rejected

```go
func (q Query[T]) Chunk(n int) Query[[]T]                            // instantiation cycle: T instantiated as []T
func (q Query[T]) GroupBy[K comparable](f func(T) K) Query[Group[K,T]] // instantiation cycle
```

`Query[T]` having such a method requires instantiating
`Query[[]T]`, which requires `Query[[][]T]`, without limit. Methods with
*fresh* parameters (`Select[R]`, `SelectMany[R]`, `Join[U,K,R]`) are fine,
because `R` is supplied by the caller and does not recurse.

**Resolution — satellite types.** The operator returns a *distinct* type
whose method set deliberately omits the cycle-forming operators:

```go
type GroupQuery[K comparable, T any] struct{ seq iter.Seq[Group[K,T]] }

func (q Query[T]) GroupBy[K comparable](key func(T) K) GroupQuery[K,T]
func (g GroupQuery[K,T]) Select[R any](f func(Group[K,T]) R) Query[R]  // re-enters Query
```

Instantiation terminates, and the chain stays fully fluent. Verified:
`From(people).Where(...).GroupBy(...).Where(...).Select(...).ToSlice()`
compiles and runs.

This also buys correctness: `OrderedQuery[T]` is the only type exposing
`ThenBy`, so `From(xs).ThenBy(...)` fails to compile — the same protection
C# gets from `IOrderedEnumerable`.

### 2.4 Methods can introduce *constrained* fresh parameters

```go
func (q Query[T]) Sum[N Numeric](sel func(T) N) N            // the C# Sum(x => x.Price) shape
func (q Query[T]) MaxBy[K cmp.Ordered](key func(T) K) (T, bool)
```

A method cannot add a constraint to the receiver's own `T`, so bare
`Query[int]` aggregation additionally needs free functions
(`goq.Sum(q)`, `goq.Min(q)`). Both forms ship; the method is primary.

## 3. Architecture

### 3.1 Pipeline types

| Type | Carries | Terminal shape |
|---|---|---|
| `Query[T]` | `iter.Seq[T]` | `ToSlice() []T`, `First() (T, bool)` |
| `TryQuery[T]` | `func(ctx) iter.Seq2[T, error]` | `ToSlice(ctx) ([]T, error)` |
| `ParQuery[T]` | plan + pool options | as `TryQuery` |
| `OrderedQuery[T]` | source + `[]func(a,b T) int` | satellite; enables `ThenBy` |
| `GroupQuery[K,T]` | `iter.Seq[Group[K,T]]` | satellite; breaks §2.3 cycle |
| `ChunkQuery[T]` | `iter.Seq[[]T]` | satellite; breaks §2.3 cycle |

`TryQuery` covers **both** the fallible-synchronous case
(`From(xs).SelectErr(parseInt)`) and the async-streaming case
(`FromChan(ch)`). From the operators' point of view, "async" adds only a
ctx and a concurrent producer, and both are properties of the source and
the terminal, not of the chain. A separate `AsyncQuery` type would have
left fallible-sync chains with nowhere to live.

Transitions: `.AsTry()`, `.AsParallel(opts...)`, `.AsSequential()`,
`.AsQuery()` off each satellite.

### 3.2 Deferred execution

Every non-terminal operator wraps the upstream iterator in a new closure;
nothing is pulled until a terminal operator ranges over it. `Query` holds
`iter.Seq[T]` directly. `TryQuery` and `ParQuery` hold a *plan*,
`func(context.Context) iter.Seq2[T, error]`, so that:

- ctx arrives at the call that actually blocks (`ToSlice(ctx)`), per Go
  convention, rather than being captured at the source;
- one pipeline value is reusable across different contexts.

### 3.3 Shared operator core

Operator logic lives once in `internal/seqcore` as free functions over
`iter.Seq` / `iter.Seq2`. The pipeline types are thin adapters. Without
this, ~50 operators across 3 engines is ~150 hand-written
implementations.
`seqcore` is also where semantics are unit-tested, independent of the
fluent surface.

### 3.4 Re-enumeration

A query is re-enumerable if and only if its source is. Slice-backed
queries re-execute freely on each enumeration.

Single-shot sources (`FromChan`) **must not** fail silently. They carry an
atomic consumed-flag, and a second terminal call returns
`goq.ErrConsumed` rather than an empty result:

```go
q := goq.FromChan(ch)
a, err := q.ToSlice(ctx)   // ok
b, err := q.ToSlice(ctx)   // nil, goq.ErrConsumed
```

`.Memoize()` opts into caching, making any query re-enumerable at the
cost of retaining every element — which forfeits streaming, so it is
opt-in only.

**Known gap, accepted.** This guarantee reaches only sources whose
terminals return an `error`, i.e. `TryQuery` and `ParQuery`. A caller who
wraps a single-shot iterator in `FromSeq` gets a `Query[T]`, whose
terminals have no error return, so re-enumeration there silently yields
nothing. `FromSeq`'s godoc must state that the caller guarantees
re-enumerability, and recommend `FromSeqTry` (which returns a
`TryQuery[T]`) for single-shot iterators. Closing the gap properly would
mean giving every `Query` terminal an error return, which would defeat
the `(T, bool)` design.

## 4. Error model

- `Query[T]` is infallible: no selector can fail, and no terminal
  reports a pipeline error. Selectors are pure `func(T) R`.
- Absence is `(T, bool)`, never a panic and never an `error`:
  `First`, `Last`, `ElementAt`, `Min`, `Max`. C#'s `...OrDefault` twins
  collapse into the discarded bool — `v, _ := q.First()` is
  `FirstOrDefault()`.
- **`Single` is the one exception**, returning `(T, error)` with
  `goq.ErrEmpty` / `goq.ErrMultiple`. `Single` exists to assert
  uniqueness, and `ErrMultiple` means the caller's data-model assumption
  is wrong — a different situation from "no match" and one that must be
  distinguishable. Note this error describes the *cardinality of the
  result set*, not a pipeline failure, so it does not contradict
  `Query[T]` being infallible.
- Failure enters via `...Err` operators (`SelectErr`, `WhereErr`,
  `SelectManyErr`) taking `func(T) (R, error)`, which yield a
  `TryQuery[R]`. A `...Ctx` variant takes `func(context.Context, T) (R,
  error)` for calls needing the ctx.
- **First error wins.** The pipeline short-circuits: no further elements
  are pulled, upstream producers are cancelled through a derived ctx, and
  all goroutines unwind before the terminal returns.
- **Terminals discard partial results.** `ToSlice(ctx)` returns
  `(nil, err)` on failure, so a truncated result cannot be mistaken for a
  complete one.
- `ctx` cancellation surfaces as `context.Canceled` /
  `context.DeadlineExceeded` from the terminal operator.

### 4.1 Sentinel errors

All comparable with `errors.Is`, all declared in one file:

| Sentinel | Meaning |
|---|---|
| `goq.ErrEmpty` | `Single` on an empty source |
| `goq.ErrMultiple` | `Single` on a source with more than one element |
| `goq.ErrConsumed` | second enumeration of a single-shot source (§3.4) |

Callback errors are returned verbatim, never wrapped, so `errors.Is` and
`errors.As` work against the caller's own error types.

## 5. Concurrency engines

### 5.1 Streaming (`TryQuery`)

`FromChan(ch)` selects on `ctx.Done()` and the channel, yielding
`ctx.Err()` on cancellation. Operators remain sequential — one element at
a time — over a concurrent source.

### 5.2 Parallel (`ParQuery`)

- Bounded worker pool; **unbuffered** input channel, so a slow consumer
  backpressures the producer instead of accumulating.
- Unordered by default. `AsOrdered()` tags elements with sequence numbers
  and reassembles them in a sink buffer capped at `window` — that cap is
  what bounds memory when a single element is pathologically slow.
- Options: `Workers(n)` (default `runtime.GOMAXPROCS(0)`), `Window(n)`
  (default `4×workers`), `AsOrdered()`.
- Cancellation and error both funnel through a derived ctx with `defer
  cancel()`, so workers unwind on every exit path including consumer
  `break`.

### 5.3 Required invariant: post-drain ctx check

**A validated bug, not a hypothetical.** When ctx fires, workers exit via
`case <-ctx.Done(): return` *without reporting*. The output channel then
closes normally, the reorder loop ends normally, and the terminal returns
a short slice with `nil` error — silent truncation presented as success.

Every concurrent terminal MUST therefore re-check ctx after the drain
loop:

```go
if cerr := ctx.Err(); cerr != nil {
    var z R
    y(z, cerr)
}
```

Confirmed to change the observed result from `err: <nil>` to
`err: context deadline exceeded`, with zero leaked goroutines. This needs
a dedicated regression test (§7.3).

## 6. Operator inventory

51 query operators and 8 source constructors. Go-native naming;
`docs/operators.md` carries the C# mapping (`ToArray`/`ToList` → `ToSlice`, `ToDictionary` → `ToMap`,
`OrderByDescending` → `OrderByDesc`).

| Family | Operators |
|---|---|
| Restriction | `Where` |
| Projection | `Select[R]`, `SelectMany[R]`, `Zip[U,R]` |
| Partitioning | `Take`, `TakeWhile`, `TakeLast`, `Skip`, `SkipWhile`, `SkipLast`, `Chunk` |
| Ordering | `OrderBy[K]`, `OrderByDesc[K]`, `ThenBy[K]`, `ThenByDesc[K]`, `Reverse` |
| Grouping | `GroupBy[K]`, `GroupBySelect[K,R]`, `ToLookup[K]` |
| Joins | `Join[U,K,R]`, `GroupJoin[U,K,R]` |
| Sets | `Distinct`, `DistinctBy[K]`, `Concat`, `Union`, `UnionBy[K]`, `Intersect`, `IntersectBy[K]`, `Except`, `ExceptBy[K]` |
| Aggregation | `Aggregate[A]`, `Count`, `Sum[N]`, `Min`, `MinBy[K]`, `Max`, `MaxBy[K]`, `Average[N]` |
| Elements | `First`, `Last`, `Single`, `ElementAt` |
| Quantifiers | `Any`, `All`, `Contains`, `SequenceEqual` |
| Materialize | `ToSlice`, `ToMap[K]`, `ToSet`, `Memoize` |
| Interop | `Seq` |
| Generators | `From`, `FromSeq`, `FromSeqTry`, `FromMap`, `FromChan`, `Range`, `Repeat`, `Empty` |

Return shapes, to remove any ambiguity:

- Elements (`First`, `Last`, `ElementAt`) and extremum aggregations
  (`Min`, `MinBy`, `Max`, `MaxBy`) return `(T, bool)`.
- `Single` returns `(T, error)` — see §4.
- `Average[N]` returns `(float64, bool)` — false on an empty source,
  never a division by zero.
- `Count` returns `int`; `Sum[N]` returns `N` (zero on empty).
- `Seq()` exposes the underlying iterator — `iter.Seq[T]` on `Query`,
  `iter.Seq2[T, error]` on `TryQuery`/`ParQuery` (as `Seq(ctx)`). This is
  the exit back into stdlib iteration (`slices.Collect`, `maps.Insert`,
  plain `range`) and, with `FromSeq`, the library's only extension point —
  §2.2 rules out an implementable interface.

Set operators require `comparable` elements; the `...By[K]` variants lift
that requirement to a `comparable` key, covering struct elements.

**The `...Err` / `...Ctx` variants are not counted separately.** Operators
that can host a fallible callback (`Select`, `SelectMany`, `Where`,
`Aggregate`, and the terminals) gain `...Err` and `...Ctx` forms on
`TryQuery` and `ParQuery`. They are the same operator with a fallible
callback, not new entries in the inventory.

## 7. Testing

Stdlib `testing` only. No testify — test-only dependencies still land in
consumers' `go.sum`.

### 7.1 Semantics
Table-driven tests per operator against `internal/seqcore`, each compared
to a naive slice implementation. Empty, single, and all-filtered-out
inputs are mandatory cases for every operator.

### 7.2 Laziness
A counting source asserting exact pull counts.
`From(xs).Select(f).First()` must invoke `f` exactly once. Lazy pipelines
regress silently into eager ones; only pull-count assertions catch it.

### 7.3 Concurrency
All tests under `-race`. Required cases:
- goroutine-leak assertion (before/after count) on every concurrent path,
  including consumer-`break` and error short-circuit;
- ctx cancellation mid-flight returns a ctx error — the §5.3 regression;
- error short-circuit stops the producer rather than draining it.

### 7.4 Contract tests for the new semantics
- `Single` returns `ErrEmpty` on empty and `ErrMultiple` on two-or-more,
  asserted via `errors.Is`.
- Second terminal call on a `FromChan` query returns `ErrConsumed`; the
  same query after `.Memoize()` returns equal results twice.
- `Seq()` output equals the corresponding `ToSlice()` output for every
  operator, so the two exits cannot diverge.

### 7.5 Ordered-parallel equivalence
Fuzz with randomized per-element work durations, asserting
`AsParallel().AsOrdered()` output is identical to the sequential result.

### 7.6 Examples and benchmarks
`Example*` tests that compile, doubling as godoc. Benchmarks against
hand-written loops so the abstraction's allocation cost is recorded rather
than assumed.

## 8. Documentation

A **Docusaurus 3.9.2** site, rooted at `docs/`, in docs-only mode.
Verified against current Docusaurus documentation rather than recalled.

### 8.1 Layout

```
goq/
  go.mod  query.go  try.go  parallel.go ...
  docs/                        <- Docusaurus site root
    package.json
    docusaurus.config.ts       <- TypeScript config
    sidebars.ts                <- autogenerated from folder structure
    src/css/custom.css
    static/
    docs/                      <- default content dir, no path override
      intro.md
      getting-started.md
      operators.md
      async-and-parallel.md
      migrating-from-linq.md
      design/
        generic-methods.md
        decisions.md
    superpowers/specs/         <- this spec; outside the content dir,
                                  so Docusaurus never publishes it
  .github/workflows/docs.yml
```

The Node toolchain lives entirely under `docs/` and is invisible to
consumers: `go get` never fetches it, and it does not weaken the
zero-runtime-dependency guarantee (§1). It does mean the repo carries two
build systems, and every npm command runs from `docs/`.

`.gitignore` must cover `docs/node_modules/`, `docs/build/`, and
`docs/.docusaurus/`.

### 8.2 Configuration

- **Docs-only mode:** `routeBasePath: '/'`, `blog: false`. The intro page
  is the site root; there is no landing page and no `src/pages/index.tsx`.
- **Deployment:** `url: 'https://oleexo.github.io'`, `baseUrl: '/goq/'`,
  `organizationName: 'oleexo'`, `projectName: 'goq'`,
  `trailingSlash: false`.
- **Sidebars:** a single `{type: 'autogenerated', dirName: '.'}` entry, so
  adding a page never requires a config edit. Ordering via
  `sidebar_position` front matter.
- **Search:** deferred, not omitted by oversight. Algolia DocSearch needs
  an application; a local-search plugin is the fallback. Revisit once the
  content exists.
- **Versioned docs and i18n:** out of scope for v1.

### 8.3 CI

`.github/workflows/docs.yml`, all steps with `working-directory: docs`:

- `push` to `main` -> `npm ci`, `npm run build`,
  `actions/upload-pages-artifact@v3`, then `actions/deploy-pages@v4` in a
  `github-pages` environment with `pages: write` and `id-token: write`.
- `pull_request` -> build only, so broken docs or dead internal links fail
  before merge rather than after deploy.
- Docusaurus's `onBrokenLinks: 'throw'` (the default) makes the build the
  link checker; no separate tool needed.

### 8.4 Content

| Page | Purpose |
|---|---|
| `intro.md` | what goq is, why generic methods make it possible, install |
| `getting-started.md` | first query, laziness, terminal operators |
| `operators.md` | all 51 operators, return shapes, C# -> goq mapping |
| `async-and-parallel.md` | `TryQuery`, `ParQuery`, ctx, ordering, `ErrConsumed` |
| `migrating-from-linq.md` | side-by-side C# and Go for common queries |
| `design/generic-methods.md` | the §2 compiler constraints and the satellite-type workaround |
| `design/decisions.md` | ADR record of §3-§6 choices and their reasoning |

**API reference stays in godoc.** The site carries guides, concepts, and
design notes; per-symbol documentation lives in doc comments and is
published by pkg.go.dev. Nothing is duplicated, so the reference cannot
drift from the code, and Go developers find it where they already look.
Every page links `pkg.go.dev/github.com/oleexo/goq`.

`README.md` (pitch, install, 60-second example) and `doc.go` (package
overview) remain, and are the entry points for readers who never visit the
site.

## 9. Risks

1. **The parallel ordered engine is the only genuinely hard component.**
   Everything else is a well-understood loop. Build and test it before any
   operator breadth.
2. **Silent truncation on cancellation** (§5.3) — found once, will recur
   in each concurrent terminal written independently. Mitigate with a
   single shared drain helper rather than per-operator copies.
3. **Generic-method compile times and code size** are unmeasured at this
   operator count. Benchmark; if instantiation cost is significant, that
   belongs in the docs.
4. **`instantiation cycle` may surface in operators not yet enumerated.**
   The satellite-type pattern is the known remedy; each new operator needs
   a compile check.
5. **Documentation code samples can drift from the real API**, and a
   Docusaurus build will not catch it — MDX code fences are never
   compiled. Convention: every non-trivial sample in `docs/docs/` must
   also exist as an `Example*` test in the Go package, which the Go test
   run does compile and execute. The site's build proves links resolve,
   not that code works.
6. **Two build systems in one repo.** Contributors touching only Go must
   never need Node. Keep the docs workflow separate from the Go workflow
   so a docs failure cannot block a Go change, and vice versa.
