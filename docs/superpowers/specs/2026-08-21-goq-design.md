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
- 50 query operators plus 7 source constructors.
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
this, 50 operators across 3 engines is 150 hand-written implementations.
`seqcore` is also where semantics are unit-tested, independent of the
fluent surface.

### 3.4 Re-enumeration

A query is re-enumerable if and only if its source is. Slice-backed
queries re-execute on each enumeration; channel-backed queries are
single-shot and yield nothing on a second pass. This matches C#, which has
the identical hazard for `IEnumerable` over a network stream. `.Memoize()`
opts into caching, making any query re-enumerable at the cost of
retaining elements. This must be documented prominently on `FromChan`.

## 4. Error model

- `Query[T]` is infallible. Selectors are pure `func(T) R`.
- Absence is `(T, bool)`, never a panic and never an `error`:
  `First`, `Last`, `Single`, `ElementAt`, `Min`, `Max`. C#'s
  `...OrDefault` twins collapse into the discarded bool — `v, _ :=
  q.First()` is `FirstOrDefault()`.
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

50 query operators and 7 source constructors. Go-native naming;
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
| Generators | `From`, `FromSeq`, `FromMap`, `FromChan`, `Range`, `Repeat`, `Empty` |

Return shapes, to remove any ambiguity:

- Elements (`First`, `Last`, `Single`, `ElementAt`) and extremum
  aggregations (`Min`, `MinBy`, `Max`, `MaxBy`) return `(T, bool)`.
- `Average[N]` returns `(float64, bool)` — false on an empty source,
  never a division by zero.
- `Count` returns `int`; `Sum[N]` returns `N` (zero on empty).
- `Single` returns `(T, false)` when the source holds zero *or* more than
  one element; the bool does not distinguish those cases. C# throws
  differently for each; we do not.

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

### 7.4 Ordered-parallel equivalence
Fuzz with randomized per-element work durations, asserting
`AsParallel().AsOrdered()` output is identical to the sequential result.

### 7.5 Examples and benchmarks
`Example*` tests that compile, doubling as godoc. Benchmarks against
hand-written loops so the abstraction's allocation cost is recorded rather
than assumed.

## 8. Documentation deliverables

- `README.md` — pitch, install, 60-second example.
- `doc.go` — package overview with runnable examples.
- `docs/getting-started.md`
- `docs/operators.md` — full reference plus C# → goq mapping.
- `docs/async-and-parallel.md` — `TryQuery`, `ParQuery`, ctx, ordering,
  and the re-enumeration hazard.
- `docs/migrating-from-linq.md`
- `docs/design/go127-generic-methods.md` — the §2 constraints and the
  satellite-type workaround. Undocumented territory; likely the most
  broadly useful artifact here.
- `docs/design/decisions.md` — ADR record of §3–§6 choices.

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
