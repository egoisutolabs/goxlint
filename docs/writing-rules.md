# Writing rules

A rule is a `*goxlint.Rule` value. It has metadata and one or two callbacks:

- `Create` runs one time for each file. It returns the node handlers for that
  file. This document describes file rules.
- `AnalyzePackage` runs one time for each package. Read
  [Package rules](package-rules.md).

A rule can have one callback or both callbacks.

## A complete rule

```go
package rules

import (
	"go/ast"
	"strconv"

	"github.com/egoisutolabs/goxlint"
)

// MaxParams limits the number of parameters of a function.
var MaxParams = &goxlint.Rule{
	Meta: goxlint.Meta{
		Name:        "max-params",
		Description: "Limit the number of function parameters.",
		Category:    goxlint.Pedantic,
		Messages: map[string]string{
			"tooMany": "`{{name}}` has {{count}} parameters. The maximum is {{max}}.",
		},
	},
	Create: func(ctx *goxlint.Context) goxlint.Visitor {
		opts := struct {
			Max int `json:"max"`
		}{Max: 4}
		if !ctx.Options(&opts) {
			return nil
		}
		return goxlint.Visit(
			goxlint.On(func(fn *ast.FuncDecl) {
				if n := fn.Type.Params.NumFields(); n > opts.Max {
					ctx.Report(goxlint.Diagnostic{
						Node:      fn.Name,
						MessageID: "tooMany",
						Data: goxlint.Data{
							"name":  fn.Name.Name,
							"count": strconv.Itoa(n),
							"max":   strconv.Itoa(opts.Max),
						},
						Help: "Put related parameters in a struct.",
					})
				}
			}),
		)
	},
}
```

## Metadata

`goxlint.Meta` describes the rule.

