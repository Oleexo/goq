---
sidebar_position: 2
---

# Design notes: decisions

An ADR-style record of the architecture decisions behind goq — one entry per
choice, each stating the decision, the alternatives that were considered,
and why the alternatives lost. Sourced from the design spec's sections 3
through 6 and 9. See [Generic methods](./generic-methods.md) for the
compiler constraints several of these decisions exist to work around.

## Architecture (spec §3)

### ADR-1: Six pipeline types instead of one

**Decision.** The public surface is `Query[T]`, `TryQuery[T]`, `ParQuery[T]`,
and three satellites — `OrderedQuery[T]`, `GroupQuery[K,T]`, `ChunkQuery[T]`
— rather than a single `IEnumerable`-like type.

**Alternatives.** One type covering every case, the way C#'s
`IEnumerable<T>` does; or one type per concern (sync/async/parallel) with no
satellites.

**Why.** A single type is impossible: interfaces can't declare generic
methods, and a method that changes `Query[T]`'s own element type in a way
that derives from `T` is an instantiation cycle (see
[Generic methods](./generic-methods.md)). The satellites exist purely to
give operators like `GroupBy` and `Chunk` a method set that terminates.

### ADR-2: `TryQuery` covers both fallible-sync and async-streaming

**Decision.** One `TryQuery[T]` type serves a fallible synchronous pipeline
(`q.SelectErr(parseInt)`) and an asynchronous streaming pipeline
(`FromChan(ch)`).

**Alternatives.** A separate `AsyncQuery[T]` type for channel-backed,
context-aware pipelines.

**Why.** From the operators' point of view, "async" only changes the source
and the terminal — a slow concurrent producer instead of an
instantly-ready slice — not the shape of `Where`/`Select` in between. A
separate `AsyncQuery` would have left fallible-synchronous chains with
nowhere to live, and doubled the operator implementations for no semantic
gain.

### ADR-3: The satellite composition law

**Decision.** An operator may be a method only if it preserves the element
type or introduces a fresh type parameter for the result. Anything that
derives a new element type from the current one is a method on `Query`
only, or a free function.

**Alternatives.** Trying to special-case individual operators as needed.

**Why.** This is the general rule that makes every specific satellite-type
choice predictable rather than ad hoc — see
[Generic methods](./generic-methods.md) for the full derivation and the
exact compiler errors.

### ADR-4: Pipelines hold a plan, not a built iterator

**Decision.** `TryQuery` and `ParQuery` hold `func(context.Context)
iter.Seq2[T, error]` rather than a context bound at construction time.

**Alternatives.** Capture `ctx` when the source is created (e.g. inside
`FromChan`), matching how some other Go async wrappers work.

**Why.** Context should arrive at the call that actually blocks, per Go
convention, not be captured upstream — and holding a plan lets one pipeline
value be executed under different contexts (a retry with a fresh deadline,
for instance) without rebuilding the chain.

### ADR-5: Operator logic lives once, in `internal/seqcore`

**Decision.** All operator semantics are implemented once, as free
functions over `iter.Seq`/`iter.Seq2` in `internal/seqcore`; the public
pipeline types are thin adapters over it.

**Alternatives.** Implement each operator directly on each of the three
engines.

**Why.** ~50 operators across three engines would otherwise be ~150
hand-written implementations. A shared core also lets semantics be unit
tested independently of the fluent surface.

### ADR-6: Re-enumeration follows the source; `FromSeqTry` closes a gap `FromSeq` cannot

**Decision.** A query re-executes freely if its source does (slices);
single-shot sources (`FromChan`) track consumption and return
`ErrConsumed` on a second terminal call; `Memoize()` opts a query into
caching so it becomes re-enumerable regardless.

**Alternatives.** Silently allow every source to be re-enumerated (wrong for
channels), or make every `Query` terminal return an error so `FromSeq` could
report `ErrConsumed` too.

