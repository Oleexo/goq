---
sidebar_position: 1
---

# Design notes: generic methods

This page documents compiler behaviour around Go 1.27's generic methods that
is not, as far as we know, written down anywhere else. It is the reason goq
can exist at all, and the reason its API is shaped the way it is — several
types that would be one type in C# (`GroupQuery`, `ChunkQuery`,
`OrderedQuery` instead of a single `IEnumerable<T>`-like type) exist purely
because of the constraints below. Everything here was established
empirically against `go1.27.0`.

## 1. Generic methods work on concrete types

Go 1.27's headline feature for this library: a method can introduce a type
parameter that isn't on its receiver.

```go
func (q Query[T]) Select[R any](f func(T) R) Query[R]  // compiles
```

Before 1.27, this was not legal Go. `Select` needs to hand back a `Query[R]`
for a caller-chosen `R` that has nothing to do with the receiver's `T` — and
a method's type parameters used to have to come entirely from its receiver.
Every earlier attempt at LINQ in Go therefore had to give up either
fluency (nesting free functions: `Select(Where(q, pred), f)`, which reads
inside-out) or type safety (projecting through `any` and asserting on the
way out). Method type parameters remove that trade-off: `q.Select(f)` reads
left-to-right and stays fully typed, because `R` is inferred from `f` at the
call site, independently of `T`.

This one feature is why `Select`, `SelectMany`, `Join`, `Zip`, and every
other operator that changes the element type can be a fluent method instead
of a free function.

## 2. Interfaces cannot declare generic methods

The natural next question is whether goq could expose an `IEnumerable[T]`-
style interface, so third parties could implement their own query source.
Go 1.27 says no:

```go
type Enumerable[T any] interface {
    Select[R any](f func(T) R) Enumerable[R]
}
// interface method must have no type parameters
```

**Consequence:** the pipeline type must be a concrete struct — `Query[T]` —
not an interface. There is no `IEnumerable[T]` analogue in goq, and no way
for another package to implement "something goq can query" by satisfying an
interface, because that interface could never declare `Select` in the first
place.

The extension point is therefore `FromSeq(iter.Seq[T]) Query[T]` on the way
in, and `Seq() iter.Seq[T]` on the way out. Anything that can produce or
consume a standard-library `iter.Seq[T]` can plug into a goq pipeline —
`FromSeq`/`Seq` is the interface, expressed as a pair of concrete
conversions instead of a method set.

## 3. A method whose result element type derives from `T` is an instantiation cycle

The third constraint is the subtle one, and it's what forces goq's satellite
types to exist. Consider two methods that look completely reasonable:

```go
func (q Query[T]) Chunk(n int) Query[[]T]
// instantiation cycle: T instantiated as []T

func (q Query[T]) GroupBy[K comparable](f func(T) K) Query[Group[K,T]]
// instantiation cycle
```

Both are rejected. The reason is that the compiler must be able to fully
resolve every method any generic type could have, at every possible
instantiation, and `Query[T]` having a `Chunk` method that returns
`Query[[]T]` means `Query[[]T]` must *also* have a `Chunk` method — because
it's the same type, `Query`, just instantiated differently — which returns
`Query[[][]T]`, which needs a `Chunk` returning `Query[[][][]T]`, and so on
without a base case. There is no bound on how deep that goes, so the
compiler can't build the method set at all and rejects the whole
declaration up front, at the point where `Chunk` is defined, not at some
particular call site.

The same logic applies to `GroupBy`: a `GroupBy` method on `Query[T]`
returning `Query[Group[K,T]]` would require `Query[Group[K,T]]` to have its
own `GroupBy`, producing `Query[Group[K2,Group[K,T]]]`, forever.

Contrast this with `Select[R]`: `R` is a **fresh** type parameter, supplied
by the caller at the call site and otherwise unconstrained by `T`. It does
not force `Query[R]` to have already been resolved as part of resolving
`Query[T]`'s method set — the compiler doesn't need to expand anything
recursively to know what methods `Query[R]` has, because `R` was never
derived from `T` in the first place. Methods with fresh parameters
(`Select[R]`, `SelectMany[R]`, `Join[U,K,R]`, `Zip[U,R]`) are fine for
exactly this reason.

