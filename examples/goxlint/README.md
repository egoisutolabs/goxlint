# Example goxlint linter

This example builds a Go linter from the goxlint engine and two example plugins:

- `plugins/antislop`: opinionated rules for error handling, types, comments, and runtime behavior.
- `plugins/maintainability`: cyclomatic complexity and duplicate-function detection.

`main.go` registers the plugins with `cli.Main`. Both plugins use goxlint's public
rule-authoring API; their namespaces remain `anti-slop` and `maintainability`.

From the repository root:

```sh
go run ./examples/goxlint rules
go run ./examples/goxlint ./...
```

To install the example as a `goxlint` command:

```sh
go install ./examples/goxlint
```

The repository's `.goxlintrc.json` configures self-linting with the example
anti-slop rules. Maintainability rules are opt-in:

```sh
go run ./examples/goxlint -W maintainability/cyclomatic-complexity \
  -W maintainability/no-duplicate-functions ./...
```

The anti-slop rule `no-map-as-struct` is also opt-in. It finds string-keyed
maps that are used as structs:

```sh
go run ./examples/goxlint -W anti-slop/no-map-as-struct ./...
```

See [maintainability rules](maintainability.md) for options and behavior, and
[suppressions](../../docs/suppressions.md) for directive syntax.

To build your own linter, create a command that calls `cli.Main` with your
plugins. The example plugins are implementations to learn from or adapt;
the engine and CLI runner do not depend on them. The
[package-rule guide](../../docs/package-rules.md) covers cross-file analyzers.

The example stays in the repository's Go module, so normal checks include it:

```sh
go test ./...
go test ./examples/goxlint/...
```

The CLI integration tests live in `main_test.go`; each plugin's unit tests live
beside its implementation.
