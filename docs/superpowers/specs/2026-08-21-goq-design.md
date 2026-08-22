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
- 52 query operators plus 8 source constructors.
- Concurrent sources (channels, ctx cancellation) and a PLINQ-style
  parallel engine.
- **Zero runtime dependencies.** Consumers of the module resolve nothing
  beyond the standard library. Test-only dependencies are permitted (§9.1).

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
| `TryQuery[T]` | `func(ctx) iter.Seq2[T, error]` | `ToSlice(ctx) ([]T, error)`, `First(ctx) (T, bool, error)` |
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

Transitions: `.AsTry()`, `.AsParallel(opts...)`, `.AsSequential()`.

### 3.1.1 The satellite composition law

An operator can be a **method** only if it either preserves the element
type, or introduces a fresh type parameter for the result element. An
operator that derives a new element type from the current one can only be
a method on `Query` itself.

Established empirically; both of these are rejected:

```go
func (g GroupQuery[K,T]) AsQuery() Query[Group[K,T]]     // instantiation cycle
func (g GroupQuery[K,T]) OrderBy[K2 cmp.Ordered](...) OrderedQuery[Group[K,T]]  // cycle
```

`GroupQuery.AsQuery()` cycles because `Query[Group[K,T]]` has `GroupBy`,
which produces `GroupQuery[K2, Group[K,T]]`, which has `AsQuery()`
again — without limit. The second fails for the same reason one step
removed, since `OrderedQuery` itself has `AsQuery()`.

**Therefore:**

- `OrderedQuery[T]` **may** expose `AsQuery() Query[T]` — the element
  type is unchanged, so nothing recurses.
- `GroupQuery[K,T]` and `ChunkQuery[T]` **must not**. Their only exits are
  `Select[R] → Query[R]` (fresh `R`) and non-`Query` terminals such as
  `ToSlice() []Group[K,T]`.
- Satellites **do** carry every element-preserving operator: `Where`,
  `Take`, `Skip`, `Distinct`, `Reverse`, and ordering. Ordering on a
  satellite returns *the same satellite type* with an accumulated
  comparator list, never an `OrderedQuery`. Verified:
  `From(people).GroupBy(dept).OrderBy(size).ThenBy(key).Select(fmt).ToSlice()`
  compiles and yields `[eng:2 hr:1 ops:1]`.
- **Derived-on-derived operations are free functions.** `goq.GroupBy(gq,
  key)` groups an already-grouped query, returning
  `GroupQuery[K2, Group[K,T]]`. Free functions instantiate per call site
  rather than as part of a method set, so they do not cycle. Verified
  working. This is the one place goq is less fluent than C#, which chains
  `GroupBy` after `GroupBy` as an ordinary method.

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
- On `TryQuery` and `ParQuery`, element operators carry both:
  `First(ctx) (T, bool, error)`. The bool is "found", the error is "the
  pipeline failed". Three returns is clumsy, but absence and failure are
  different facts and collapsing them into one error would make the
  reflexive `if err != nil { return err }` silently treat an empty
  result as a failure.
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
| `goq.ErrDuplicateKey` | `ToMap` found two elements with the same key |

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
- **`Window` is therefore a DISPATCH bound in ordered mode, not just a buffer
  bound.** Effective parallelism becomes `min(workers, window)`, so
  `Window(1)` serialises the pipeline regardless of `Workers(n)`. The default
  (`4×workers`) exceeds `workers`, so default throughput is unaffected; only an
  explicitly small window throttles. Both `Window`'s and `AsOrdered`'s godoc
  must say so.
- **The cap is enforced by a producer-side admission gate, not by the sink.**
  The producer blocks before dispatching index `i` until `i < emitted +
  window`, so at most `window` results can ever be outstanding. An earlier
  design merely *checked* the sink's size and errored when exceeded, which
  bounded nothing and — worse — fired during correct execution whenever
  `window` was smaller than the in-flight capacity (`Workers(8)` with
  `Window(1)` can legitimately have ~17 results outstanding). The gate must
  select on `ctx.Done()`, or cancelling a pipeline deadlocks on joining the
  producer.
- Options: `Workers(n)` (default `runtime.GOMAXPROCS(0)`), `Window(n)`
  (default `4×workers`), `AsOrdered()`.
