---
sidebar_position: 1
slug: /
---

# goq

goq is a lazy, type-safe query pipeline for Go, modelled on LINQ-to-Objects.

```go
names := goq.From(people).
    Where(func(p Person) bool { return p.Age >= 18 }).
    Select(func(p Person) string { return p.Name }).
    ToSlice()
```

## Why this could not exist before Go 1.27

`Select` changes the element type: it takes a `Query[T]` and returns a
`Query[R]`. Until Go 1.27, a *method* could not introduce the new type
parameter `R`, so every previous attempt at LINQ in Go had to choose between
free-function nesting — `Select(Where(q, pred), f)`, which reads inside-out —
and giving up type safety with `any`.

Go 1.27 permits type parameters on methods. That single change is what makes a
fluent, fully typed pipeline possible, and it is why goq requires Go 1.27 and
will not compile on anything earlier. See [Design notes](./design/generic-methods.md)
for the compiler constraints this uncovered.

## Install

```bash
go get github.com/oleexo/goq
```

## What is here

- **[Getting started](./getting-started.md)** — your first query, and what lazy
  evaluation means in practice.
- **[Operators](./operators.md)** — all 52 operators, their return shapes, and
  the C# name for each.
- **[Async and parallel](./async-and-parallel.md)** — channel sources,
  cancellation, and the worker pool.
- **[Migrating from LINQ](./migrating-from-linq.md)** — side-by-side C# and Go.
- **[Design notes](./design/generic-methods.md)** — the compiler constraints
  that shaped the API, and why some operators are functions rather than methods.

Per-symbol API documentation lives in godoc, at
[pkg.go.dev/github.com/oleexo/goq](https://pkg.go.dev/github.com/oleexo/goq).
It is not duplicated here, so it cannot drift from the code.