**Why.** Silent re-enumeration of a drained channel would look like an
empty result, indistinguishable from "there was nothing to begin with" —
worth reporting instead. Giving every `Query` terminal an error return would
defeat the `(T, bool)` design (see ADR-7), so the guarantee only reaches
`TryQuery`/`ParQuery`; `FromSeq`'s godoc states the re-enumerability
requirement explicitly, and `FromSeqTry` exists as the fallible alternative
for callers who can't guarantee it.

## Error model (spec §4)

### ADR-7: `Query[T]` is infallible; absence is `(T, bool)`

**Decision.** `Query[T]` selectors are pure `func(T) R`, and terminals like
`First`/`Last`/`Min`/`Max` report absence as a second `bool` return, never a
panic or an `error`.

**Alternatives.** Return `(T, error)` uniformly, the way many Go APIs do;
or panic on an empty source, mirroring some LINQ methods' exception
behaviour in C#.

**Why.** "No element found" is not a failure — it's expected on an empty
source — so encoding it as an `error` would invite the reflexive
`if err != nil { return err }` to treat a legitimate empty result as
broken. `(T, bool)` makes C#'s `...OrDefault` twins collapse into a
discarded bool: `v, _ := q.First()`.

### ADR-8: `Single` is the one exception, with two sentinel errors

**Decision.** `Single()` returns `(T, error)`: `goq.ErrEmpty` or
`goq.ErrMultiple`, both comparable with `errors.Is`.

**Alternatives.** `(T, bool)` like the other element operators; or two
distinct panic/exception types, mirroring C#.

**Why.** `Single` exists specifically to *assert* uniqueness. "Zero
matches" and "more than one match" are both violations of that assertion,
but different ones, and a caller needs to tell them apart — a single `bool`
can't carry that distinction. This describes the cardinality of the result
set, not a pipeline failure, so it doesn't contradict `Query[T]` being
infallible.

### ADR-9: First error wins; terminals discard partial results

**Decision.** On `TryQuery`/`ParQuery`, the first error short-circuits the
whole pipeline; collecting terminals return `(nil, err)` rather than
whatever was gathered so far.

**Alternatives.** Best-effort collection, returning both the partial slice
and the error.

**Why.** A truncated result returned alongside an error invites callers to
use the slice and ignore the error. Discarding it makes "check the error"
the only path to any result at all.

### ADR-10: A small set of sentinel errors, all comparable with `errors.Is`

**Decision.** `ErrEmpty`, `ErrMultiple`, `ErrConsumed`, `ErrDuplicateKey` are
the only sentinels goq defines; callback errors are returned verbatim,
never wrapped.

**Alternatives.** Wrap callback errors in a goq-specific error type carrying
additional context.

**Why.** Wrapping would break `errors.Is`/`errors.As` against the caller's
own error types for no benefit goq is positioned to provide; the sentinels
that do exist each describe a distinct fact about the data or the source,
not the pipeline mechanics.

## Concurrency engines (spec §5)

### ADR-11: `FromChan` stays sequential over a concurrent source

**Decision.** `FromChan` selects on `ctx.Done()` and the channel, but
operators downstream of it remain sequential — one element at a time.

**Alternatives.** Fan out consumption of the channel itself.

**Why.** Parallel *consumption* is `ParQuery`'s job; `FromChan`'s job is
just to be a source that can report cancellation and single-shot exhaustion
correctly. Conflating the two would have made a simple source type carry
worker-pool concerns.

### ADR-12: Parallel engine unordered by default; `Window` is a dispatch bound in ordered mode

**Decision.** `ParQuery` yields results in completion order unless
`AsOrdered()` is used. In ordered mode, a producer-side admission gate
prevents dispatching item `i` more than `Window` positions ahead of the
next one the sink is waiting to emit — making effective parallelism
`min(Workers, Window)`.

**Alternatives.** Order by default (simpler mental model, matches source
order intuitively); bound the sink's buffer by checking its size after the
fact rather than gating dispatch.

