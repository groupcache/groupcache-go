# Contributing

## Development checks

`make ci` runs `go mod tidy`, golangci-lint and the tests.

### Tiger

[Tiger](https://github.com/kapetan-io/tiger) is a static analyzer that requires
bounded loops, goroutines someone waits for, cancellable waits and similar
rules. It runs on your machine, not in CI.

Install it:

```
go install github.com/kapetan-io/tiger/cmd/tiger@latest
```

Run it over every package:

```
make tiger
```

Tiger exits 0 when clean, 1 when it has findings and 2 when it could not check
the packages. `make` reports any failure as its own exit 2, so read the line
`make tiger` prints to tell the two apart.

The repository still has findings, so Tiger is advisory for now.

`make tiger` pins `GOTOOLCHAIN` to the Go version in `go.mod`. Tiger reads the
Go that runs it, and under a newer local Go every package fails to load with
`export data version 5 is greater than maximum supported version 4`.

### Run Tiger before each push

A `pre-push` hook in `.githooks/` runs `make tiger` before `git push`. It prints
the findings and lets the push go. It skips Tiger, with the install command, when
Tiger is not installed. Turn it on once per clone, which is optional:

```
git config core.hooksPath .githooks
```
