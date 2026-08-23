# goq

goq is a lazy, type-safe query pipeline for Go, modelled on LINQ-to-Objects:
`Where`, `Select`, `GroupBy`, `OrderBy`, `Join`, and 56 operators in total,
none of which run until a terminal operator consumes the pipeline.

```go
names := goq.From(people).
    Where(func(p Person) bool { return p.Age >= 18 }).
    Select(func(p Person) string { return p.Name }).
    ToSlice()
```

## Requires Go 1.27

`Select` changes the element type: it takes a `Query[T]` and returns a
`Query[R]`. Before Go 1.27, a *method* could not introduce a new type
parameter, so a fluent, fully-typed `Select` on a generic receiver was not
expressible — every earlier attempt at LINQ in Go had to choose between
inside-out free-function nesting (`Select(Where(q, pred), f)`) or giving up
type safety with `any`. Go 1.27 added method type parameters, and that single
change is what makes goq possible. It will not compile on earlier Go
versions.

## Install

```bash
go get github.com/oleexo/goq
```

## A 60-second example

```go
package main

import (
    "fmt"

    "github.com/oleexo/goq"
)

type Person struct {
    Name string
    Age  int
}

func main() {
    people := []Person{
        {"Ada", 36},
        {"Bob", 17},
        {"Cy", 41},
    }

    adults := goq.From(people).
        Where(func(p Person) bool { return p.Age >= 18 }).
        Select(func(p Person) string { return p.Name }).
        ToSlice()

    fmt.Println(adults) // [Ada Cy]
}
```

Nothing runs until `ToSlice` (or any other terminal operator) consumes the
pipeline: building the chain only composes closures.

## What makes it different

- **Lazy.** Operators build a plan; nothing is pulled from the source until a
  terminal operator (`ToSlice`, `First`, `Count`, ...) runs it.
- **Typed, all the way through.** `Select` can change the element type with
  no `any` and no reflection, because method type parameters make it
  possible to express in Go for the first time.
- **Zero runtime dependencies.** The library imports nothing beyond the
  standard library — asserted by a test, not just claimed in prose.
- **Channel and parallel sources.** `FromChan` gives you a fallible,
  cancellable pipeline over a channel; `AsParallel` runs element-wise
  operators across a bounded worker pool, with optional result ordering.

## Documentation

- Docs site: <https://oleexo.github.io/goq/>
- API reference: <https://pkg.go.dev/github.com/oleexo/goq>
- What is shipped and what is not: [ROADMAP.md](./ROADMAP.md)

## License

MIT. See [LICENSE](./LICENSE).

## Stability

The API is `v0.x` and may change before `v1`.