- Cancellation and error both funnel through a derived ctx with `defer
  cancel()`, so workers unwind on every exit path including consumer
  `break`.

**Operator scope is deliberately narrow.** `ParQuery` carries only
element-wise operators — `Select`, `SelectMany`, `Where`, their `...Err`
and `...Ctx` forms — plus the terminals. Ordering, grouping, set
operators, and `Reverse` all require materialising the stream, so they are
not available; reaching them requires an explicit `.AsSequential()`:

```go
res, err := goq.From(urls).AsParallel(goq.Workers(8)).
    SelectErr(httpGet).
    AsSequential().          // the barrier: back to a sequential TryQuery
    ToSlice(ctx)             // materialise, and handle the error here
if err != nil {
    return err
}
ordered := goq.From(res).OrderBy(byLatency).ToSlice()
```

Full PLINQ parity would let one fluent call silently serialise and buffer
an entire pipeline. Making the barrier a named transition keeps the cost
where the reader can see it, and keeps the engine small enough to test
exhaustively.

**Note that `AsSequential()` returns a `TryQuery`, whose surface is also
deliberately narrow** — it carries no ordering, grouping, or set operators
either. So the route to those is through a terminal: materialise with
`ToSlice(ctx)`, handle the error, then re-enter with `From`. An earlier
draft of this spec showed `AsSequential().OrderBy(...)` chained fluently,
which does not compile. The materialising form is arguably the better API
regardless: ordering a fallible stream requires buffering it anyway, and
routing through `ToSlice(ctx)` forces the caller to deal with the error
*before* sorting rather than discovering it afterwards.

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

### 5.4 Panic propagation

A panicking user callback on a worker goroutine would otherwise kill the
process from a goroutine the caller cannot recover, with a stack pointing
into library internals.

Workers therefore `recover`, forward the value and `debug.Stack()` to the
terminal, and the terminal re-panics **on the caller's goroutine** after
cancelling and joining every worker:

```go
// worker
defer func() {
    if r := recover(); r != nil { panicCh <- panicInfo{r, debug.Stack()} }
}()

// terminal, on the caller's goroutine
if p := <-panicCh; p != nil {
    cancel(); wg.Wait()          // no leaks before unwinding
    panic(wrappedPanic{p.val, p.stack})
}
```

A panic stays a panic — it is not reclassified as an `error`, because a
panic signals a bug and an `error` signals an expected condition.
`wrappedPanic` must expose the original value so `recover()` in caller
code can still type-assert it, and print the worker stack alongside the
caller's.

This applies to `ParQuery` only; sequential pipelines panic naturally on
the caller's goroutine already.

## 6. Operator inventory

52 query operators and 8 source constructors. Go-native naming;
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
| Materialize | `ToSlice`, `ToMap[K]`, `ToMapLast[K]`, `ToSet`, `Memoize` |
| Interop | `Seq` |
| Generators | `From`, `FromSeq`, `FromSeqTry`, `FromMap`, `FromChan`, `Range`, `Repeat`, `Empty` |

Return shapes, to remove any ambiguity:

- Elements (`First`, `Last`, `ElementAt`) and extremum aggregations
  (`Min`, `MinBy`, `Max`, `MaxBy`) return `(T, bool)`.
- `Single` returns `(T, error)` — see §4.
- `Average[N]` returns `(float64, bool)` — false on an empty source,
  never a division by zero.
- `Count` returns `int`; `Sum[N]` returns `N` (zero on empty).
- `ToMap[K]` returns `(map[K]T, error)`, yielding `ErrDuplicateKey` when
  two elements share a key — a duplicate means the caller's uniqueness
  assumption is wrong, the same reasoning as `ErrMultiple` on `Single`.
  `ToMapLast[K]` returns a plain `map[K]T` with last-wins overwrite, for
  callers who want that on purpose. `ToMap` is therefore a second
  fallible terminal on `Query[T]`; like `Single`, its error describes the
  data, not a pipeline failure.
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

### 6.1 Streaming vs buffering

Buffering operators must materialise their entire source before yielding
anything. On a bounded source that is only a memory cost; on an unbounded
`FromChan` stream they **never yield at all**. This must be stated in each
operator's godoc, not merely noted here.

