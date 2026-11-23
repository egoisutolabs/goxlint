# Package rules

A file rule examines one file. A package rule examines all files of a package
together. Use a package rule when the problem is across files, for example a
function that is a copy of a function in a different file.

A rule can have `Create`, `AnalyzePackage`, or both. Package rules use the same
metadata, message templates, options, and diagnostics as file rules. Read
[Writing rules](writing-rules.md) first.

## A package rule

This rule reports a package that has too many files.

```go
var LargePackage = &goxlint.Rule{
	Meta: goxlint.Meta{
		Name:        "large-package",
		Description: "Report packages with too many files.",
		Category:    goxlint.Pedantic,
		Messages: map[string]string{
			"large": "Package `{{name}}` has {{count}} files. The maximum is {{max}}.",
		},
	},
	AnalyzePackage: func(pkg *goxlint.PackageContext) {
		for _, file := range pkg.Files {
			if !file.Enabled {
				continue
			}
			opts := struct {
				MaxFiles int `json:"maxFiles"`
			}{MaxFiles: 20}
			if !file.Options(&opts) {
				continue
			}
			if len(pkg.Files) > opts.MaxFiles {
				file.Report(goxlint.Diagnostic{
					Node:      file.File.Name,
					MessageID: "large",
					Data: goxlint.Data{
						"name":  pkg.Name,
						"count": strconv.Itoa(len(pkg.Files)),
						"max":   strconv.Itoa(opts.MaxFiles),
					},
					Help: "Move different responsibilities to different packages.",
				})
			}
		}
	},
}
```

Register a package rule in a plugin, the same as a file rule. The rule ID is
`<plugin>/<rule>`.

## The package context

`AnalyzePackage` gets a `*goxlint.PackageContext`:

| Field | Description |
| --- | --- |
| `Name` | The Go package name from the `package` clause. |
| `Files` | One `*goxlint.Context` for each file. The list is in file name order. |

Each file context has the same fields and methods as in a file rule. Read
[The context](writing-rules.md#the-context).

## Which files are in a package

goxlint puts files in the same group when they are in the same directory and have
the same package name.

- An external test package (`p_test`) is a different group from package `p`.
- Internal test files (`package p` in a `_test.go` file) are in the same group
  as the production files.
- goxlint does not include generated files or ignored files.
- In typed mode, goxlint includes each file one time. It does not include the
  copies from the test variants of the package.

The group contains only the files that goxlint loaded. In syntax mode, a file
pattern such as `goxlint a.go` can select only part of a package. If the rule
must have the complete package, set `Meta.NeedsTypes: true`. goxlint then uses
typed mode. Typed mode loads full packages, and it obeys build tags and
`--tags`.

## Enabled files and options

Each file has its own configuration, because overrides can change the rule for
some files.

- `file.Enabled` is `false` when the rule is off for that file. The file is
  still in `Files`. You can use it as evidence, for example as the original of
  a copied function.
- goxlint ignores reports from a file where `Enabled` is `false`.
- `file.Options` decodes the options for that file, with overrides. A disabled
  file has no options.
- A report uses the severity of the file that contains the report.

The engine does not divide a package into groups with the same options. The
rule must decide how the options of different files apply.

## Reports

Report through the context of the file that contains the primary node. The
primary diagnostic and its fix must be in that file. If the primary node is
from a different file, goxlint reports an internal error for the rule.

To refer to a supporting node, use `Related`. This helper reports a copied
function and points to the original function:

```go
package rules

import (
	"go/ast"

	"github.com/egoisutolabs/goxlint"
)

func reportDuplicate(file *goxlint.Context, fn, original *ast.FuncDecl) {
	file.Report(goxlint.Diagnostic{
		Node:    fn.Name,
		Message: "This function copies an existing function.",
		Related: []goxlint.Related{{
			Node:    original.Name,
			Message: "Existing function.",
		}},
	})
}
```

Both nodes must use `file.Fset`. The original function can be in any loaded
file of the same package, including a file where the rule is off. A related
location cannot refer to an ignored file or to a different package. See
[Related locations](writing-rules.md#related-locations) for range validation
and suppression behavior.

Report before `AnalyzePackage` returns. goxlint ignores reports after that.

## Type information

goxlint gives type information only when the rule has `NeedsTypes`. Use the `Pkg`
and `TypesInfo` fields of each file together. Test files can come from a test
variant of the package. That variant has different type-checking data.

goxlint does not run a package rule with `NeedsTypes` if one file of the package
has no type information.

## Uses in a package

`PackageContext.UsesOf` returns the uses of an object in every loaded file of
the package. The order is the file name, then the position in the file.
`Files` has the same file order.

A file where the rule is off is included. `Use.Ctx` is the context of that
file. `Use.Ctx.Enabled` is false when the rule is off for the file.

`Parent` and `Ancestors` are empty in a package rule. `UsesOf` still reports
the parent of each use. Read [Uses of an object](writing-rules.md#uses-of-an-object).

```go
package rules

import (
	"go/types"

	"github.com/egoisutolabs/goxlint"
)

func enabledUses(pkg *goxlint.PackageContext, obj types.Object) []goxlint.Use {
	var uses []goxlint.Use
	for _, use := range pkg.UsesOf(obj) {
		if use.Ctx.Enabled {
			uses = append(uses, use)
		}
	}
	return uses
}
```

## When goxlint does not run a package rule

goxlint does not run a package rule on a package in these conditions:

- The package did not load correctly.
- In typed mode, the package imports a package that did not load correctly.
- The rule needs type information, and one or more files have none.

goxlint also does not report unused suppressions for that package. Other packages
are not affected.

## Rules for package rules

- Keep all state in the `AnalyzePackage` call. goxlint can analyze different
  packages at the same time.
- Do not change syntax trees, source, type information, or metadata.
- `Parent` and `Ancestors` are empty in a package rule. Walk the AST yourself,
  for example with `ast.Inspect`. For the parent of each use, call `UsesOf`.
  Read [Uses in a package](#uses-in-a-package).

## Panics

If `AnalyzePackage` causes a panic, goxlint reports an internal error for the
rule. goxlint discards the reports that the call made before the panic. It does
not report unused suppressions for that package. Other rules continue to run.
goxlint keeps the file-rule diagnostics.

## Order of operations

1. goxlint finds the rules for each file and runs the file rules.
2. goxlint puts the files in package groups. For each group, goxlint runs each
   enabled package rule one time, in rule ID order.
3. goxlint adds the diagnostics of the two phases together.
4. goxlint applies suppressions one time, and reports unused suppressions.
5. goxlint sorts the diagnostics by position.

## Test a package rule

Use `goxlinttest.RunPackage` to test a package rule with several source files.
It runs both rule phases and supports per-file options and disabled files.
Read [Testing rules](testing-rules.md#package-rules).

## Example

The example plugin [`maintainability`](../examples/goxlint/maintainability.md)
has a package rule, `no-duplicate-functions`. It compares functions across the
files of a package with type information.