**Why.** Unordered is strictly faster, so it's the default; ordering is
opt-in for callers who need it and are willing to pay for it. The
size-check alternative was tried and rejected: it bounds nothing (a
`Workers(8)`/`Window(1)` pipeline can legitimately have ~17 results
outstanding under that scheme) and fires spuriously during otherwise
correct execution. The gate must itself select on `ctx.Done()`, or
cancelling a pipeline deadlocks joining the producer.

### ADR-13: `ParQuery`'s operator scope is deliberately narrow; `AsSequential()` is a visible barrier

**Decision.** `ParQuery` carries only element-wise operators (`Select`,
`SelectMany`, `Where`, their `...Err`/`...Ctx` forms) plus terminals.
Ordering, grouping, and set operators require `.AsSequential()` first.

**Alternatives.** Full PLINQ parity, where any operator can appear after
`AsParallel()` and the engine silently serialises and buffers as needed.

**Why.** Silent serialisation hides a real cost — materialising the whole
stream — behind a fluent call that looks free. Making `AsSequential()` an
explicit, named transition keeps that cost visible to the reader and keeps
the parallel engine small enough to test exhaustively.

### ADR-14: Every concurrent terminal re-checks `ctx` after its drain loop

**Decision.** Concurrent terminals check `ctx.Err()` again after draining
workers, even though the drain loop itself also watches `ctx.Done()`.

**Alternatives.** Trust that a `ctx.Done()` case inside the drain loop is
sufficient.

**Why.** It isn't: when `ctx` fires mid-drain, a worker can exit via
`case <-ctx.Done(): return` without reporting anything, the output channel
then closes normally, and the terminal returns a short slice with a `nil`
error — silent truncation presented as success. This was found as a real
bug, not a hypothetical, and needs a dedicated regression test per
pipeline.

### ADR-15: Panics re-panic as `goq.PanicValue` on the caller's goroutine

**Decision.** A worker that recovers a panic forwards the value and a stack
trace to the terminal, which cancels and joins every other worker, then
re-panics with a `PanicValue` on the caller's own goroutine.

**Alternatives.** Convert the panic into an `error` and return it normally;
let the process crash, as an unrecovered panic on a worker goroutine
otherwise would.

**Why.** A panic signals a bug in the caller's callback; an `error` signals
an expected condition. Reclassifying one as the other would hide real bugs
behind ordinary error-handling. Letting the process crash from an internal
worker goroutine the caller never sees would make ordinary `recover()` in
the caller's own code useless against it. Re-panicking on the caller's
goroutine, with the original value reachable via a type assertion, gives
the caller the same `recover()` semantics they'd get from a synchronous
pipeline.

## Operator inventory (spec §6)

### ADR-16: Go-native naming over literal C# names

**Decision.** Operators use Go-idiomatic names (`ToSlice`, `OrderByDesc`)
rather than the C# LINQ names verbatim (`ToList`/`ToArray`,
`OrderByDescending`), with a mapping table in
[Operators](../operators.md) for anyone coming from C#.

**Alternatives.** Match C#'s names exactly, easing the transition for
LINQ users at the cost of un-Go-like names (`ToArray` returning a slice,
for instance).

**Why.** goq is a Go library first; its names should read naturally in Go
code, not as a translation layer. The mapping table gets LINQ users to the
right name without forcing every reader to live with C# naming forever.

### ADR-17: `...Err`/`...Ctx` variants aren't separate inventory entries

**Decision.** `SelectErr`, `WhereErr`, `SelectCtx`, and similar variants are
documented as the fallible/context-aware form of their base operator, not
counted as additional entries in the 52-operator inventory.

**Alternatives.** List every variant as its own operator.

**Why.** They're the same operator with a different callback signature,
available wherever the base operator can host a fallible or
context-consuming callback. Counting them separately would inflate the
inventory without adding a new concept to learn.

## Tooling and CI (spec §9)

### ADR-18: Zero runtime dependencies; two test-only exceptions

