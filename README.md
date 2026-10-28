<p align="center">
  <img src="docs/assets/goxlint-mascot.png" alt="The goxlint mascot, a blue oxpecker that cleans itself with a lint roller" width="200">
</p>

# goxlint

[![CI](https://github.com/egoisutolabs/goxlint/actions/workflows/ci.yml/badge.svg)](https://github.com/egoisutolabs/goxlint/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/egoisutolabs/goxlint.svg)](https://pkg.go.dev/github.com/egoisutolabs/goxlint)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Every codebase has rules that no linter ships with. Nothing reads the wall
clock outside `package clock`. Handlers log through the request logger, never
`fmt.Println`. Error messages do not start with "failed to". You wrote these
down in CONTRIBUTING.md, and lately in AGENTS.md too, and then you waited to
see who would remember.

People forget. Coding agents forget faster: an instruction in a prompt is
advice, and advice loses to whatever pattern the model saw most often. The
thing that does not forget is a tool that runs on every save and every pull
request and says, in the same words each time, exactly what is wrong, where,
and what to do instead.

goxlint is an engine for building that tool. You write rules in Go against an
API shaped like oxlint's and ESLint's. goxlint loads your code, runs all the
rules in one shared walk of each file, and handles everything around them:
configuration, severities, suppression comments, fixes, output formats, a test
harness. Your linter is a `main` package of three lines.

## A rule

Here is the clock rule from the first paragraph, complete.

```go
package rules

import (
	"go/ast"

	"github.com/egoisutolabs/goxlint"
)

// NoWallClock reports calls to time.Now outside package clock. Everything
// else takes a clock.Clock, so tests can control time.
var NoWallClock = &goxlint.Rule{
	Meta: goxlint.Meta{
		Name:        "no-wall-clock",
		Description: "Disallow time.Now outside package clock.",
		Category:    goxlint.Restriction,
		NeedsTypes:  true,
		Messages: map[string]string{
			"now": "`time.Now` reads the wall clock. Tests cannot control it.",
		},
	},
	Create: func(ctx *goxlint.Context) goxlint.Visitor {
		if ctx.Pkg.Name() == "clock" {
			return nil
		}
		return goxlint.Visit(
			goxlint.On(func(call *ast.CallExpr) {
				if ctx.IsPkgFunc(call, "time", "Now") {
					ctx.Report(goxlint.Diagnostic{
						Node:      call,
						MessageID: "now",
						Help:      "Take a clock.Clock and call its Now method.",
					})
				}
			}),
		)
	},
}
```

`On` registers a handler for one AST node type, and the engine calls it for
every call expression in the file. `NeedsTypes` asks for `go/types`
information, so `IsPkgFunc` sees through renamed imports and local shadowing.
Leave `NeedsTypes` off and the rule runs on the syntax tree alone, without
type-checking anything.

Add goxlint to your module with `go get github.com/egoisutolabs/goxlint`. Put
the rule in a plugin, `goxlint.Plugin{Name: "acme", Rules: []*goxlint.Rule{NoWallClock}}`,
hand the plugin to `cli.Main`, and the result is your linter:

```go
package main

import (
	"github.com/egoisutolabs/goxlint/cli"

	"example.com/app/tools/lint/rules"
)

func main() { cli.Main(rules.Plugin) }
```

Run `go run ./tools/lint ./...` and this is what the person, or the agent,
sees:

```text
  × acme(no-wall-clock): `time.Now` reads the wall clock. Tests cannot control it.
   ╭─[billing/invoice.go:13:39]
12 │ func NewInvoice(total int) Invoice {
13 │     return Invoice{Total: total, Issued: time.Now()}
   ·                                          ──────────
14 │ }
   ╰────
  help: Take a clock.Clock and call its Now method.

Found 0 warnings and 1 error.
Finished in 157ms on 6 files with 1 rule using 8 threads (typed mode).
```

The message says what is wrong. The help says what to do. Both come from the
rule, so the words are yours, and the exit code is `1` until the code changes.

## Around the rule

A rule on its own is a check. What makes it a linter is the part you did not
have to write.

**Configuration.** A `.goxlintrc.json` sets a severity for each rule or
category, passes options to rules, ignores paths, and overrides settings per
file pattern. `go run ./tools/lint init` writes one with every rule turned on.
`-D`, `-W`, and `-A` flip a rule to error, warning, or off for a single run,
and `rules` prints the registry.

```json
{
  "categories": { "correctness": "error" },
  "rules": { "acme/no-wall-clock": "error" },
  "overrides": [
    { "files": ["**/*_test.go"], "rules": { "acme/no-wall-clock": "off" } }
  ]
}
```

**Suppressions, with a reason.** When a rule is right in general and wrong in
one place, the exception lives next to the code and carries an explanation.
goxlint reports suppressions that no longer suppress anything, so the
exceptions do not quietly outlive their reasons.

```go
package billing

import "time"

// IssuedAt stamps a new invoice. Tests check the field, not the clock.
func IssuedAt() time.Time {
	//goxlint:ignore acme/no-wall-clock Invoices keep the real issue time.
	return time.Now()
}
```

**Tests.** Rules are code, so they get tests. `goxlinttest` works like
ESLint's `RuleTester`: samples that must stay quiet, samples that must fire, inline in
the test. Every sample is type-checked, so a case that does not compile fails
instead of passing by accident. `RunPackage` does the same for rules that look
across files.

```go
package rules

import (
	"testing"

	"github.com/egoisutolabs/goxlint/goxlinttest"
)

func TestNoWallClock(t *testing.T) {
	goxlinttest.Run(t, NoWallClock, goxlinttest.Cases{
		Valid: []goxlinttest.Case{
			{Code: "package clock\nimport \"time\"\nfunc Now() time.Time { return time.Now() }"},
			{Code: "import \"time\"\nvar epoch = time.Unix(0, 0)"},
		},
		Invalid: []goxlinttest.Case{{
			Code:   "import \"time\"\nvar start = time.Now()",
			Errors: []goxlinttest.Error{{MessageID: "now", Line: 2}},
		}},
	})
}
```

**Fixes.** A rule can attach edits to a diagnostic and label them `safe`,
`suggestion`, or `dangerous`. `--fix` applies the safe ones;
`--fix-suggestions` and `--fix-dangerously` widen the net on purpose. goxlint
re-lints after each pass so fixes can build on each other, stops after ten,
refuses any batch that produces invalid Go, and leaves a `gofmt`-clean file
`gofmt`-clean.

**Package rules.** `Create` sees one file. `AnalyzePackage` sees all the files
of a package at once, for problems like a function copied from a sibling file.
A diagnostic can carry related locations, so the report points at the original
as well as the copy.

**Output for whoever is reading.** Code frames for a terminal. `unix` for
editors. `github` for pull request annotations, `sarif` for code scanning,
`json` for scripts and agents. `--stdin-filename` lints an unsaved buffer from
an editor or an agent hook, and with a fix flag it writes the fixed source back
to stdout. The same diagnostic as JSON:

```json
[
  {
    "rule": "acme/no-wall-clock",
    "severity": "error",
    "file": "billing/invoice.go",
    "line": 13,
    "column": 39,
    "endLine": 13,
    "endColumn": 49,
    "message": "`time.Now` reads the wall clock. Tests cannot control it.",
    "help": "Take a clock.Clock and call its Now method."
  }
]
```

**Isolation.** A rule that panics, or reports a position outside its file,
becomes an internal-error diagnostic for that rule. The run continues and the
other rules still report. A package that fails to load does not take the rest
of the run with it; its files are marked incomplete and every other package
gets its normal results. `--timing` names the slowest rules when something is
slow.

## Next to the linters you already run

Keep `go vet`, staticcheck, and golangci-lint. They know Go. goxlint knows
your project, and only what you tell it. The two kinds of rule do not compete
for the same job.

If your pipeline is built on `go/analysis`, the `goxlintanalysis` package
converts goxlint rules into `analysis.Analyzer` values. The same rule then runs
under `go vet -vettool`, `multichecker`, or a golangci-lint module plugin, with
its suppression comments, help text, related locations, and safe and
suggestion fixes intact.
A `multichecker` command is one line:

```go
package main

import (
	"github.com/egoisutolabs/goxlint/goxlintanalysis"
	"golang.org/x/tools/go/analysis/multichecker"

	"example.com/app/tools/lint/rules"
)

func main() { multichecker.Main(goxlintanalysis.Analyzers(rules.Plugin)...) }
```

Compared with writing an `analysis.Analyzer` by hand, a goxlint rule is about
the same length. The difference is everything in the previous section, which
the `go/analysis` drivers leave to you. [The adapter guide](docs/go-analysis.md)
has the details and the trade-offs.

## Status

goxlint is v0.x; the next release is v0.1.0. The public packages are
`goxlint`, `cli`, `goxlinttest`, and `goxlintanalysis`. Their APIs can change
before v1.0.0, and each release says what moved. Before v0.0.2 the module was
named `goose-lint`; update imports to `github.com/egoisutolabs/goxlint`.

It needs Go 1.26 or later and depends on `golang.org/x/tools` and nothing
else. goxlint lints itself in CI, on Linux, macOS, and Windows, with the
linter in [`examples/goxlint`](examples/goxlint/README.md). That example and
its plugins exist to show the API in use. The engine is the project.

## Read more

[Getting started](docs/getting-started.md) walks through a first rule,
configuration, and a test. [Writing rules](docs/writing-rules.md) is the full
rule API, and [Package rules](docs/package-rules.md) covers cross-file
analysis. [Configuration](docs/configuration.md), [Command line](docs/cli.md),
[Fixes](docs/fixes.md), [Suppressions](docs/suppressions.md), and
[Testing rules](docs/testing-rules.md) each cover one part of the engine.
[How goxlint works](docs/how-goxlint-works.md) explains the order of
operations inside a run. The index is at [docs/README.md](docs/README.md).

Questions, ideas, and rules you built go in
[Discussions](https://github.com/egoisutolabs/goxlint/discussions). To report a
problem or send a change, read [CONTRIBUTING.md](CONTRIBUTING.md).
For security problems, read [SECURITY.md](SECURITY.md).

goxlint is available under the [MIT License](LICENSE).
