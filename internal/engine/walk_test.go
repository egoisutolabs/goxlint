package engine

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/egoisutolabs/goxlint"
)

// rareSrc holds nodes that are easy to miss: generics, labels, goto, select,
// channel types, a struct tag, and a BadExpr from a deliberate parse error.
const rareSrc = `package sample

func f(ch chan int) {
start:
	goto start
	select {
	case <-ch:
	default:
	}
	_ = pair[int, string]{}
}

type pair[T any, U any] struct {
	Left T ` + "`json:\"left\"`" + `
}

var x =
`

func TestConcreteNodeTypesComplete(t *testing.T) {
	known := map[reflect.Type]bool{}
	for _, nt := range concreteNodeTypes {
		if nt.Kind() != reflect.Pointer || !nt.Implements(reflect.TypeFor[ast.Node]()) {
			t.Fatalf("%s is not a concrete ast.Node pointer", nt)
		}
		if _, ok := reflect.Zero(nt).Interface().(ast.Node); !ok {
			t.Fatalf("%s cannot be used in the walk filter", nt)
		}
		if known[nt] {
			t.Fatalf("duplicate node type %s", nt)
		}
		known[nt] = true
	}

	check := func(f *ast.File) {
		t.Helper()
		var missing []string
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				return false
			}
			nt := reflect.TypeOf(n)
			if !known[nt] {
				missing = append(missing, nt.String())
			}
			return true
		})
		slices.Sort(missing)
		missing = slices.Compact(missing)
		if len(missing) > 0 {
			t.Fatalf("node types missing from concreteNodeTypes: %s", strings.Join(missing, ", "))
		}
	}

	fset := token.NewFileSet()
	rare, err := parser.ParseFile(fset, "rare.go", rareSrc, parser.ParseComments)
	if rare == nil {
		t.Fatalf("parse rare.go: %v", err)
	}
	if err == nil {
		t.Fatal("rare.go parsed without the deliberate error")
	}
	var sawTag, sawGoto, sawIndexList, sawLabel, sawSelect, sawChan, sawBadExpr bool
	ast.Inspect(rare, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Field:
			if n.Tag != nil {
				sawTag = true
			}
		case *ast.BranchStmt:
			if n.Tok == token.GOTO {
				sawGoto = true
			}
		case *ast.IndexListExpr:
			sawIndexList = true
		case *ast.LabeledStmt:
			sawLabel = true
		case *ast.SelectStmt:
			sawSelect = true
		case *ast.ChanType:
			sawChan = true
		case *ast.BadExpr:
			sawBadExpr = true
		}
		return true
	})
	if !sawTag || !sawGoto || !sawIndexList || !sawLabel || !sawSelect || !sawChan || !sawBadExpr {
		t.Fatalf("rare.go missing nodes: tag=%t goto=%t indexList=%t label=%t select=%t chan=%t badExpr=%t",
			sawTag, sawGoto, sawIndexList, sawLabel, sawSelect, sawChan, sawBadExpr)
	}
	check(rare)

	goroot := runtime.GOROOT()
	if goroot == "" {
		t.Fatal("GOROOT is empty")
	}
	for _, pkg := range []string{"go/ast", "go/parser", "net/http", "reflect"} {
		dir := filepath.Join(goroot, "src", filepath.FromSlash(pkg))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		files := 0
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			parsed, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if err != nil {
				t.Fatalf("parse %s: %v", filepath.ToSlash(path), err)
			}
			check(parsed)
			files++
		}
		if files == 0 {
			t.Fatalf("no Go files in %s", filepath.ToSlash(dir))
		}
	}
}