**Decision.** goq has no runtime dependencies at all. `go.mod` carries
exactly two test-only dependencies: `github.com/google/go-cmp` and
`go.uber.org/goleak`.

**Alternatives.** Adopt a general-purpose assertion library (e.g. testify)
for more expressive test failures.

**Why.** Consumers of the module should resolve nothing beyond the standard
library — that guarantee is part of the pitch. Test-only dependencies
appear in a consumer's `go.sum` but are never built or linked into their
binaries, so they don't compromise it. Table-driven tests don't need an
assertion DSL on top of stdlib `testing`.

### ADR-19: golangci-lint pinned to a version built with Go 1.27

**Decision.** CI requires a golangci-lint build compiled with Go 1.27, and
`.golangci.yml` uses the v2 schema (`formatters` split out of `linters`,
`linters.exclusions.paths` instead of `issues.exclude-files`).

**Alternatives.** Use whatever golangci-lint version the
`golangci-lint-action` documentation shows by default.

**Why.** A linter built with an older Go toolchain cannot typecheck method
type parameters at all, so it would either crash or silently skip every
generic method in the codebase. The tooling around a language feature this
new lags the feature itself; the version pin has to be verified, not
assumed, and re-checked more often than in a mature-syntax project.

### ADR-20: Four separate CI workflows

**Decision.** `ci.yml`, `lint.yml`, `docs.yml`, and `dependabot.yml` are
separate files rather than one combined workflow.

**Alternatives.** A single workflow with multiple jobs.

**Why.** A documentation build failure should never block a Go change
reaching CI green, and vice versa — the two build systems (Go, Node) in one
repo shouldn't be coupled at the workflow level just because they live in
the same repository.

### ADR-21: OS-only test matrix, no Go-version axis

**Decision.** `ci.yml` runs on `ubuntu-latest`, `macos-latest`, and
`windows-latest`, with a single pinned Go version — no matrix across Go
versions.

**Alternatives.** Test against multiple recent Go versions, as many
libraries do to track a support window.

**Why.** Generic methods make Go 1.27 both the floor and the ceiling: the
library cannot compile on anything earlier, and there is no meaningful
older version to test against. The OS spread is what actually earns its
cost here, catching timing-sensitive assumptions in the concurrency tests
that a single-OS matrix would miss.

### ADR-22: MIT license, `v0.1.0` first release, tag-only releases

**Decision.** MIT license at the repository root; the first tagged release
is `v0.1.0`, not `v1.0.0`; releases are tag-only with no separate publish
step.

**Alternatives.** Start at `v1.0.0` to signal production-readiness
immediately.

**Why.** Go's module system treats `v0` as explicitly unstable, so breaking
changes don't require a `/v2` module path — headroom worth keeping while
the satellite-type composition pattern and the parallel engine meet real
usage for the first time, on language behaviour that is itself only weeks
old. `v1.0.0` is the right signal once the operator surface has survived
outside use, not before.

## Deferred, on the record (spec §9.5)

These were considered and deliberately deferred for v1 — not oversights,
and not expected to be "discovered" as gaps later:

| Item | Why deferred |
|---|---|
| `ToChan` sink | Pipelines cannot yet feed an existing channel graph; no consumer need identified yet. |
| Docs search (Algolia DocSearch / local plugin) | Algolia DocSearch needs an application process; a local-search plugin is the fallback once there's enough content to make search useful. |
| Coverage upload | Coverage is measured (`-covermode=atomic`) but not uploaded — a coverage service would mean a third-party integration and a repository secret for a v1 that doesn't need it yet. |
| `Cast`/`OfType` | Reflection-based element casting would defeat the type system that motivates the whole library. |
| `IQueryable`/SQL translation | Go has no expression-tree representation of a closure, so translating a goq pipeline to SQL would require a separate builder DSL entirely — out of scope, not a missing feature. |
| Parallel `Aggregate` with a combiner | `ParQuery` stays deliberately element-wise (ADR-13); a parallel reduction needs a combiner function and its own correctness argument that hasn't been built yet. |