| Behaviour | Operators |
|---|---|
| Streaming — O(1) extra memory | `Where`, `Select`, `SelectMany`, `Take`, `TakeWhile`, `Skip`, `SkipWhile`, `Zip`, `Concat`, `Chunk`, `Any`, `All`, `Contains`, `First`, `ElementAt`, `Count`, `Sum`, `Aggregate` |
| Bounded buffer | `TakeLast(n)`, `SkipLast(n)` — retain n elements |
| Full materialisation | `OrderBy`, `ThenBy`, `Reverse`, `GroupBy`, `ToLookup`, `Distinct`, `DistinctBy`, `Union`, `Intersect`, `Except` and their `...By` forms, `Memoize`, `ToSlice`, `ToMap`, `ToSet` |
| Materialises the *argument*, streams the receiver | `Intersect`, `Except`, `Union` build a set from the other sequence |

`Last`, `Single`, `Min`, `Max`, and `Average` stream in O(1) memory but
must reach the end of the source, so they too never return on an
unbounded stream.

## 7. Testing

Stdlib `testing`, plus two test-only dependencies (§9.1):
`github.com/google/go-cmp` for readable diffs and `go.uber.org/goleak`
for leak detection. No testify — table-driven tests do not need an
assertion DSL.

### 7.1 Semantics
Table-driven tests per operator against `internal/seqcore`, each compared
to a naive slice implementation. Empty, single, and all-filtered-out
inputs are mandatory cases for every operator.

### 7.2 Laziness
A counting source asserting exact pull counts.
`From(xs).Select(f).First()` must invoke `f` exactly once. Lazy pipelines
regress silently into eager ones; only pull-count assertions catch it.

### 7.3 Concurrency
All tests under `-race`. Leak detection uses `goleak.VerifyTestMain`,
not a hand-rolled `runtime.NumGoroutine()` comparison — the counter
approach is racy and needs an arbitrary sleep to avoid false positives,
as the design probe demonstrated.

Required cases:
- no leaked goroutines on every concurrent path, including consumer-`break`
  and error short-circuit;
- ctx cancellation mid-flight returns a ctx error — the §5.3 regression;
- error short-circuit stops the producer rather than draining it.

`goleak` may need `IgnoreTopFunction` entries for runtime goroutines on
some platforms; add them narrowly, never a blanket ignore.

### 7.4 Contract tests for the new semantics
- `Single` returns `ErrEmpty` on empty and `ErrMultiple` on two-or-more,
  asserted via `errors.Is`.
- Second terminal call on a `FromChan` query returns `ErrConsumed`; the
  same query after `.Memoize()` returns equal results twice.
- `Seq()` output equals the corresponding `ToSlice()` output for every
  operator, so the two exits cannot diverge.
- `ToMap` returns `ErrDuplicateKey` on collision; `ToMapLast` overwrites.
- A panicking callback in a `ParQuery` worker re-panics on the caller's
  goroutine, the original value survives `recover()`, and no goroutine
  leaks (§5.4). Verified with `goleak` plus an explicit `recover()` in the
  test body.
- Buffering operators (§6.1) are asserted to consume the whole source, and
  streaming operators asserted *not* to — a `Take(1)` over an infinite
  generator must terminate.

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

## 9. Tooling and CI

### 9.1 Dependencies

| Scope | Policy |
|---|---|
| Runtime | none — standard library only |
| Test | `github.com/google/go-cmp`, `go.uber.org/goleak` |
| Docs | Node/npm, confined to `docs/` (§8.1) |

Test dependencies appear in consumers' `go.sum` but are never built or
linked into their binaries. Because `go.mod` now has entries, Dependabot
must watch `gomod` in addition to `github-actions` and the `docs/` npm
tree.

### 9.2 golangci-lint

Config at `.golangci.yml`, schema `version: "2"` — v2 moved formatters
out of `linters` into their own `formatters` section and renamed
`issues.exclude-files` to `linters.exclusions.paths`.

Curated strict set: `errcheck`, `govet` (`enable-all`, minus
`fieldalignment`), `staticcheck`, `unused`, `ineffassign`, `revive`,
`gocritic`, `errorlint`, `unconvert`, `unparam`, `nilerr`, `copyloopvar`,
`wastedassign`, `thelper`, `tparallel`, `misspell`, `predeclared`,
`makezero`, `durationcheck`. Formatters: `gofmt`, `goimports`.

