# Roadmap

`goq` is at **v0.1.0**. The API is `v0.x` and may change — Go tooling treats
that as explicitly unstable, so breaking changes need no `/v2` module path.
That headroom is deliberate: the satellite-type composition and the parallel
engine will meet real usage for the first time.

Everything under [Not done](#not-done) is **additive**. A v0.2.0 can add all of
it without breaking a v0.1.0 caller.

---

## Shipped in v0.1.0

**56 query operators and 8 source constructors**, zero runtime dependencies,
90.9% test coverage across the module.

### Pipeline types

| Type | Purpose | Surface |
|---|---|---|
| `Query[T]` | lazy, infallible, over `iter.Seq[T]` | 49 methods |
| `TryQuery[T]` | fallible and context-aware; holds a *plan*, so the context arrives at the terminal | 19 methods |
| `ParQuery[T]` | bounded worker pool; holds a *builder*, so `AsOrdered()` can be written after the operator it applies to | 10 methods |
| `OrderedQuery[T]` | satellite; makes `ThenBy` reachable only after `OrderBy` | 6 methods |
| `GroupQuery[K,T]` | satellite; groups, orderable, no `AsQuery` by construction | 9 methods |
| `ChunkQuery[T]` | satellite; streaming batches, no `AsQuery` by construction | 5 methods |

Plus 22 package-level functions — the operators that need a `comparable` or
`cmp.Ordered` element type, which a method can never require of its own
receiver.

### Operator families

- **Restriction / projection** — `Where`, `Select`, `SelectIndex`, `SelectMany`,
  `SelectManySeq`, `Zip`
- **Partitioning** — `Take`, `TakeWhile`, `TakeLast`, `Skip`, `SkipWhile`,
  `SkipLast`, `Chunk`
- **Ordering** — `OrderBy`, `OrderByDesc`, `OrderByFunc`, `ThenBy`,
  `ThenByDesc`, `Reverse`
- **Grouping** — `GroupBy`, `GroupBySelect`, `ToLookup`, and a package-level
  `GroupBy` for grouping an already-grouped pipeline
- **Joins** — `Join` (inner), `GroupJoin` (left outer)
- **Sets** — `Distinct`, `DistinctBy`, `Concat`, `Union`, `UnionBy`,
  `Intersect`, `IntersectBy`, `Except`, `ExceptBy`
- **Aggregation** — `Aggregate`, `Count`, `Sum`, `Min`, `MinBy`, `Max`,
  `MaxBy`, `Average`
- **Elements / quantifiers** — `First`, `Last`, `Single`, `ElementAt`, `Any`,
  `AnyWhere`, `All`, `Contains`, `SequenceEqual`
- **Materialisation** — `ToSlice`, `ToMap`, `ToMapLast`, `ToSet`, `Memoize`,
  `Seq`
- **Sources** — `From`, `FromSeq`, `FromSeqTry`, `FromMap`, `FromChan`,
  `Range`, `Repeat`, `Empty`

### Semantics

- **Lazy throughout.** Nothing runs until a terminal consumes the pipeline.
  Every operator's godoc states whether it streams, buffers a bounded window,
  retains a growing key set, or fully materialises — because that is what tells
  a caller what is safe on an unbounded source.
- **Absence is `(T, bool)`, not an error.** `Single` is the deliberate
  exception, returning `ErrEmpty` or `ErrMultiple`, because it exists to assert
  uniqueness and "more than one" must be distinguishable from "none".
- **Four sentinel errors** — `ErrEmpty`, `ErrMultiple`, `ErrConsumed`,
  `ErrDuplicateKey`. Callback errors pass through verbatim, so `errors.Is` and
  `errors.As` work against your own types.
- **Terminals discard partial results** on error. A truncated slice can never
  be mistaken for a complete one.
- **Single-shot sources report `ErrConsumed`** on a second enumeration rather
  than silently yielding nothing, and the guard survives arbitrary operator
  chains including `AsParallel`.

### Parallel engine

Bounded worker pool with an unbuffered input channel, so a slow consumer
backpressures the producer. Unordered by default; `AsOrdered()` reassembles in
source order behind a producer-side admission gate that bounds outstanding work
to `Window`. Four invariants, each with a test:

1. No leaked goroutines on any exit path — completion, consumer break, callback
   error, cancellation, worker panic, producer panic.
2. Cancellation surfaces as an error, never as a truncated success.
3. Panics are recovered and re-raised as `goq.PanicValue` **on the caller's
   goroutine**, after every worker is joined — a panic stays a panic rather
   than being laundered into an error.
4. Backpressure via the unbuffered input.

### Tooling

`ci.yml` (build, vet, `-race -shuffle=on` across ubuntu/macos/windows),
`lint.yml` (golangci-lint 2.13.1, which must be built with Go 1.27 to typecheck
generic methods), `docs.yml` (Docusaurus build on PRs, deploy on `main`), and
Dependabot. Seven documentation pages, including a design note on the Go 1.27
compiler constraints that shaped the API.

Two tests assert properties that would otherwise drift silently: one proves the
module has zero non-stdlib runtime imports, the other cross-checks the design
spec's operator inventory against the real API in both directions.

---

## Not done

### 1. Missing fallible and parallel operators — highest priority

The design spec states a rule that was only partly implemented: `Select`,
`SelectMany`, `Where`, `Aggregate`, and the terminals should each gain `...Err`
and `...Ctx` forms on `TryQuery` and `ParQuery`. Only `Select` follows it.

| Missing | Notes |
|---|---|
| `ParQuery.WhereErr` / `WhereCtx` | **The most obviously missing operator in the library.** A fallible parallel filter — parallel validation, a parallel HEAD check — is a core use case, and the spec names both verbatim. |
| `SelectManyErr` | Named explicitly in the spec's error model; does not exist. |
| `SelectManyCtx`, `WhereCtx`, `AggregateErr`, `AggregateCtx` | The rest of the rule. |
| `ParQuery.First` | The spec gives `ParQuery`'s terminal shape as "as `TryQuery`". Callers currently route through `AsSequential()`. |
| `ParQuery.Seq(ctx)` | Promised on both fallible types; present only on `TryQuery`. |

These were dropped when the implementation plan was written, and the deviation
was not recorded at the time — so they are a plan defect rather than a
considered omission.

### 2. `TryQuery`'s operator set needs a stated rule

`ParQuery`'s narrowness is principled and documented: ordering, grouping, and
set operators all require materialising the stream, so reaching them means an
explicit `AsSequential()` and the barrier stays visible.

`TryQuery`'s narrowness has no such rule. `Any`, `All`, `ElementAt`, `ToMap`,
and `SkipWhile` are all absent although each is O(1)-streaming and would work
unchanged, and `TakeWhile` is present while `SkipWhile` is not. The subset
reads as accreted rather than chosen. Either widen it or document why it stops
where it does.

### 3. No benchmarks

The design spec required benchmarks against hand-written loops, so the
abstraction's cost would be recorded rather than assumed. The requirement was
dropped at planning time and never reached the deferred register.

Consequences:

- The spec's own risk about generic-method compile time and code size cannot be
  evaluated.
- `ParQuery.Where` boxes each element through `any` to avoid an instantiation
  cycle, costing one allocation per element. Threading a keep-flag through the
  engine's stage signature as `(R, bool, error)` would remove the boxing and the
  type assertion together — but that refactor should be decided *with* a
  measurement, not before one.

### 4. Satellite operator breadth

The composition law is respected everywhere: `AsQuery` exists only on
`OrderedQuery`, and every satellite `Select[R]` returns a full `Query[R]`. But
the promised breadth was not built — none of the three satellites carries
`Take`, `Skip`, `Distinct`, or `Reverse`, and they differ from each other:

| | `Where` | ordering | `Count` | `AsQuery` |
|---|---|---|---|---|
| `OrderedQuery` | — | ✓ | — | ✓ |
| `GroupQuery` | ✓ | ✓ | ✓ | (correctly absent) |
| `ChunkQuery` | ✓ | — | ✓ | (correctly absent) |

`OrderedQuery` escapes via `AsQuery`. The other two escape via the identity
projection `.Select(func(x T) T { return x })`, which returns a full `Query[T]`.
That is the general satellite escape hatch and it should be documented as such.

### 5. `internal/seqcore` shares less than the architecture claims

The design said operator logic would live once in `internal/seqcore`, with all
three pipeline types as thin adapters. As built, `seqcore` holds 11 functions
over `iter.Seq`, used by two files; the fallible pipeline implements its own
stages and the parallel engine shares nothing.

The feared explosion never arrived, because `TryQuery` and `ParQuery` carry
much smaller surfaces than `Query` — the duplication is a handful of loops, not
fifty. But the drift it guarded against is already latent: `seqcore.Take` and
`TryQuery.Take` are independent implementations with independently chosen
error-versus-index ordering. Consolidate them, or accept the duplication
explicitly and test both.

Related: the spec asked for table-driven tests per operator directly against
`seqcore`. Those do not exist; the functions are covered transitively through
the fluent surface (69–100% each, measured with `-coverpkg`). Not a coverage
hole, but it forfeits the "semantics tested independent of the fluent surface"
property.

### 6. Deliberately deferred

Decided against for v1, listed so they are not rediscovered as gaps:

| Item | Why |
|---|---|
| `ToChan` sink | A pipeline cannot yet feed an existing channel graph. Deliberately left out of scope; easy to add. |
| Parallel `Aggregate` with a combiner | `ParQuery` stays element-wise. A genuinely parallel reduction needs an explicit associative merge function — one more concept a caller must get right. |
| `Cast` / `OfType` | Reflection defeats the type system the library exists for. |
| IQueryable / SQL translation | Go has no expression trees, so this would need a separate builder DSL — effectively a second library. |
| Docs search | Algolia DocSearch needs an application; a local-search plugin is the fallback. |
| Coverage upload | Needs a third-party integration and a repository secret. |

---

## Before the first publish

Neither of these is code work, and both are blocking in practice:

- **Enable GitHub Pages** — repository settings, Source → "GitHub Actions".
  The docs workflow's first deploy fails without it.
- **CI has never run.** There is no remote, so everything has only been
  verified locally on `darwin/arm64`. The Windows and Linux matrix legs are
  untested, and the concurrency tests are the most timing-sensitive part of the
  suite — they are the likeliest thing to behave differently on another
  platform.

---

## Suggested order

1. **`ParQuery.WhereErr` / `WhereCtx`** — smallest change with the largest
   practical payoff.
2. **Benchmarks** — cheap, and everything in §3 depends on having them.
3. **The rest of the `...Err` / `...Ctx` rule**, plus `ParQuery.First` and
   `Seq(ctx)` — mechanical once the first one establishes the pattern.
4. **State `TryQuery`'s rule**, widening the set or documenting the boundary.
5. **`seqcore` consolidation** — the only item that is a refactor rather than
   an addition, and the only one worth doing before the API stabilises.

---

Design rationale for every decision here — including the compiler constraints
that shaped the API and a record of the decisions taken during implementation —
is in `docs/superpowers/`.