func TestInterfaceAndConcreteDiagnostics(t *testing.T) {
	src := "package p\nfunc f(x int) int {\n\tif x > 0 {\n\t\treturn x\n\t}\n\treturn 0\n}\n"
	report := func(message string) func(*goxlint.Context, ast.Node) {
		return func(ctx *goxlint.Context, n ast.Node) {
			ctx.Report(goxlint.Diagnostic{Node: n, Message: message})
		}
	}
	stmt := func(name, message string, exit bool) *goxlint.Rule {
		return &goxlint.Rule{
			Meta: goxlint.Meta{Name: name},
			Create: func(ctx *goxlint.Context) goxlint.Visitor {
				fn := func(n ast.Stmt) { report(message)(ctx, n) }
				if exit {
					return goxlint.Visit(goxlint.OnExit(fn))
				}
				return goxlint.Visit(goxlint.On(fn))
			},
		}
	}
	ifs := func(name, message string, exit bool) *goxlint.Rule {
		return &goxlint.Rule{
			Meta: goxlint.Meta{Name: name},
			Create: func(ctx *goxlint.Context) goxlint.Visitor {
				fn := func(n *ast.IfStmt) { report(message)(ctx, n) }
				if exit {
					return goxlint.Visit(goxlint.OnExit(fn))
				}
				return goxlint.Visit(goxlint.On(fn))
			},
		}
	}
	diags, _ := lintSource(t, src,
		stmt("stmt-enter", "stmt-enter", false),
		ifs("if-enter", "if-enter", false),
		stmt("stmt-exit", "stmt-exit", true),
		ifs("if-exit", "if-exit", true),
	)
	var got []string
	for _, d := range diags {
		got = append(got, fmt.Sprintf("%s %d:%d %s", d.RuleID, d.Start.Line, d.Start.Column, d.Message))
	}
	want := []string{
		"t/stmt-enter 2:19 stmt-enter",
		"t/stmt-exit 2:19 stmt-exit",
		"t/if-enter 3:2 if-enter",
		"t/stmt-enter 3:2 stmt-enter",
		"t/if-exit 3:2 if-exit",
		"t/stmt-exit 3:2 stmt-exit",
		"t/stmt-enter 3:11 stmt-enter",
		"t/stmt-exit 3:11 stmt-exit",
		"t/stmt-enter 4:3 stmt-enter",
		"t/stmt-exit 4:3 stmt-exit",
		"t/stmt-enter 6:2 stmt-enter",
		"t/stmt-exit 6:2 stmt-exit",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestConcreteHandlersRunBeforeInterfaceHandlers(t *testing.T) {
	var calls []string
	var ancestors []string
	note := func(name string, ctx *goxlint.Context, n ast.Node) {
		if _, ok := n.(*ast.IfStmt); !ok {
			return
		}
		calls = append(calls, fmt.Sprintf("%s %T", name, ctx.Parent()))
		if name == "both-concrete" {
			for _, a := range ctx.Ancestors() {
				ancestors = append(ancestors, fmt.Sprintf("%T", a))
			}
		}
	}
	iface := func(name string, exit bool) *goxlint.Rule {
		return &goxlint.Rule{
			Meta: goxlint.Meta{Name: name},
			Create: func(ctx *goxlint.Context) goxlint.Visitor {
				fn := func(n ast.Stmt) { note(name, ctx, n) }
				if exit {
					return goxlint.Visit(goxlint.OnExit(fn))
				}
				return goxlint.Visit(goxlint.On(fn))
			},
		}
	}
	concrete := func(name string, exit bool) *goxlint.Rule {
		return &goxlint.Rule{
			Meta: goxlint.Meta{Name: name},
			Create: func(ctx *goxlint.Context) goxlint.Visitor {
				fn := func(n *ast.IfStmt) { note(name, ctx, n) }
				if exit {
					return goxlint.Visit(goxlint.OnExit(fn))
				}
				return goxlint.Visit(goxlint.On(fn))
			},
		}
	}
	both := &goxlint.Rule{
		Meta: goxlint.Meta{Name: "both"},
		Create: func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(
				goxlint.On(func(n *ast.IfStmt) { note("both-concrete", ctx, n) }),
				goxlint.On(func(n ast.Stmt) { note("both-iface", ctx, n) }),
			)
		},
	}
	lintSource(t, "package p\nfunc f() {\n\tif true {\n\t}\n}\n",
		iface("iface-a", false),
		both,
		concrete("concrete-b", false),
		iface("iface-d", false),
		iface("iface-exit-e", true),
		concrete("concrete-exit-f", true),
	)
	want := []string{
		"both-concrete *ast.BlockStmt",
		"concrete-b *ast.BlockStmt",
		"iface-a *ast.BlockStmt",
		"both-iface *ast.BlockStmt",
		"iface-d *ast.BlockStmt",
		"concrete-exit-f *ast.BlockStmt",
		"iface-exit-e *ast.BlockStmt",
	}
	if !slices.Equal(calls, want) {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
	wantAncestors := []string{"*ast.BlockStmt", "*ast.FuncDecl", "*ast.File"}
	if !slices.Equal(ancestors, wantAncestors) {
		t.Fatalf("ancestors: got %v, want %v", ancestors, wantAncestors)
	}
}

func TestInterfaceHandlerKeepsWalkFilter(t *testing.T) {
	var visited []string
	previous := observeWalk
	observeWalk = func(n ast.Node, push bool) {
		if push {
			visited = append(visited, reflect.TypeOf(n).String())
		}
	}
	t.Cleanup(func() { observeWalk = previous })

	exprRule := &goxlint.Rule{
		Meta: goxlint.Meta{Name: "expr"},
		Create: func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(goxlint.On(func(e ast.Expr) {
				if lit, ok := e.(*ast.BasicLit); ok && lit.Value == "1" {
					ctx.Report(goxlint.Diagnostic{Node: e, Message: "lit"})
				}
			}))
		},
	}
	diags, _ := lintSource(t, "package p\nfunc f() int { return 1 }\n", exprRule)
	if len(diags) != 1 || diags[0].Message != "lit" || diags[0].Start.Line != 2 {
		t.Fatalf("expression handler: %+v", diags)
	}
	sawExpr := false
	for _, typ := range visited {
		switch typ {
		case "*ast.ReturnStmt", "*ast.BlockStmt", "*ast.FuncDecl", "*ast.File", "*ast.FieldList", "*ast.Field":
			t.Fatalf("walk dispatched %s", typ)
		case "*ast.BasicLit", "*ast.Ident":
			sawExpr = true
		}
	}
	if !sawExpr {
		t.Fatalf("walk did not dispatch expression nodes: %v", visited)
	}
}

func TestInterfacePanicDoesNotStopOtherRules(t *testing.T) {
	var calls []string
	panics := &goxlint.Rule{
		Meta: goxlint.Meta{Name: "panics"},
		Create: func(*goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(
				goxlint.On(func(*ast.IfStmt) {
					calls = append(calls, "concrete")
					panic("boom")
				}),
				goxlint.On(func(n ast.Stmt) {
					if _, ok := n.(*ast.IfStmt); ok {
						calls = append(calls, "same-rule-interface")
					}
				}),
				goxlint.OnExit(func(*ast.IfStmt) { calls = append(calls, "same-rule-exit") }),
			)
		},
	}
	other := &goxlint.Rule{
		Meta: goxlint.Meta{Name: "other"},
		Create: func(*goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(goxlint.On(func(n ast.Stmt) {
				if _, ok := n.(*ast.IfStmt); ok {
					calls = append(calls, "other-if")
				}
			}))
		},
	}
	diags, _ := lintSource(t, "package p\nfunc f() {\n\tif true {\n\t}\n}\n", panics, other)
	want := []string{"concrete", "other-if"}
	if !slices.Equal(calls, want) {
		t.Fatalf("calls: got %v, want %v", calls, want)
	}
	if len(diags) != 1 || diags[0].RuleID != "t/panics" || !strings.Contains(diags[0].Message, "boom") || !diags[0].internal {
		t.Fatalf("panic diagnostic: %+v", diags)
	}
}
