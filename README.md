<p align="center">
  <img src="docs/assets/goxlint-mascot.png" alt="The goxlint mascot, a blue oxpecker that cleans itself with a lint roller" width="200">
</p>

# goxlint

[![CI](https://github.com/egoisutolabs/goxlint/actions/workflows/ci.yml/badge.svg)](https://github.com/egoisutolabs/goxlint/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/egoisutolabs/goxlint.svg)](https://pkg.go.dev/github.com/egoisutolabs/goxlint)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

goxlint is an engine for building your own Go linter. You write the rules that
only your codebase has, in Go, against an API shaped like oxlint's and
ESLint's. goxlint does the rest: loading, the AST walk, configuration, fixes,
suppressions, output, and a test harness.

The rules a team cares about most are usually its own: which package may read
the clock, which logger handlers use, who may call `charge()`. They live in
review comments and in AGENTS.md, and people and coding agents follow them
until they don't. A linter doesn't forget, and it says the same thing every
time.

## A rule

```go
var NoWallClock = &goxlint.Rule{
	Meta: goxlint.Meta{
		Name:       "no-wall-clock",
		Category:   goxlint.Restriction,
		NeedsTypes: true,
		Messages:   map[string]string{"now": "`time.Now` reads the wall clock. Tests cannot control it."},
	},
	Create: func(ctx *goxlint.Context) goxlint.Visitor {
		if ctx.Pkg.Name() == "clock" {
			return nil
		}
		return goxlint.Visit(goxlint.On(func(call *ast.CallExpr) {
			if ctx.IsPkgFunc(call, "time", "Now") {
				ctx.Report(goxlint.Diagnostic{Node: call, MessageID: "now", Help: "Take a clock.Clock and call its Now method."})
			}
		}))
	},
}
```

Your linter is a Go program that lists its plugins:

```go
func main() { cli.Main(goxlint.Plugin{Name: "acme", Rules: []*goxlint.Rule{NoWallClock}}) }
```

```text
  × acme(no-wall-clock): `time.Now` reads the wall clock. Tests cannot control it.
   ╭─[billing/invoice.go:14:39]
13 │ func NewInvoice(total int) Invoice {
14 │     return Invoice{Total: total, Issued: time.Now()}
   ·                                          ──────────
15 │ }
   ╰────
  help: Take a clock.Clock and call its Now method.

Found 0 warnings and 1 error.
```

To see it on your own code, run the example linter:

```sh
go run github.com/egoisutolabs/goxlint/examples/goxlint@latest init
go run github.com/egoisutolabs/goxlint/examples/goxlint@latest ./...
```

Then write your first rule with [Getting started](docs/getting-started.md).

## What goxlint does well

**One walk for every rule.** Rules don't traverse the AST. They register
handlers for the node types they care about, and the engine walks each file
once and dispatches by type. Handlers run only for the node types they asked
for, and a file with no handlers is not walked at all. Adding a rule adds
handlers, not traversals.

**Fixes you can run without reading every diff.** Each fix declares how safe it
is: `safe`, `suggestion`, or `dangerous`, and each level has its own flag.
goxlint applies fixes in passes, so one fix can build on another, up to ten
passes. An edit that overlaps an earlier one waits for the next pass. Every
batch is parsed before the file is written, so a broken fix is rejected, not
saved. A file that was `gofmt`-clean stays `gofmt`-clean, and a file that was
not is left with only the rule's edits. A package that did not load cleanly
gets no fixes at all.

**Suppressions that stay honest.** `//goxlint:ignore <rule> <reason>` needs a
reason. goxlint reports a suppression that no longer suppresses anything, or
that repeats an earlier one. A suppression for a rule that is turned off in
this run is kept, so an exception for an opt-in rule survives the runs that
skip it.

**Failures stay where they happen.** A rule that panics, or reports a position
outside its file, becomes an internal-error diagnostic for that rule, and the
other rules keep reporting. A package that fails to load marks its own files,
and the files of packages that import it, as incomplete. Everything else gets
normal results.

**Rules that see the whole package.** `AnalyzePackage` gets every file of a
package at once, for problems that cross files. A file where the rule is
turned off still counts as evidence. A diagnostic can point at related code in
another file, and each related position is checked against the exact files
that were loaded for this run.

**Positions you can trust.** goxlint reports physical positions and ignores
`//line` directives. SARIF output converts Go's byte columns to Unicode code
points, as the format requires.

**Rules are code, so they get tests.** `goxlinttest` works like ESLint's
`RuleTester`: inline samples that must stay quiet and samples that must report.
Every sample is type-checked, so a case that does not compile fails instead of
passing by accident. `RunPackage` tests cross-file rules, including their
related locations.

**It runs where the code is.**
- Output for people (code frames) and for tools: `unix`, `json`, `github` annotations, and `sarif` for code scanning.
- `--stdin-filename` lints an unsaved buffer. In typed mode the buffer replaces the file on disk through a `go/packages` overlay. With a fix flag, it writes the fixed source to stdout. This is the hook for editors and coding agents.
- The [go/analysis adapter](docs/go-analysis.md) runs the same rules under `go vet -vettool`, `multichecker`, and golangci-lint module plugins.

goxlint depends only on `golang.org/x/tools`, and it lints itself in CI on
Linux, macOS, and Windows.

## When not to use goxlint

- **You want a large set of ready-made checks.** goxlint ships no rule set. The
  plugins in [`examples/goxlint`](examples/goxlint/README.md) show the API, and
  they are not a product. For general Go checks, staticcheck is excellent.
- **You need data-flow analysis.** Rules see the syntax tree and `go/types`, not
  how values move through a function. A rule like "every path closes the rows"
  is hard to write today ([#34](https://github.com/egoisutolabs/goxlint/issues/34)).
- **You want results in your editor as you type.** There is no language server
  yet ([#15](https://github.com/egoisutolabs/goxlint/issues/15)). The pieces it
  needs, such as stdin overlays and related locations, exist.
- **You lint a very large repository on every keystroke.** There is no result
  cache. Each run loads and lints everything again, and in typed mode most of
  the time goes to loading packages. We have no benchmarks yet
  ([#25](https://github.com/egoisutolabs/goxlint/issues/25)), so we make no
  speed claims.
- **You need a stable API today.** goxlint is v0.x. The rule API can still
  change before v1.0.0, and each release says what changed.

## Status

The current release is [v0.1.0](https://github.com/egoisutolabs/goxlint/releases/tag/v0.1.0).
goxlint needs Go 1.26 or later. Before v0.0.2 the module was named `goose-lint`.

[Documentation](docs/README.md) ·
[Discussions](https://github.com/egoisutolabs/goxlint/discussions) ·
[Contributing](CONTRIBUTING.md) ·
[Security](SECURITY.md) ·
[MIT License](LICENSE)
