---
sidebar_position: 2
---

# Getting started

## Install and import

```bash
go get github.com/oleexo/goq
```

```go
import "github.com/oleexo/goq"
```

## Your first query

```go
type Person struct {
    Name string
    Age  int
}

people := []Person{
    {"Ada", 36},
    {"Bob", 17},
    {"Cy", 41},
}

names := goq.From(people).
    Where(func(p Person) bool { return p.Age >= 18 }).
    Select(func(p Person) string { return p.Name }).
    ToSlice()

// names == []string{"Ada", "Cy"}
```

`goq.From` wraps a slice; `Where` and `Select` build a pipeline; `ToSlice`
runs it and collects the result.

## Laziness

Nothing in a goq pipeline runs until a **terminal operator** consumes it.
Building a chain of `Where`/`Select` calls does no work by itself — it only
composes closures. This matters for correctness, not just performance: a
selector that is expensive, or has a side effect, runs exactly as many times
as the terminal actually needs.

```go
calls := 0
expensive := func(n int) int {
    calls++
    return n * n
}

million := make([]int, 1_000_000)
// ... fill million ...

v, ok := goq.From(million).Select(expensive).First()
// calls == 1: First pulls exactly one element, so expensive ran once,
// not a million times.
```

## Terminal operators

A terminal operator is what actually pulls elements from the pipeline. A
non-exhaustive starting set:

| Operator | Shape | Notes |
|---|---|---|
| `ToSlice()` | `[]T` | materialises every element |
| `ToMap(key)` | `(map[K]T, error)` | fails on a duplicate key — see below |
| `First()` | `(T, bool)` | pulls at most one element |
| `Any()` | `bool` | pulls at most one element |
| `Count()` | `int` | enumerates the whole source |
| `Seq()` | `iter.Seq[T]` | exits back into `range`, `slices.Collect`, etc. |

## Absence is `(T, bool)`, not an error

Operators that may or may not find an element — `First`, `Last`, `ElementAt`,
`Min`, `Max`, `MinBy`, `MaxBy` — return the element and a `bool`, never an
`error` and never a panic:

```go
v, ok := q.First()
if !ok {
    // source was empty
}
```

C#'s `FirstOrDefault` is the same call with the bool discarded:

```go
v, _ := q.First() // FirstOrDefault
```

## `Single` is the exception

`Single` is the one element-lookup operator that returns `(T, error)`
instead of `(T, bool)`:

```go
v, err := q.Single()
// err is goq.ErrEmpty   if the source has no elements
// err is goq.ErrMultiple if the source has more than one
```

`Single` exists to *assert* that a source has exactly one element. "There
were zero" and "there were more than one" are both violations of that
assertion, but they are different violations, and a caller catching the
error needs to tell them apart — a `bool` cannot carry that distinction, so
`Single` uses two distinct sentinel errors instead. Both are comparable with
`errors.Is`.

## Buffering operators

Every operator either **streams** — pulling and yielding one element at a
time in O(1) extra memory — or **buffers**, meaning it must consume its
entire source before it can yield anything. On an unbounded source (a
`FromChan` stream that never closes) a buffering operator never yields at
all.

| Behaviour | Operators |
|---|---|
| Streaming — O(1) extra memory | `Where`, `Select`, `SelectMany`, `Take`, `TakeWhile`, `Skip`, `SkipWhile`, `Zip`, `Concat`, `Chunk`, `Any`, `All`, `Contains`, `First`, `ElementAt`, `Count`, `Sum`, `Aggregate` |
| Bounded buffer | `TakeLast(n)`, `SkipLast(n)` — retain `n` elements |
| Full materialisation | `OrderBy`, `ThenBy`, `Reverse`, `GroupBy`, `ToLookup`, `Distinct`, `DistinctBy`, `Union`, `Intersect`, `Except` and their `...By` forms, `Memoize`, `ToSlice`, `ToMap`, `ToSet` |
| Materialises the *argument*, streams the receiver | `Intersect`, `Except`, `Union` build a set from the other sequence first |

`Last`, `Single`, `Min`, `Max`, and `Average` stream in O(1) memory but must
reach the end of the source to answer, so they too never return on an
unbounded stream.

**`OrderBy`, `GroupBy`, `Reverse`, and the set operators never yield on an
unbounded source.** Put one after a `FromChan` pipeline that never closes and
the terminal blocks forever waiting for a value it cannot produce.

## Re-enumeration

A query is re-enumerable if and only if its source is:

- **Slice-backed queries re-execute freely.** `goq.From(xs)` can be run
  through `ToSlice()` (or any terminal) as many times as you like; each call
  walks the slice again.
- **`FromChan` is single-shot.** A channel can only be drained once, so a
  second terminal call on the same `TryQuery` returns `goq.ErrConsumed`
  rather than silently yielding nothing:

  ```go
  q := goq.FromChan(ch)
  a, err := q.ToSlice(ctx) // ok
  b, err := q.ToSlice(ctx) // nil, goq.ErrConsumed
  ```

- **`.Memoize()` opts out.** It caches every element on first enumeration
  and replays them afterwards, making any query — including one over a
  single-shot source — safely re-enumerable. The cost is that every element
  is retained, so it forfeits streaming and must not be used on an unbounded
  source.

See [Async and parallel](./async-and-parallel.md) for how this interacts
with `TryQuery` and `ParQuery`, and the [operator inventory](./operators.md)
for the full return-shape reference.