`revive`'s `exported` rule is load-bearing, not decoration: §8.4 makes
godoc the API reference, so a missing doc comment on an exported symbol is
a documentation defect. The linter is what enforces the docs strategy.

**Version constraint.** golangci-lint must be built with Go 1.27 or it
cannot typecheck generic methods. Verified working: 2.13.1 (built with
go1.27.0) type-checks inside generic method bodies and reports no
false positives on the satellite types or `iter.Seq` closures. The
golangci-lint-action docs currently show `v2.12`, which predates Go 1.27 —
do not copy that pin.

### 9.3 Workflows

Four files, deliberately separate so a docs failure cannot block a Go
change:

| File | Trigger | Contents |
|---|---|---|
| `ci.yml` | push `main`, PR | build, vet, `go test -race -shuffle=on -covermode=atomic` |
| `lint.yml` | push `main`, PR | `golangci/golangci-lint-action@v9` |
| `docs.yml` | push `main`, PR | Docusaurus build; deploy on `main` only (§8.3) |
| `dependabot.yml` | schedule | `gomod`, `github-actions`, npm in `/docs` |

Practices applied to all of them:

- least-privilege `permissions: contents: read` at the top level, widened
  per-job only where required (`pages: write` + `id-token: write` for the
  docs deploy, `pull-requests: read` for lint annotations);
- `concurrency` group keyed on workflow and ref with
  `cancel-in-progress: true`, so superseded pushes stop immediately;
- `timeout-minutes` on every job, so a hung test cannot burn an hour;
- `actions/checkout@v6`, `actions/setup-go@v6`, pinned by major version;
  `fetch-depth: 0` for the lint job;
- Go toolchain pinned `1.27.x` — dependency caching comes from
  `setup-go`'s built-in cache, keyed on `go.sum`.

**The test matrix is OS-only:** `ubuntu-latest`, `macos-latest`,
`windows-latest`, with `fail-fast: false`. Generic methods make 1.27 both
the floor and the ceiling, so there is no Go-version axis to vary. The OS
spread is what earns its cost here — it is what catches timing-sensitive
assumptions in the concurrency tests.

`ci.yml` runs `go vet` even though `govet` also runs inside
golangci-lint. That redundancy is intentional: `ci.yml` must be
meaningful on its own if the lint workflow is disabled or fails to install.

Coverage is measured (`-covermode=atomic`) but not uploaded; adding a
coverage service would mean a third-party integration and a repository
secret. Deferred, not overlooked.

### 9.4 License and versioning

- **MIT**, `LICENSE` at the repo root, `Copyright (c) 2026 Oleexo`.
  Without a license file nobody may legally use the module, so this is a
  release blocker rather than paperwork.
- **First release `v0.1.0`.** Go tooling treats `v0` as explicitly
  unstable, so breaking changes need no `/v2` module path. That headroom
  is worth keeping: satellite-type composition (§3.1.1) and the parallel
  engine (§5.2) will meet real usage for the first time, and the language
  behaviour they rest on is only weeks old.
- Tag-only releases; Go needs no publish step. Move to `v1.0.0` once the
  operator surface has survived outside use.

### 9.5 Deferred, on the record

Not oversights — decided against for v1, listed so they are not
rediscovered as gaps:

| Item | Why deferred |
|---|---|
| `ToChan` sink | pipelines cannot yet feed an existing channel graph |
| Docs search (§8.2) | Algolia needs an application; local plugin is the fallback |
| Coverage upload (§9.3) | needs a third-party integration and a repo secret |
| `Cast`/`OfType` | reflection defeats the type system (§1) |
| IQueryable/SQL | no expression trees in Go (§1) |
| Parallel `Aggregate` with combiner | `ParQuery` stays element-wise (§5.2) |

## 10. Risks

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
7. **The toolchain around generic methods is very new.** golangci-lint
   2.13.1 is confirmed working (§9.2), but other tooling a contributor
   might reach for — standalone `staticcheck`, older editor language
   servers, code generators — may not parse method type parameters yet.
   `CONTRIBUTING.md` should state the minimum golangci-lint version and
   that `go build`/`go vet` from Go 1.27 are the authority. Expect to
   raise pinned versions more often than a mature-syntax project would.
