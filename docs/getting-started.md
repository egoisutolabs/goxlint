# Getting started

This procedure makes a linter with one rule. The rule finds calls to
`fmt.Println`. Then you configure the rule, test it, and run the linter.

## Requirements

- Go 1.26 or later.
- A Go module that you want to lint.

## 1. Add goxlint to your module

1. Go to the root directory of your module.
2. Add the dependency:

   ```sh
   go get github.com/egoisutolabs/goxlint
   ```

You can also keep the linter in a separate module. goxlint does not need to be in
the same module as the code that it examines. From that module, use `-C` to
lint the repository root. Read
[Run from another directory](cli.md#run-from-another-directory).

## 2. Write a rule

1. Make a directory for your plugin, for example `tools/lint/rules`.
2. Make the file `tools/lint/rules/no_println.go`:

   ```go
   package rules

   import (
   	"go/ast"

   	"github.com/egoisutolabs/goxlint"
   )

   // NoPrintln finds calls to fmt.Println.
   var NoPrintln = &goxlint.Rule{
   	Meta: goxlint.Meta{
   		Name:        "no-println",
   		Description: "Disallow fmt.Println.",
   		Category:    goxlint.Restriction,
   		NeedsTypes:  true,
   		Messages: map[string]string{
   			"println": "Do not write to stdout with `fmt.Println`.",
   		},
   	},
   	Create: func(ctx *goxlint.Context) goxlint.Visitor {
   		return goxlint.Visit(
   			goxlint.On(func(call *ast.CallExpr) {
   				if ctx.IsPkgFunc(call, "fmt", "Println") {
   					ctx.Report(goxlint.Diagnostic{
   						Node:      call,
   						MessageID: "println",
   						Help:      "Use a logger that the caller can configure.",
   					})
   				}
   			}),
   		)
   	},
   }
   ```

The rule asks for type information with `NeedsTypes: true`. With type
information, `ctx.IsPkgFunc` finds `fmt.Println` also when the import has a
different name.

3. Make the file `tools/lint/rules/plugin.go`:

   ```go
   package rules

   import "github.com/egoisutolabs/goxlint"

   // Plugin registers the rules. Their IDs start with "acme/".
   var Plugin = goxlint.Plugin{
   	Name:  "acme",
   	Rules: []*goxlint.Rule{NoPrintln},
   }
   ```

## 3. Make the linter command

1. Make the file `tools/lint/main.go`:

   ```go
   package main

   import (
   	"github.com/egoisutolabs/goxlint/cli"

   	"example.com/project/tools/lint/rules"
   )

   func main() { cli.Main(rules.Plugin) }
   ```

2. Replace `example.com/project` with the path of your module.
3. Make sure that the command builds:

   ```sh
   go build ./tools/lint
   ```

You can register more than one plugin: `cli.Main(rules.Plugin, other.Plugin)`.
Each rule ID must be unique. If two rules have the same ID, the linter stops
with a panic when it starts.

## 4. Enable the rule

The default configuration enables only the `correctness` category, with the
severity `warn`. The example rule is in the `restriction` category. Thus you
must enable it.

1. Make the configuration file:

   ```sh
   go run ./tools/lint init
   ```

   This command writes `.goxlintrc.json`. The file sets each registered rule to
   `error`.

2. Examine the file. Change the severities that you do not want.

For all settings, read [Configuration](configuration.md).

## 5. Run the linter

1. Run the linter on all packages:

   ```sh
   go run ./tools/lint ./...
   ```

2. Read the result. Each diagnostic shows the rule ID, the message, the code,
   and the help text:

   ```text
     × acme(no-println): Do not write to stdout with `fmt.Println`.
      ╭─[server/handler.go:14:2]
   13 │ 	if err != nil {
   14 │ 		fmt.Println(err)
      · 		────────────────
   15 │ 	}
      ╰────
     help: Use a logger that the caller can configure.

   Found 0 warnings and 1 error.
   Finished in 412ms on 38 files with 1 rule using 8 threads (typed mode).
   ```

The exit code is `1` when there are errors. For all flags and exit codes, read
[Command line](cli.md).

## 6. Test the rule

1. Make the file `tools/lint/rules/no_println_test.go`:

   ```go
   package rules

   import (
   	"testing"

   	"github.com/egoisutolabs/goxlint/goxlinttest"
   )

   func TestNoPrintln(t *testing.T) {
   	goxlinttest.Run(t, NoPrintln, goxlinttest.Cases{
   		Valid: []goxlinttest.Case{
   			{Code: "import \"fmt\"\nfunc f() { fmt.Print(1) }"},
   		},
   		Invalid: []goxlinttest.Case{
   			{
   				Code:   "import \"fmt\"\nfunc f() { fmt.Println(1) }",
   				Errors: []goxlinttest.Error{{MessageID: "println", Line: 2}},
   			},
   		},
   	})
   }
   ```

2. Run the test:

   ```sh
   go test ./tools/lint/...
   ```

For all test settings, read [Testing rules](testing-rules.md).

## Next steps

- Read [Writing rules](writing-rules.md) for the complete rule API.
- Add a fix to your rule. Read [Fixes](fixes.md).
- Read the example plugins in [`examples/goxlint/plugins`](../examples/goxlint/plugins).
