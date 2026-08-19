# prim

Shared primitives (see README.md for the list and the rules).

- Each package is stdlib-only and imports no sibling package.
- No protocol constants or types here; those belong to the
  consuming protocol repo.
- Every package carries known-answer / round-trip tests; the
  tests are the contract.
- Build and test: `go build ./... && go test ./...`.
