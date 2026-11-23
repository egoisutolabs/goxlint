# A rule from a spec

A spec is the examples and the counterexamples for one rule. An example is Go
code that the rule must report. A counterexample is Go code that the rule must
not report.

The test file keeps the spec. A later change that breaks the spec fails the
test.

This document shows one spec. It then shows the test file and the rule. The
rule reports a call to `context.TODO`.

## 1. Spec

### Examples

The rule must report a direct call:

```go
import "context"

func f() {
	_ = context.TODO()
}
```

The rule must report a call through a renamed import:

```go
import c "context"

func f() {
	_ = c.TODO()
}
```

The rule must report a call used as an argument:

```go
import "context"

func f() {
	do(context.TODO())
}

func do(ctx context.Context) { _ = ctx }
```

### Counterexamples

The rule must not report `context.Background`:

```go
import "context"

func f() {
	ctx := context.Background()
	_ = ctx
}
```

The rule must not report a `context.Context` parameter:

```go
import "context"

func f(ctx context.Context) {
	_ = ctx
}
```

The rule must not report the function value. The value is not a call:

```go
import "context"

func f() {
	todo := context.TODO
	_ = todo
}
```

The rule must not report a method named `TODO` on another type:

```go
type box struct{}

func (box) TODO() int { return 0 }

func f() int { return box{}.TODO() }
```

## 2. Test file

Save this file as `tools/lint/rules/no_context_todo_test.go`.

`goxlinttest` adds `package p` when the code has no package clause. Line numbers
in `Errors` stay relative to the code in the case.

In a test, the rule ID is `test/rule`. The valid case `suppressed` uses that
ID. The other valid cases are the counterexamples. The invalid cases are the
examples.

```go
package rules

import (
	"testing"

	"github.com/egoisutolabs/goxlint/goxlinttest"
)

func TestNoContextTodo(t *testing.T) {
	goxlinttest.Run(t, NoContextTodo, goxlinttest.Cases{
		Valid: []goxlinttest.Case{
			{
				Name: "background",
				Code: `import "context"

func f() {
	ctx := context.Background()
	_ = ctx
}`,
			},
			{
				Name: "parameter",
				Code: `import "context"

func f(ctx context.Context) {
	_ = ctx
}`,
			},
			{
				Name: "value",
				Code: `import "context"

func f() {
	todo := context.TODO
	_ = todo
}`,
			},
			{
				Name: "method",
				Code: `type box struct{}

func (box) TODO() int { return 0 }

func f() int { return box{}.TODO() }`,
			},
			{
				Name: "suppressed",
				Code: `import "context"

func f() {
	//goxlint:ignore test/rule The caller has no context yet.
	_ = context.TODO()
}`,
			},
		},
		Invalid: []goxlinttest.Case{
			{
				Name: "call",
				Code: `import "context"

func f() {
	_ = context.TODO()
}`,
				Errors: []goxlinttest.Error{{MessageID: "todo", Line: 4, Column: 6}},
			},
			{
				Name: "renamed import",
				Code: `import c "context"

func f() {
	_ = c.TODO()
}`,
				Errors: []goxlinttest.Error{{MessageID: "todo", Line: 4, Column: 6}},
			},
			{
				Name: "argument",
				Code: `import "context"

func f() {
	do(context.TODO())
}

func do(ctx context.Context) { _ = ctx }`,
				Errors: []goxlinttest.Error{{MessageID: "todo", Line: 4, Column: 5}},
			},
		},
	})
}
```

## 3. Rule

Save this file as `tools/lint/rules/no_context_todo.go`.

`reportContextTodo` makes the decision. `Create` calls that function for each
`*ast.CallExpr`. A later change can replace the body of `reportContextTodo`.
The tests stay the same.

`NeedsTypes` is true. `IsPkgFunc` matches `context.TODO` when the import has
another name. It does not match a method named `TODO`.

```go
package rules

import (
	"go/ast"

	"github.com/egoisutolabs/goxlint"
)

// NoContextTodo reports calls to context.TODO.
var NoContextTodo = &goxlint.Rule{
	Meta: goxlint.Meta{
		Name:        "no-context-todo",
		Description: "Disallow context.TODO.",
		Category:    goxlint.Restriction,
		NeedsTypes:  true,
		Messages: map[string]string{
			"todo": "`context.TODO` has no deadline and no values.",
		},
	},
	Create: func(ctx *goxlint.Context) goxlint.Visitor {
		return goxlint.Visit(
			goxlint.On(func(call *ast.CallExpr) {
				reportContextTodo(ctx, call)
			}),
		)
	},
}

// reportContextTodo reports one call when it is context.TODO.
func reportContextTodo(ctx *goxlint.Context, call *ast.CallExpr) {
	if !ctx.IsPkgFunc(call, "context", "TODO") {
		return
	}
	ctx.Report(goxlint.Diagnostic{
		Node:      call,
		MessageID: "todo",
		Help:      "Take a context.Context from the caller.",
	})
}
```

## 4. Plugin

Register the rule on the plugin. The rule ID is `billing/no-context-todo`.
Replace `billing` with the plugin name of the repository.

```go
package rules

import "github.com/egoisutolabs/goxlint"

// Plugin registers the rules of this repository.
var Plugin = goxlint.Plugin{
	Name:  "billing",
	Rules: []*goxlint.Rule{NoContextTodo},
}
```

## 5. Configuration

The category of this rule is `restriction`. The default configuration does not
enable `restriction`. Run `go run ./tools/lint init` before the configuration
file exists. `init` then sets this rule to `error`.

When the file already exists, add the rule ID. `init` does not change an
existing file. This is the file that `init` writes for this rule:

```json
{
  "categories": {
    "correctness": "warn"
  },
  "rules": {
    "billing/no-context-todo": "error"
  }
}
```
