---
sidebar_position: 4
---

# Async and parallel

`Query[T]` is infallible and synchronous. Two other pipeline types cover
everything else: `TryQuery[T]` for fallible or asynchronous sources, and
`ParQuery[T]` for a bounded worker pool.

## `TryQuery`: what makes a pipeline fallible

A pipeline becomes a `TryQuery[T]` the moment something in it can fail or
block on a context:

- `q.SelectErr(f)` / `q.WhereErr(pred)` on a `Query[T]` — a fallible
  projection or predicate that returns `(R, error)` / `(bool, error)`.
- `goq.FromChan(ch)` — a source reading from a channel.
- `q.AsTry()` — an explicit lift, for reaching `TryQuery`-only operators
  without a fallible callback.

`TryQuery` deliberately serves **two** cases with one engine: a fallible
*synchronous* pipeline (parsing strings, validating rows) and an asynchronous
*streaming* pipeline (`FromChan`). From the point of view of the operators
in between, those differ only in the source — a slow channel producer versus
an instantly-ready slice — and both are properties of the source and the
terminal, not of the chain. A separate `AsyncQuery` type would have left
fallible-sync chains with nowhere to live, and every operator would have
needed writing twice.

## ctx goes to the terminal, not the source

```go
q := goq.FromChan(ch) // no context here

ctx1, cancel1 := context.WithTimeout(context.Background(), time.Second)
defer cancel1()
a, err := q.ToSlice(ctx1) // context supplied at the call that actually blocks
```

`TryQuery` and `ParQuery` hold a *plan* — `func(context.Context) iter.Seq2[T,
error]` — rather than a built iterator. The context is supplied to whichever
terminal operator actually blocks (`ToSlice(ctx)`, `First(ctx)`, ...), which
means one pipeline value can be run under different contexts, and matches
the normal Go convention of passing `ctx` as the first argument to the call
that does the blocking rather than capturing it upstream.

## Error semantics

- **First error wins.** The pipeline short-circuits: no further elements are
  pulled, upstream producers are cancelled through a derived context, and
  every goroutine unwinds before the terminal returns.
- **Errors propagate verbatim**, never wrapped, so `errors.Is` and
  `errors.As` work against your own error types straight through the
  pipeline.
- **Terminals discard partial results on error.** `ToSlice(ctx)` returns
  `(nil, err)`, not a truncated slice, so a partial result can never be
  mistaken for a complete one.
- Context cancellation surfaces as `context.Canceled` or
  `context.DeadlineExceeded` from the terminal, same as any error.

## `ErrConsumed` and `Memoize`

A channel can only be drained once. `FromChan` therefore returns a
**single-shot** `TryQuery`: a second terminal call returns `goq.ErrConsumed`
rather than silently yielding nothing.

```go
q := goq.FromChan(ch)
a, err := q.ToSlice(ctx) // ok
b, err := q.ToSlice(ctx) // nil, goq.ErrConsumed
```

`.Memoize()` caches the elements (and the terminal error) on first
enumeration and replays both afterwards, making the result re-enumerable —
it never returns `ErrConsumed`, even over an originally single-shot source.
The tradeoff is the same as on `Query`: every element is retained, so it
must not be used on an unbounded source.

**`FromSeqTry` exists for the same reason.** `FromSeq` returns a plain
`Query[T]`, whose terminals have no `error` return — so if you hand it a
single-shot `iter.Seq[T]` (one that reads from a channel or a network
stream), a second enumeration silently yields nothing, with no way to report
it. `FromSeqTry` returns a `TryQuery[T]` instead, which *can* report
`ErrConsumed` the same way `FromChan` does. Prefer it whenever the iterator
you're wrapping cannot be safely enumerated twice.

## `ParQuery`: the worker pool

`q.AsParallel(opts...)` runs subsequent element-wise operators — `Select`,
`SelectErr`, `SelectCtx`, `SelectMany`, `Where` — across a bounded pool of
workers:

```go
out, err := goq.From(xs).
    AsParallel(goq.Workers(4), goq.Window(2)).
    AsOrdered().
    Select(func(x int) int { return x * x }).
    ToSlice(ctx)
```

- **`Workers(n)`** sets the pool size (default `runtime.GOMAXPROCS(0)`).
- **`Window(n)`** bounds how many out-of-order results the ordered sink may
  buffer (default `4×Workers`). It only matters with `AsOrdered`.
- **`AsOrdered()`** makes results arrive in source order instead of
  completion order. It costs latency and memory: one slow element delays
  every later one.

**`Window` is a dispatch bound, not just a buffer bound, in ordered mode.**
The producer will not dispatch an item more than `Window` positions ahead of
the one the sink is still waiting on, so effective parallelism becomes
`min(Workers, Window)`. The default window (`4×Workers`) exceeds `Workers`,
so it never throttles by default — but an explicitly small window does, all
the way down to full serialisation: `Window(1)` with `AsOrdered()`
serialises the pipeline regardless of how many workers you asked for.

### Operator scope is deliberately narrow

`ParQuery` only carries element-wise operators, plus terminals (`ToSlice`,
`Count`, `ForEach`). Ordering, grouping, and set operators all require
materialising the whole stream first, so they are not methods on `ParQuery`
at all — reaching them means leaving the pool explicitly with
`AsSequential()`:

```go
fetched, err := goq.From(urls).
    AsParallel(goq.Workers(8)).
    SelectErr(fetch).
    AsSequential().        // the barrier: back to a TryQuery, out of the pool
    ToSlice(ctx)
if err != nil {
    return err
}
sorted := goq.From(fetched).
    OrderBy(func(s string) string { return s }).
    ToSlice()
```

`AsSequential()` returns a `TryQuery[T]`, whose own method set is likewise
element-wise-only; ordering and grouping are back on `Query[T]`, reached by
materialising with a terminal first, as above. Full PLINQ parity would let
one fluent call silently serialise and buffer the entire pipeline behind the
scenes — making the barrier a named, visible transition keeps that cost
where a reader sees it.

## Panics

**A panicking callback surfaces as a `goq.PanicValue` panic on the caller's
goroutine**, not the worker's. If a `Select`/`Where`/... callback running on
a `ParQuery` worker panics, the worker recovers it, and the terminal
operator — after cancelling and joining every other worker, so nothing leaks
— re-panics on the goroutine that called the terminal, carrying the original
value and the worker's stack trace:

```go
defer func() {
    if r := recover(); r != nil {
        pv, ok := r.(goq.PanicValue)
        if !ok {
            panic(r) // not one of ours
        }
        log.Printf("callback panicked: %v\n%s", pv.Value, pv.Stack)
    }
}()

_, _ = goq.From(xs).AsParallel().Select(riskyProjection).ToSlice(ctx)
```

Callers who `recover()` **must type-assert `goq.PanicValue`** and read
`.Value` to reach the original panic argument — a bare `recover()` gets the
`PanicValue` wrapper, not the string or error the callback actually panicked
with. This is deliberate: a panic reports a bug in your callback, and an
`error` reports an expected condition, so the engine never reclassifies one
as the other. This applies to `ParQuery` only — a sequential pipeline panics
naturally on the caller's own goroutine already, with no wrapping needed.

## See also

- [Operators](./operators.md) for the full inventory and return shapes.
- [Design notes: generic methods](./design/generic-methods.md) for why
  `ParQuery`'s narrow operator scope is a compiler constraint as much as a
  design choice.
