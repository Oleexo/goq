---
sidebar_position: 3
---

# Operators

goq ships 56 query operators plus 8 source constructors, all Go-native in
naming (`ToSlice` rather than `ToArray`, `OrderByDesc` rather than
`OrderByDescending`). This page is the full inventory. Per-symbol
documentation — parameters, exact behaviour, edge cases — lives in godoc at
[pkg.go.dev/github.com/oleexo/goq](https://pkg.go.dev/github.com/oleexo/goq);
this page exists to help you find the operator you want and know its shape
before you read the doc comment.

## Inventory

| Family | Operators |
|---|---|
| Restriction | `Where` |
| Projection | `Select[R]`, `SelectIndex[R]`, `SelectMany[R]`, `SelectManySeq[R]`, `Zip[U,R]` |
| Partitioning | `Take`, `TakeWhile`, `TakeLast`, `Skip`, `SkipWhile`, `SkipLast`, `Chunk` |
| Ordering | `OrderBy[K]`, `OrderByDesc[K]`, `OrderByFunc`, `ThenBy[K]`, `ThenByDesc[K]`, `Reverse` |
| Grouping | `GroupBy[K]`, `GroupBySelect[K,R]`, `ToLookup[K]` |
| Joins | `Join[U,K,R]`, `GroupJoin[U,K,R]` |
| Sets | `Distinct`, `DistinctBy[K]`, `Concat`, `Union`, `UnionBy[K]`, `Intersect`, `IntersectBy[K]`, `Except`, `ExceptBy[K]` |
| Aggregation | `Aggregate[A]`, `Count`, `Sum[N]`, `Min`, `MinBy[K]`, `Max`, `MaxBy[K]`, `Average[N]` |
| Elements | `First`, `Last`, `Single`, `ElementAt` |
| Quantifiers | `Any`, `AnyWhere`, `All`, `Contains`, `SequenceEqual` |
| Materialize | `ToSlice`, `ToMap[K]`, `ToMapLast[K]`, `ToSet`, `Memoize` |
| Interop | `Seq` |
| Generators | `From`, `FromSeq`, `FromSeqTry`, `FromMap`, `FromChan`, `Range`, `Repeat`, `Empty` |

`ForEach` is deliberately not in this table — see
[ForEach: the side-effecting terminal](#foreach-the-side-effecting-terminal)
below for why.

### Four names worth a closer look

Four operators above aren't self-explanatory from their name alone:

- **`SelectIndex[R](f func(int, T) R) Query[R]`** is `Select` with a
  zero-based element index passed alongside each element — reach for it
  when the projection needs to know an element's position, not just its
  value (numbering rows, alternating behaviour by parity, and so on).
- **`SelectManySeq[R](f func(T) iter.Seq[R]) Query[R]`** is `SelectMany`
  for callers whose inner sequence is itself lazy or unbounded. `SelectMany`
  takes `func(T) []R` — a slice, which must already exist in full. Reach for
  `SelectManySeq` instead when building that slice up front would be
  wasteful (a large or expensive-to-materialise expansion per element) or
  impossible (the inner sequence doesn't terminate).
- **`OrderByFunc(cmpFn func(a, b T) int) OrderedQuery[T]`** sorts by an
  explicit three-way comparison instead of a `cmp.Ordered` key. Reach for
  it when an element's ordering isn't expressible as a single sortable key
  at all — a case-insensitive string comparison, a comparison that needs to
  look at two fields together as one decision rather than as `OrderBy` plus
  a `ThenBy` tie-breaker, or any comparison whose logic doesn't reduce to
  "compute a key, then compare keys."
- **`AnyWhere(pred func(T) bool) bool`** is `Any` with a predicate baked in,
  equivalent to `Where(pred).Any()` but without building the intermediate
  `Query`. It is what the C# → goq mapping table below lists as the
  equivalent of `Any(predicate)`.

## Return shapes

Get these right and most of the API reads itself:

- **Elements** (`First`, `Last`, `ElementAt`) and **extremum aggregations**
  (`Min`, `MinBy`, `Max`, `MaxBy`) return `(T, bool)` — the bool is "found".
- **`Single`** returns `(T, error)` — `goq.ErrEmpty` on zero elements,
  `goq.ErrMultiple` on more than one. See
  [Getting started](./getting-started.md#single-is-the-exception) for why it
  alone breaks the `(T, bool)` pattern.
- **`Average[N]`** returns `(float64, bool)` — `false` on an empty source,
  never a division by zero.
- **`Count`** returns `int`; **`Sum[N]`** returns `N` (the zero value on an
  empty source; integer overflow wraps rather than erroring).
- **`ToMap[K]`** returns `(map[K]T, error)`, yielding `goq.ErrDuplicateKey`
  when two elements produce the same key — a collision means the caller's
  uniqueness assumption was wrong, the same reasoning as `ErrMultiple` on
  `Single`. **`ToMapLast[K]`** returns a plain `map[K]T`, with later elements
  overwriting earlier ones on collision, for callers who want that on
  purpose.
- **`Seq()`** exposes the underlying iterator: `iter.Seq[T]` on `Query`,
  `iter.Seq2[T, error]` on `TryQuery` (as `Seq(ctx)`). This is the exit back
  into stdlib iteration (`range`, `slices.Collect`) and, together with
  `FromSeq`, the library's only extension point — see
  [Design notes](./design/generic-methods.md) for why there is no
  implementable interface instead.
- On `TryQuery` and `ParQuery`, element operators carry both facts:
  `First(ctx) (T, bool, error)`. The bool is "found", the error is "the
  pipeline failed" — collapsing them into one error would make a reflexive
  `if err != nil { return err }` treat an empty result as a failure. See
  [Async and parallel](./async-and-parallel.md).

Set operators (`Distinct`, `Union`, `Intersect`, `Except`, `Contains`,
`SequenceEqual`, `ToSet`) require `comparable` elements; the `...By[K]`
variants lift that requirement onto a `comparable` *key* instead, so they
work on struct elements.

The `...Err` / `...Ctx` variants (`SelectErr`, `WhereErr`, `SelectCtx`, ...)
are not counted as separate operators in the inventory above — they are the
same operator with a fallible or context-aware callback, available on
`TryQuery` and `ParQuery`.

## Streaming vs. buffering

See [Getting started](./getting-started.md#buffering-operators) for the full
table and the warning about unbounded sources. In short: `OrderBy`,
`GroupBy`, `Reverse`, and the set operators all fully materialise their
source before yielding anything, so none of them terminate on an
unbounded `FromChan` stream that never closes.

## ForEach: the side-effecting terminal

`ForEach(ctx, fn func(T) error) error` is not in the inventory table above
and is not counted in the "56 query operators" figure, because unlike the
`...Err`/`...Ctx` variants it isn't a fallible or context-aware form of an
existing `Query` operator — it has no `Query[T]` equivalent at all. It only
exists on **`TryQuery`** and **`ParQuery`**, as the terminal for
side-effecting work: it calls `fn` for each element, stopping at the first
error from either the pipeline or `fn`.

```go
err := goq.From(xs).AsTry().ForEach(ctx, func(x int) error {
    return process(x)
})
```

On `ParQuery`, `fn` runs on the caller's goroutine and is never invoked
concurrently, even though elements complete out of order across the
worker pool — so `fn` itself never needs its own synchronization. See
[Async and parallel](./async-and-parallel.md) for the rest of the
`TryQuery`/`ParQuery` surface.

## Functions, not methods

A handful of operators are **package-level functions rather than methods**:
`Distinct`, `Union`, `Intersect`, `Except`, `Contains`, `SequenceEqual`,
`ToSet`, `Sum`, `Average`, `Min`, `Max`, and `GroupBy` when grouping an
already-grouped query (`goq.GroupBy(groupQuery, key)`).

The one-sentence reason: **a method cannot add a constraint to its
receiver's own type parameter**, and a method whose result element type
*derives* from the receiver's is an instantiation cycle that the Go
compiler rejects outright. `Distinct` needs `T` to be `comparable`, which
`Query[T]`'s method set cannot demand of an arbitrary `T`; nested `GroupBy`
would need `Query[Group[K,T]]` to have its own `GroupBy`, which needs
`Query[Group[K2,Group[K,T]]]`, without limit. Full explanation, with the
exact compiler errors, in [Design notes: generic methods](./design/generic-methods.md).

Each of these functions has a method sibling for the common case where the
constraint is already satisfiable: `DistinctBy`, `UnionBy`, `IntersectBy`,
`ExceptBy` (comparable *key* instead of element), `AnyWhere`/`Contains` via
`Where(pred).Any()`, `Sum[N]`/`Average[N]`/`MinBy`/`MaxBy` with an explicit
selector.

## C# → goq mapping

| C# | goq |
|---|---|
| `ToArray()` / `ToList()` | `ToSlice()` |
| `ToDictionary(keySelector)` | `ToMap(key)` (errors on duplicate keys) / `ToMapLast(key)` (overwrites) |
| `OrderByDescending` | `OrderByDesc` |
| `ThenByDescending` | `ThenByDesc` |
| `FirstOrDefault()` | `v, _ := q.First()` — same call, discard the bool |
| `Single()` (throws `InvalidOperationException`, two different messages) | `Single()` returning `(T, error)` with `goq.ErrEmpty` / `goq.ErrMultiple` |
| `Count(predicate)` | `Where(pred).Count()` |
| `Any(predicate)` | `AnyWhere(pred)` |
| `Cast<T>()` / `OfType<T>()` | not provided — reflection-based casting would defeat the type system goq exists for |
| `AsEnumerable()` | not needed — use `Seq()` to exit into a plain `iter.Seq[T]` |
| `Sum(x => x.Price)` | `q.Sum(func(x T) N { return x.Price })` (method form); `goq.Sum(q)` for a bare numeric `Query[N]` |
| `GroupBy(keySelector)` | `GroupBy(key)`, returning `GroupQuery[K,T]` — see [Design notes](./design/generic-methods.md) |
| `Join` / `GroupJoin` | `Join[U,K,R]` / `GroupJoin[U,K,R]`, same shape |
| `AsParallel()` | `AsParallel(opts...)` → `ParQuery[T]`, see [Async and parallel](./async-and-parallel.md) |

## Further reading

- [Async and parallel](./async-and-parallel.md) for `TryQuery` and `ParQuery`
  specifics.
- [Design notes: generic methods](./design/generic-methods.md) for why the
  API is shaped the way it is.
