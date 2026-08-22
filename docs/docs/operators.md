---
sidebar_position: 3
---

# Operators

goq ships 52 query operators plus 8 source constructors, all Go-native in
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
