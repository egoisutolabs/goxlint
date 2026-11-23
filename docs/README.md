# goxlint documentation

goxlint is an engine to build your own Go linter. These documents tell you how to
make a linter, write rules, configure them, and run them.

## Documents

Read the documents in this order if you are new to goxlint.

1. [Getting started](getting-started.md): make a linter and write your first rule.
2. [Writing rules](writing-rules.md): the complete rule API.
3. [Testing rules](testing-rules.md): test a rule with code samples.
4. [A rule from a spec](rule-from-a-spec.md): one spec, its test file, and its rule.
5. [Configuration](configuration.md): the `.goxlintrc.json` file.
6. [Command line](cli.md): commands, flags, output formats, and exit codes.
7. [Fixes](fixes.md): attach fixes to reports, and apply them.
8. [Suppressions](suppressions.md): disable a rule for one line or one file.
9. [Package rules](package-rules.md): examine all files of a package together.
10. [go/analysis adapter](go-analysis.md): run rules with existing analysis drivers.
11. [How goxlint works](how-goxlint-works.md): the internal order of operations.

The repository also contains agent skills in [`skills/`](../skills). The install
command is in [Getting started](getting-started.md#agent-skills).

## Packages

goxlint has four public packages. Other packages are internal.

| Package | Use |
| --- | --- |
| `github.com/egoisutolabs/goxlint` | The rule API. Rules import this package. |
| `github.com/egoisutolabs/goxlint/cli` | The command runner. Your `main` package calls `cli.Main`. |
| `github.com/egoisutolabs/goxlint/goxlinttest` | The rule test harness. |
| `github.com/egoisutolabs/goxlint/goxlintanalysis` | The go/analysis adapter. |

## Terms

These documents use these terms with these meanings.

| Term | Meaning |
| --- | --- |
| Linter | The program that you build. It calls `cli.Main` with your plugins. |
| Plugin | A named set of rules, `goxlint.Plugin`. |
| Rule | One check, `goxlint.Rule`. |
| Rule ID | The plugin name and the rule name, for example `acme/no-foo`. |
| Diagnostic | One problem that a rule reports. |
| Related location | A supporting source range attached to a diagnostic. |
| Severity | `off`, `warn`, or `error`. The configuration sets the severity of each rule. |
| Category | A group of rules, for example `correctness` or `style`. |
| Fix | A set of text edits that a rule attaches to a diagnostic. |
| Pattern | A compiled example of Go code with variables. |
| Capture | The nodes that a pattern variable records. |
| Exception | A pattern that removes a match. |
| Suppression | A `//goxlint:ignore` comment. |
| Syntax mode | goxlint parses files only. Rules do not get type information. |
| Typed mode | goxlint loads packages and type-checks them. |
| Use | One place where the source refers to an object. The type is `goxlint.Use`. |
| Spec | The examples and the counterexamples for one rule. |
| Example | In a spec, Go code that the rule must report. |
| Counterexample | In a spec, Go code that the rule must not report. |

## Writing style

These documents use ASD-STE100 Simplified Technical English. The sentences are
short. Procedures use one instruction in each step. Code names, such as
`ctx.Report`, are technical names.
