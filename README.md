<p align="center">
  <img src="docs/assets/goxlint-mascot.png" alt="The goxlint mascot, a blue oxpecker that cleans itself with a lint roller" width="200">
</p>

<h1 align="center">goxlint</h1>

<p align="center"><b>Your codebase's rules, enforced on every run. For your team and for your agents.</b></p>

<p align="center">
  <a href="https://github.com/egoisutolabs/goxlint/actions/workflows/ci.yml"><img src="https://github.com/egoisutolabs/goxlint/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://pkg.go.dev/github.com/egoisutolabs/goxlint"><img src="https://pkg.go.dev/badge/github.com/egoisutolabs/goxlint.svg" alt="Go Reference"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License: MIT"></a>
</p>

You have rules no linter knows. *Don't read the wall clock outside `package clock`.*
*Log through the request logger.* *Never call `charge()` outside `billing/`.*
Today they live in a wiki, a review comment, an AGENTS.md that your coding agent
reads once and then forgets.

**goxlint turns them into a linter.** Write each rule in a few lines of Go. Get
a real tool back: configuration, fixes, suppressions with reasons, a test
harness, CI annotations, and output an agent can act on.

## Twenty lines of Go

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

```go
func main() { cli.Main(goxlint.Plugin{Name: "acme", Rules: []*goxlint.Rule{NoWallClock}}) }
```

## What everyone sees

```text
  × acme(no-wall-clock): `time.Now` reads the wall clock. Tests cannot control it.
   ╭─[billing/invoice.go:13:39]
12 │ func NewInvoice(total int) Invoice {
13 │     return Invoice{Total: total, Issued: time.Now()}
   ·                                          ──────────
14 │ }
   ╰────
  help: Take a clock.Clock and call its Now method.
```

The same words, every time, for a new hire, a reviewer, and a coding agent.
Exit code `1` until it is fixed.

## Try it today

See goxlint on your own repository in two commands, with the example rules:

```sh
go run github.com/egoisutolabs/goxlint/examples/goxlint@latest init
go run github.com/egoisutolabs/goxlint/examples/goxlint@latest ./...
```

Then write your own first rule with [Getting started](docs/getting-started.md).

## Everything around the rule, done

- **Fixes** at three safety levels. `--fix` applies only the safe ones and keeps your files `gofmt`-clean.
- **Suppressions that need a reason**, and a report when one stops suppressing anything.
- **A rule test harness** (`goxlinttest`) in the style of ESLint's `RuleTester`, for file and cross-file rules.
- **Cross-file package rules**, with diagnostics that point to the related code.
- **Output for every reader**: code frames, `json`, `github` annotations, `sarif` for code scanning.
- **Agent hooks**: `--stdin-filename` lints an unsaved buffer, and with `--fix` returns the fixed source.
- **Runs where you already lint**: the same rules work in `go vet` and golangci-lint through the [go/analysis adapter](docs/go-analysis.md).
- **One dependency**: `golang.org/x/tools`. goxlint lints itself in CI on Linux, macOS, and Windows.

## Status

v0.x, moving fast. The next release is v0.1.0. Go 1.26 or later.

[Docs](docs/README.md) · [Discussions](https://github.com/egoisutolabs/goxlint/discussions) · [Contributing](CONTRIBUTING.md) · [Security](SECURITY.md) · [MIT License](LICENSE)