| Field | Description |
| --- | --- |
| `Name` | The rule name without the plugin name, for example `max-params`. Use lowercase words with hyphens. |
| `Description` | One line. `goxlint rules` shows it. |
| `Category` | One of `goxlint.Correctness`, `goxlint.Suspicious`, `goxlint.Pedantic`, `goxlint.Style`, `goxlint.Restriction`, `goxlint.Nursery`. |
| `Messages` | Message templates. The key is a message ID. Read [Messages](#messages). |
| `Fixable` | The most dangerous fix level that the rule can make. The default is `goxlint.FixNone`. Read [Fixes](fixes.md). |
| `NeedsTypes` | Set to `true` when the rule uses type information. |

The rule ID is `<plugin>/<name>`. Users write this ID in the configuration and
in suppression comments.

## Plugins

A plugin is a named set of rules:

```go
var Plugin = goxlint.Plugin{
	Name:  "acme",
	Rules: []*goxlint.Rule{MaxParams, NoPrintln},
}
```

Give the plugin to `cli.Main`. Each rule ID must be unique across all plugins.

## The visitor

`Create` returns a `goxlint.Visitor`. Make it with `goxlint.Visit` and one or more
handlers:

| Function | goxlint calls the handler |
| --- | --- |
| `goxlint.On(func(n *ast.T) {...})` | Before the children of each node of type `*ast.T`. |
| `goxlint.OnExit(func(n *ast.T) {...})` | After the children of each node of type `*ast.T`. |

The type parameter selects the node type. You can register more than one
handler for the same type.

```go
return goxlint.Visit(
	goxlint.On(func(fn *ast.FuncDecl) { depth++ }),
	goxlint.OnExit(func(fn *ast.FuncDecl) { depth-- }),
	goxlint.On(func(ret *ast.ReturnStmt) { /* use depth */ }),
)
```

goxlint walks each file one time for all rules. It calls only the handlers for
the node types that the rules registered.

### Interface node types

You can also register a handler for an interface type, for example `ast.Stmt`
or `ast.Expr`. goxlint then calls the handler for each node that implements the
interface.

```go
goxlint.On(func(s ast.Stmt) { /* each statement */ })
```

**CAUTION:** An interface handler makes goxlint visit every node of the file.
This is slower. Use concrete node types when possible.

### No handlers

`Create` can return `nil`. The rule then does nothing on that file. Use this
when the rule does not apply, for example:

```go
if ctx.IsTestFile() {
	return nil
}
```

### State

`Create` runs one time for each file. Variables that you declare in `Create`
are local to that file. goxlint can lint different files at the same time. Thus,
do not keep state in package-level variables.

## The context

`Create` gets a `*goxlint.Context`. The context gives access to the file and
reports diagnostics.

### Fields

| Field | Description |
| --- | --- |
| `Rule` | The rule. |
| `RuleID` | The rule ID, for example `acme/max-params`. |
| `Fset` | The `token.FileSet` of the file. |
| `File` | The `*ast.File`. |
| `Filename` | The absolute path of the file. |
| `Source` | The contents of the file. |
| `Pkg` | The `*types.Package`. It is `nil` if the rule does not have `NeedsTypes`. |
| `TypesInfo` | The `*types.Info`. It is `nil` if the rule does not have `NeedsTypes`. |
| `Enabled` | For package rules only. Read [Package rules](package-rules.md). |

Do not change the AST, the source, or the type information. Other rules use
the same data.

### Methods

| Method | Returns |
| --- | --- |
| `Report(d)` | Records a diagnostic. Read [Reports](#reports). |
| `Options(&v)` | Decodes the rule options into `v`. Read [Options](#options). |
| `Parent()` | The parent of the current node, or `nil`. |
| `Ancestors()` | The ancestors of the current node. The nearest ancestor is first. |
| `Text(node)` | The source text of the node. |
| `Snippet(node)` | The source text on one line, with a maximum of 60 characters. Use it in messages. |
| `Line(pos)` | The line number of a position. |
| `OwnLine(pos)` | `true` if only white space is before `pos` on its line. |
| `CommentsBefore(pos)` | The comment group that ends on the line directly above `pos`, or `nil`. |
| `TrailingComment(pos)` | The comment group after `pos` on the same line, or `nil`. |
| `IsTestFile()` | `true` if the file name ends with `_test.go`. |
| `TypeOf(expr)` | The type of the expression, or `nil` without type information. |
| `ObjectOf(id)` | The object that the identifier refers to, or `nil` without type information. |
| `CalleeFunc(call)` | The function or method that the call refers to, or `nil`. |
| `IsPkgFunc(call, path, names...)` | `true` if the call is a call to one of the package-level functions `path.name`. |

`Parent` and `Ancestors` are correct only while a handler runs. Do not keep
their results for later use.

`Line`, `OwnLine`, and the comment methods use the physical lines of the file.
They ignore `//line` directives.

## Reports

Call `ctx.Report` with a `goxlint.Diagnostic`:

| Field | Description |
| --- | --- |
| `Node` | The node that has the problem. goxlint uses its start and end positions. |
| `Pos`, `End` | Positions. Use them instead of `Node` when the problem is not one node. |
| `MessageID` | A key of `Meta.Messages`. |
| `Message` | A message text. Use it instead of `MessageID`. |
| `Data` | Values for the placeholders in the message template. |
| `Help` | Optional. A hint that tells the user how to correct the problem. |
| `Fix` | Optional. Read [Fixes](fixes.md). |
| `Related` | Optional. Supporting locations in loaded files of the same package. |

The primary positions must be in the current file. The start must not be
after the end. If `End` is not set, goxlint uses `Pos`.

### Related locations

Set `Diagnostic.Related` to a list of `goxlint.Related` values. Use this field
for supporting locations, such as the original function for a copied function.
Each entry has these fields:

| Field | Description |
| --- | --- |
| `Node` | The supporting node. Its positions take precedence over `Pos` and `End`. |
| `Pos`, `End` | Positions to use when `Node` is not set. An omitted `End` means a point at `Pos`. |
| `Message` | Optional. Text that labels the supporting location. |

Use positions from the reporting context's `Fset`. Each range must be ordered
and stay within one loaded file of the same package. The file can be the
primary file or a different file. A file where the rule is off can supply a
related location. An ignored file, a generated file, an unloaded file, or a
file from a different package cannot supply one. Positions from an old parse
are not valid, even if the file name is the same.

Related locations stay attached to the primary diagnostic, in report order.
They do not add warnings or errors. Suppressions match only the primary
location. A suppression at a related location does not suppress the report
or receive credit in the unused-suppression check. Fixes still apply only to
the primary file.

If one related range is invalid, goxlint replaces the whole report with an
internal-error diagnostic. It does not keep the related locations or the fix.
Other rules continue to run. See the [package rule example](package-rules.md#reports).
Use `goxlinttest.RelatedError` to [test related locations](testing-rules.md#related-locations).

### Messages

Write message templates in `Meta.Messages`. A template can contain
placeholders with the form `{{key}}`. goxlint replaces each placeholder with the
value from `Data`.

```go
Messages: map[string]string{
	"tooMany": "`{{name}}` has {{count}} parameters. The maximum is {{max}}.",
},
```

Message IDs give these advantages:

- All messages of a rule are in one location.
- Tests can examine the message ID instead of the full text.

Use `Message` only for text that you cannot make from a template.

### Severity

The rule does not set the severity. The configuration sets it. The same report
can be a warning in one project and an error in a different project.

### Rule errors

goxlint stops a rule on a file and reports an internal error in these
conditions:

- The rule calls `Report` with an unknown `MessageID`.
- The rule reports a fix with a level that is more dangerous than `Meta.Fixable`.
- The primary diagnostic or a fix edit has positions outside the current file.
- A related location has an invalid range or refers to a file outside the
  loaded package.
- A handler or `Create` causes a panic.

An internal error has the severity `error`. Its message starts with
`internal error in rule`. Other rules continue to run.

## Options

A rule can accept options. Users set them in the configuration:

```json
{ "rules": { "acme/max-params": ["warn", { "max": 6 }] } }
```

Decode the options in `Create`:

1. Declare a struct with JSON tags.
2. Set the default values in the struct.
3. Call `ctx.Options` with a pointer to the struct.
4. If `ctx.Options` returns `false`, return `nil`.

```go
opts := struct {
	Max        int  `json:"max"`
	CheckTests bool `json:"checkTests"`
}{Max: 4}
if !ctx.Options(&opts) {
	return nil
}
```

`ctx.Options` does not change the struct when the user gives no options. Thus
the default values stay. Fields that the user does not give also keep their
default values.

`ctx.Options` does not accept unknown fields. When the options are not correct,
it reports an `invalid options for <rule ID>` diagnostic and returns `false`.

To validate values, report a diagnostic yourself:

```go
if opts.Max < 1 {
	ctx.Report(goxlint.Diagnostic{Pos: ctx.File.Package, Message: "invalid options: max must be at least 1"})
	return nil
}
```

Write the options of each rule in its documentation comment.

## Type information

Set `NeedsTypes: true` to get type information. The context then has `Pkg` and
`TypesInfo`, and `TypeOf`, `ObjectOf`, `CalleeFunc`, and `IsPkgFunc` return
values.

```go
goxlint.On(func(call *ast.CallExpr) {
	if ctx.IsPkgFunc(call, "context", "TODO") {
		ctx.Report(goxlint.Diagnostic{Node: call, MessageID: "todo"})
	}
})
```

Use type information to identify functions and types. A name in the syntax
tree is not sufficient. An import can have a different name, and a local
declaration can hide a package-level one.

**CAUTION:** One enabled rule with `NeedsTypes` puts the full run in typed mode.
Typed mode loads and type-checks all packages. It is much slower than syntax
mode. Do not set `NeedsTypes` when the syntax tree is sufficient.

If type information is not available for a file, goxlint does not run the rule on
that file. This can occur when you call the engine without types.

## Test files

goxlint lints `_test.go` files with the same rules as other files. Many rules
must not apply to tests. Use `ctx.IsTestFile()`, and give users an option to
change this:

```go
opts := struct {
	CheckTests bool `json:"checkTests"`
}{}
if !ctx.Options(&opts) || (ctx.IsTestFile() && !opts.CheckTests) {
	return nil
}
```

Users can also turn rules off for test files with an override. Read
[Configuration](configuration.md#overrides).

## Guidelines

- Write one problem in each rule. Small rules are easier to configure and to
  suppress.
- Make the message tell what is wrong. Put the correction in `Help`.
- Quote code with `ctx.Snippet`. It keeps messages on one line.
- Make reports on the smallest node that shows the problem.
- Prevent false positives. A rule that reports correct code causes users to
  suppress it or turn it off.
- Run the rule on real code before you release it. Examine each report.
- Test the rule. Read [Testing rules](testing-rules.md).

## Examples

The [`examples/goxlint/plugins`](../examples/goxlint/plugins) directory has complete
rules. These rules show:

- Options with default values and `checkTests`.
- Type information with `IsPkgFunc`, `CalleeFunc`, and `TypeOf`.
- Comment analysis with `CommentsBefore` and `TrailingComment`.
- Suggestion fixes.
- A package rule with type information.
