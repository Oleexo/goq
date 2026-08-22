---
sidebar_position: 5
---

# Migrating from LINQ

Side-by-side C# and Go for the queries that come up most often. Every Go
sample on this page compiles against the real API (verified as an
`Example*` test in the package).

## Filter and project

```csharp
var names = people
    .Where(p => p.Age >= 18)
    .Select(p => p.Name)
    .ToList();
```

```go
names := goq.From(people).
    Where(func(p Person) bool { return p.Age >= 18 }).
    Select(func(p Person) string { return p.Name }).
    ToSlice()
```

## Ordering with a tie-breaker

```csharp
var sorted = people
    .OrderBy(p => p.Dept)
    .ThenByDescending(p => p.Age)
    .ToList();
```

```go
sorted := goq.From(people).
    OrderBy(func(p Person) string { return p.Dept }).
    ThenByDesc(func(p Person) int { return p.Age }).
    ToSlice()
```

`ThenBy`/`ThenByDesc` are only reachable after `OrderBy`/`OrderByDesc`,
because they exist on `OrderedQuery[T]`, not on `Query[T]` — the same
protection C# gets from `IOrderedEnumerable`, enforced here by the type
system instead of a runtime check.

## Grouping with aggregation

```csharp
var byDept = people
    .GroupBy(p => p.Dept)
    .OrderByDescending(g => g.Count())
    .Select(g => $"{g.Key}:{g.Count()}")
    .ToList();
```

```go
byDept := goq.From(people).
    GroupBy(func(p Person) string { return p.Dept }).
    OrderByDesc(func(g goq.Group[string, Person]) int { return len(g.Items) }).
    Select(func(g goq.Group[string, Person]) string {
        return fmt.Sprintf("%s:%d", g.Key, len(g.Items))
    }).
    ToSlice()
```

`GroupBy` returns `GroupQuery[K,T]`, a distinct type from `Query[T]`, and
`g.Items` stands in for C#'s `IGrouping<TKey,TElement>.Count()`. See
[Design notes](./design/generic-methods.md) for why `GroupQuery` has to be a
separate type at all.

## Join

```csharp
var result = orders
    .Join(people,
        o => o.Customer,
        p => p.Name,
        (o, p) => $"{p.Name}:{o.Total}")
    .ToList();
```

```go
result := goq.From(orders).Join(
    goq.From(people),
    func(o Order) string { return o.Customer },
    func(p Person) string { return p.Name },
    func(o Order, p Person) string { return fmt.Sprintf("%s:%v", p.Name, o.Total) },
).ToSlice()
```

Same shape as C#: outer sequence, inner sequence, outer key selector, inner
key selector, result selector. `GroupJoin` mirrors C#'s left-outer-join
`GroupJoin` the same way.

## `ToDictionary`

```csharp
var byId = orders.ToDictionary(o => o.ID);
```

```go
byID, err := goq.From(orders).ToMap(func(o Order) int { return o.ID })
if err != nil {
    // a duplicate ID means the uniqueness assumption was wrong
}
```

C#'s `ToDictionary` throws `ArgumentException` on a duplicate key; goq
returns `goq.ErrDuplicateKey` instead of throwing. If you want C#'s other
common pattern — last value wins, no exception — use `ToMapLast` instead:

```go
byID := goq.From(orders).ToMapLast(func(o Order) int { return o.ID })
```

## `First` / `Single`

```csharp
var first = people.FirstOrDefault();
var one   = people.Single(p => p.Name == "Ada"); // throws on 0 or 2+ matches
```

```go
first, _ := goq.From(people).First() // discard the bool for FirstOrDefault's behaviour

one, err := goq.From(people).
    Where(func(p Person) bool { return p.Name == "Ada" }).
    Single()
// err is goq.ErrEmpty if none matched, goq.ErrMultiple if more than one did
```

C# throws two different exception *types* for "no match" versus "more than
one match"; goq returns the same `error` type with two different sentinel
values, both comparable with `errors.Is`. See
[Getting started](./getting-started.md#single-is-the-exception) for why the
distinction matters.

## Parallel `Select`

```csharp
var results = urls
    .AsParallel()
    .Select(Fetch)
    .ToList();
```

```go
results, err := goq.From(urls).
    AsParallel().
    SelectErr(Fetch).
    ToSlice(ctx)
```

The Go version takes a `context.Context` at the terminal (`ToSlice(ctx)`)
rather than relying on ambient cancellation, and uses `SelectErr` because
`Fetch` is fallible — see [Async and parallel](./async-and-parallel.md) for
`Workers`, `Window`, `AsOrdered`, and what happens when a callback panics
inside the pool.
