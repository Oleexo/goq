# Contributing to goq

## Toolchain floors

- **Go 1.27.0 or later.** goq uses method type parameters; earlier releases
  cannot compile it.
- **golangci-lint 2.13.1 or later.** It must be built with Go 1.27 or it cannot
  typecheck generic methods. Check with `golangci-lint --version` — the "built
  with" field must read `go1.27` or newer. Older builds report spurious
  `typecheck` failures on every file.

`go build ./...` and `go vet ./...` from Go 1.27 are the authority. Other tools
may not yet parse method type parameters.

## Running things

    go test ./...                  # all tests
    go test -race -shuffle=on ./... # what CI runs
    golangci-lint run ./...        # lint
    golangci-lint fmt ./...        # apply formatters

## Conventions

- Every exported symbol needs a doc comment; the linter enforces it.
- Operator logic belongs in `internal/seqcore` as a free function over
  `iter.Seq`. Pipeline methods should be thin adapters.
- If you hit `instantiation cycle`, read the satellite composition law in
  `docs/superpowers/specs/2026-08-21-goq-design.md` §3.1.1 before changing
  anything.