## The satellite pattern

The fix is for the operator to return a **different, distinct type** whose
method set simply doesn't include the operator that would have caused the
cycle:

```go
type GroupQuery[K comparable, T any] struct{ /* ... */ }

func (q Query[T]) GroupBy[K comparable](key func(T) K) GroupQuery[K, T]
func (g GroupQuery[K, T]) Select[R any](f func(Group[K, T]) R) Query[R] // re-enters Query
```

`GroupQuery[K,T]` is a real, separate generic type from `Query[T]`. It
doesn't have a `GroupBy` method that returns another `GroupQuery`, so
nothing recurses — instantiation terminates, and the chain stays fully
fluent:

```go
goq.From(people).Where(...).GroupBy(...).Where(...).Select(...).ToSlice()
```

This compiles and runs. In goq, `Chunk` returns `ChunkQuery[T]` for the
identical reason `GroupBy` returns `GroupQuery[K,T]`.

**The rule this implies:** a method may only either *preserve* the
receiver's element type, or introduce a *fresh* type parameter for the
result's element type. An operator whose result element type is *derived*
from the receiver's own element type (`[]T`, `Group[K,T]`) can only ever be
a method on `Query` itself, where the target of the method call is a
brand-new instantiation chosen by the *caller*, not implied by a method
declaration that has to hold for every instantiation of the receiver.

This buys correctness as a side effect, not just working code:
`OrderedQuery[T]` is the only type exposing `ThenBy`, so
`goq.From(xs).ThenBy(...)` (without a preceding `OrderBy`) is a compile
error — the same protection C#'s `IOrderedEnumerable` gives at the interface
level, but enforced here by which concrete type a value happens to have.

## Why `OrderedQuery` gets `AsQuery()` but `GroupQuery`/`ChunkQuery` don't

`OrderedQuery[T].AsQuery() Query[T]` is safe: sorting doesn't change the
element type, so `Query[T]` on the way out is the *same* `T` that came in.
Nothing new gets instantiated that wasn't already part of the caller's
graph.

```go
func (g GroupQuery[K,T]) AsQuery() Query[Group[K,T]] // instantiation cycle
```

is not safe, and the compiler rejects it for the same reason as `Chunk`
above: `Query[Group[K,T]]` has a `GroupBy` method (because every `Query[X]`
does), which would produce `GroupQuery[K2, Group[K,T]]`, whose own
hypothetical `AsQuery()` would need `Query[Group[K2,Group[K,T]]]` — the
cycle again, just one layer removed. The same reasoning rules out an
`AsQuery()` on `ChunkQuery[T]`: `Query[[]T]` has `Chunk`, so
`Query[[]T].AsQuery()` would recreate the original problem.

`GroupQuery` and `ChunkQuery` therefore have exactly two ways out: `Select`,
which introduces a fresh element type and re-enters `Query`, or a non-`Query`
terminal such as `ToSlice() []Group[K,T]`. `OrderedQuery` additionally gets
`AsQuery()` because, uniquely among the three, it never changed the element
type in the first place.

## Why ordering a `GroupQuery` stays a `GroupQuery`

```go
func (g GroupQuery[K, T]) OrderBy[K2 cmp.Ordered](key func(Group[K, T]) K2) GroupQuery[K, T]
```

`OrderBy` on `GroupQuery` returns another `GroupQuery[K,T]` — with the sort
key accumulated internally, ready for `ThenBy` — rather than an
`OrderedQuery[Group[K,T]]`. Returning `OrderedQuery[Group[K,T]]` would
reintroduce exactly the cycle `GroupQuery` exists to avoid: `OrderedQuery`
has `AsQuery()`, which would produce `Query[Group[K,T]]`, which has
`GroupBy`, and we're back to a `Query` of groups of groups without limit.
Keeping the result as `GroupQuery[K,T]` means ordering, then grouping-typed
operators, then more ordering, all stay inside a method set that was
already proven to terminate.

Verified working: chaining ordering and a tie-breaker on a grouped pipeline
compiles and runs —

```go
goq.From(people).
    GroupBy(func(p Person) string { return p.Dept }).
    OrderBy(func(g goq.Group[string, Person]) int { return len(g.Items) }).
    ThenBy(func(g goq.Group[string, Person]) string { return g.Key }).
    Select(func(g goq.Group[string, Person]) string {
        return fmt.Sprintf("%s:%d", g.Key, len(g.Items))
    }).
    ToSlice()
// []string{"eng:2", "hr:1", "ops:1"}
```

## Why nested grouping is a free function

C# chains `GroupBy` after `GroupBy` as an ordinary method on
`IEnumerable<IGrouping<...>>`. goq cannot offer that as a method on
`GroupQuery`, for the reason above — but it can offer it as a **free
function**:

```go
func GroupBy[K comparable, T any, K2 comparable](
    g GroupQuery[K, T], key func(Group[K, T]) K2,
) GroupQuery[K2, Group[K, T]]
```

A free function's type parameters are resolved **per call site**, not as
part of a generic type's method set that has to be valid for every possible
instantiation of that type. Calling `goq.GroupBy(gq, key)` instantiates
exactly one concrete `GroupQuery[K2, Group[K,T]]` for that call, and stops —
there's no obligation for the compiler to have already resolved what methods
*that* type has before it can finish typechecking the call, the way there
is for a method declared once on the generic type itself. This is the one
place goq is deliberately less fluent than C#: nested grouping needs
`goq.GroupBy(gq, key)` instead of `gq.GroupBy(key)`.

## Why `comparable`-element operators are functions, not methods

`Distinct`, `Union`, `Intersect`, `Except`, `Contains`, `SequenceEqual`, and
`ToSet` all need their element type to satisfy `comparable`. None of them
are methods on `Query[T]`:

```go
func Distinct[T comparable](q Query[T]) Query[T]
func Contains[T comparable](q Query[T], v T) bool
```

The reason is structural, not a matter of taste: **a method cannot add a
constraint to its receiver's own type parameter.** `Query[T]` is declared
with `T any`. A method can't retroactively say "well, for *this* method,
`T` must additionally be `comparable`" — the receiver's type parameter list
is fixed by the type declaration, once, for every method on that type. A
free function, by contrast, declares its own type parameter list
independent of any receiver, so `Distinct[T comparable]` is free to require
`comparable` for exactly the operators that need it, while `Query[T any]`
stays usable for element types that aren't comparable (structs with slice
fields, for instance) everywhere else.

The `...By[K]` siblings (`DistinctBy`, `UnionBy`, `IntersectBy`, `ExceptBy`)
get to be ordinary methods because they push the constraint onto a *fresh*
type parameter — the key, `K comparable` — rather than onto the receiver's
existing `T`. That fresh parameter is exactly the kind method type
parameters in Go 1.27 support, per §1 above.

## Summary

| Question | Answer |
|---|---|
| Can a method introduce a new type parameter? | Yes — this is what makes `Select` possible. |
| Can an interface declare a generic method? | No — `interface method must have no type parameters`. No `IEnumerable[T]`; `FromSeq`/`Seq` is the extension point instead. |
| Can a method's result element type derive from the receiver's? | No — instantiation cycle. Resolved with satellite types (`GroupQuery`, `ChunkQuery`). |
| Can a method add a constraint to its own receiver's type parameter? | No. Resolved with free functions (`Distinct`, `Sum`, ...) or a fresh key parameter (`DistinctBy`, ...). |

See [Design notes: decisions](./decisions.md) for the rest of the
architecture's reasoning, and [Operators](../operators.md) for where each
rule shows up in the actual API surface.
