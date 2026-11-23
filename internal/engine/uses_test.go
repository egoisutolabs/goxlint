package engine

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/egoisutolabs/goxlint"
)

func TestUsesOfReportsEachKind(t *testing.T) {
	src := `package p

type Rec struct{ Field int }

func callee() int { return 1 }

var pkgVar int

func demo(param int) int {
	local := param
	_ = local
	_ = pkgVar
	_ = (pkgVar)
	r := Rec{Field: local}
	_ = r.Field
	_ = (r).Field
	_ = (r.Field)
	return callee()
}

func indexed() int {
	m := map[string]int{"k": 1}
	return m["k"] + (m)["k"] + ((m))["k"]
}

type box struct{ M map[string]int }

func fieldIndex() int {
	ms := box{M: map[string]int{"k": 1}}
	return ms.M["k"] + (ms.M)["k"]
}
`
	rule := &goxlint.Rule{
		Meta:   goxlint.Meta{Name: "uses", NeedsTypes: true},
		Create: func(ctx *goxlint.Context) goxlint.Visitor { return describeUses(ctx) },
	}
	_, diags := lintOne(t, src, rule)
	requireMessages(t, diags, []string{
		"param AssignStmt Ident",
		"local AssignStmt Ident",
		"pkgVar AssignStmt Ident",
		"pkgVar AssignStmt Ident",
		"Field KeyValueExpr Ident",
		"local KeyValueExpr Ident",
		"Field AssignStmt SelectorExpr",
		"Field AssignStmt SelectorExpr",
		"Field AssignStmt SelectorExpr",
		"callee CallExpr Ident",
		"m IndexExpr Ident direct",
		"m IndexExpr Ident paren",
		"m IndexExpr Ident paren",
		"M KeyValueExpr Ident",
		"M IndexExpr SelectorExpr direct",
		"M IndexExpr SelectorExpr paren",
	})
}

func describeUses(ctx *goxlint.Context) goxlint.Visitor {
	return goxlint.Visit(goxlint.On(func(*ast.File) {
		for _, name := range []string{"local", "param", "pkgVar", "Field", "callee", "m", "M"} {
			obj := findDef(ctx, name)
			if obj == nil {
				ctx.Report(goxlint.Diagnostic{Node: ctx.File.Name, Message: "missing " + name})
				continue
			}
			for _, use := range ctx.UsesOf(obj) {
				rel := ""
				if ix, ok := use.Parent.(*ast.IndexExpr); ok {
					if ix.X == use.Expr {
						rel = " direct"
					} else {
						rel = " paren"
					}
				}
				ctx.Report(goxlint.Diagnostic{
					Node:    use.Ident,
					Message: name + " " + nodeKind(use.Parent) + " " + nodeKind(use.Expr) + rel,
				})
			}
		}
	}))
}

func TestUsesOfWithoutTypes(t *testing.T) {
	src := `package p

func f() int {
	x := 1
	return x
}
`
	plain := &goxlint.Rule{
		Meta: goxlint.Meta{Name: "plain"},
		Create: func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(goxlint.On(func(*ast.File) {
				obj := types.NewVar(token.NoPos, nil, "x", types.Typ[types.Int])
				if ctx.UsesOf(obj) != nil || ctx.UsesOf(nil) != nil || ctx.ObjectOfExpr(ctx.File.Name) != nil || ctx.CalleeFunc(nil) != nil || ctx.CalleeFunc(&ast.CallExpr{Fun: ctx.File.Name}) != nil {
					ctx.Report(goxlint.Diagnostic{Node: ctx.File.Name, Message: "not-nil"})
					return
				}
				ctx.Report(goxlint.Diagnostic{Node: ctx.File.Name, Message: "nil"})
			}))
		},
	}
	typed := &goxlint.Rule{
		Meta: goxlint.Meta{Name: "typed", NeedsTypes: true},
		Create: func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(goxlint.On(func(*ast.File) {
				ctx.Report(goxlint.Diagnostic{Node: ctx.File.Name, Message: "typed"})
			}))
		},
	}
	files, diags := lintOne(t, src, plain, typed)
	requireMessages(t, diags, []string{"nil", "typed"})
	if files[0].uses.builds != 0 {
		t.Fatalf("built the index %d times without UsesOf", files[0].uses.builds)
	}
}

func TestUsesBuiltOnce(t *testing.T) {
	src := `package p

var x = 1

func f() int { return x }
`
	rule := func(name string, analyze bool) *goxlint.Rule {
		r := &goxlint.Rule{Meta: goxlint.Meta{Name: name, NeedsTypes: true}}
		r.Create = func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(goxlint.On(func(*ast.File) {
				obj := findDef(ctx, "x")
				var bad atomic.Bool
				var wg sync.WaitGroup
				for range 8 {
					wg.Go(func() {
						uses := ctx.UsesOf(obj)
						if len(uses) != 1 || uses[0].Ctx != ctx {
							bad.Store(true)
						}
					})
				}
				wg.Wait()
				msg := name + " ok"
				if bad.Load() || obj == nil {
					msg = name + " bad"
				}
				ctx.Report(goxlint.Diagnostic{Node: ctx.File.Name, Message: msg})
			}))
		}
		if analyze {
			r.AnalyzePackage = func(pkg *goxlint.PackageContext) {
				for _, file := range pkg.Files {
					obj := findDef(file, "x")
					_ = file.UsesOf(obj)
					_ = pkg.UsesOf(obj)
				}
			}
		}
		return r
	}
	files, diags := lintOne(t, src, rule("one", true), rule("two", false))
	requireMessages(t, diags, []string{"one ok", "two ok"})
	if files[0].uses.builds != 1 {
		t.Fatalf("builds = %d, want 1", files[0].uses.builds)
	}
}

func TestPackageUsesOf(t *testing.T) {
	files := map[string]string{
		"b.go": `package p

var shared = 1

func B() int { return shared }
`,
		"a.go": `package p

func A() int { return shared }
`,
	}
	rule := &goxlint.Rule{
		Meta: goxlint.Meta{Name: "uses", NeedsTypes: true},
		AnalyzePackage: func(pkg *goxlint.PackageContext) {
			var obj types.Object
			for _, file := range pkg.Files {
				if found := findDef(file, "shared"); found != nil {
					obj = found
				}
			}
			var parts []string
			for _, use := range pkg.UsesOf(obj) {
				parts = append(parts, fmt.Sprintf("%s:%t", use.Ctx.Filename, use.Ctx.Enabled))
			}
			for _, file := range pkg.Files {
				if file.Enabled {
					file.Report(goxlint.Diagnostic{Node: file.File.Name, Message: strings.Join(parts, " ")})
					return
				}
			}
		},
	}
	parsed, out := lintTyped(t, files, func(name string) []ConfiguredRule {
		if name == "b.go" {
			return nil
		}
		return []ConfiguredRule{{ID: "t/uses", Plugin: "t", Rule: rule, Severity: Error}}
	})
	var diags []Diagnostic
	for _, fileDiags := range out {
		diags = append(diags, fileDiags...)
	}
	requireMessages(t, diags, []string{"a.go:true b.go:false"})
	builds := map[string]int{}
	for _, file := range parsed {
		builds[file.Filename] = file.uses.builds
	}
	if builds["a.go"] != 1 || builds["b.go"] != 1 {
		t.Fatalf("builds = %v, want 1 for each file", builds)
	}
}

func TestObjectOfExpr(t *testing.T) {
	src := `package p

func f() int {
	x := 1
	y := x
	n := 1 + 2
	return y + n
}

type S struct{ F int }

func g() int {
	s := S{F: 1}
	return (s.F)
}
`
	rule := &goxlint.Rule{
		Meta: goxlint.Meta{Name: "obj", NeedsTypes: true},
		Create: func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(goxlint.On(func(file *ast.File) {
				bad := ""
				ast.Inspect(file, func(n ast.Node) bool {
					switch e := n.(type) {
					case *ast.Ident:
						if ctx.ObjectOfExpr(e) != ctx.ObjectOf(e) {
							bad = "ident " + e.Name
						}
					case *ast.SelectorExpr:
						got := ctx.ObjectOfExpr(e)
						if got == nil || got != ctx.ObjectOf(e.Sel) {
							bad = "selector"
						}
					case *ast.ParenExpr:
						if ctx.ObjectOfExpr(e) != ctx.ObjectOfExpr(e.X) {
							bad = "paren"
						}
					case *ast.BinaryExpr:
						if ctx.ObjectOfExpr(e) != nil {
							bad = "binary"
						}
					}
					return bad == ""
				})
				msg := "ok"
				if bad != "" {
					msg = bad
				}
				ctx.Report(goxlint.Diagnostic{Node: file.Name, Message: msg})
			}))
		},
	}
	_, diags := lintOne(t, src, rule)
	requireMessages(t, diags, []string{"ok"})
}

func TestCalleeFuncInstantiationAndMethodExpr(t *testing.T) {
	src := `package p

func id[T any](v T) T { return v }

func pair[A, B any](a A, b B) A { return a }

type T struct{}

func (T) M(x int) int { return x }

type Alias = T

type Box[A any] struct{}

func (Box[A]) Get(v A) A { return v }

func calls() int {
	id[int](1)
	pair[int, string](1, "a")
	(id[int])(2)
	id(3)
	_ = T.M(T{}, 1)
	_ = (*T).M(&T{}, 2)
	_ = Alias.M(T{}, 3)
	var b Box[int]
	_ = b.Get(4)
	_ = func() int { return 1 }()
	return Box[int].Get(b, 5)
}
`
	rule := &goxlint.Rule{
		Meta: goxlint.Meta{Name: "callee", NeedsTypes: true},
		Create: func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(goxlint.On(func(call *ast.CallExpr) {
				fn := ctx.CalleeFunc(call)
				if fn == nil {
					ctx.Report(goxlint.Diagnostic{Node: call, Message: "nil"})
					return
				}
				kind := "func"
				if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
					kind = "method"
				}
				msg := fn.Name() + " " + kind
				if ctx.IsPkgFunc(call, ctx.Pkg.Path(), fn.Name()) {
					msg += " pkg"
				}
				ctx.Report(goxlint.Diagnostic{Node: call, Message: msg})
			}))
		},
	}
	_, diags := lintOne(t, src, rule)
	requireMessages(t, diags, []string{
		"id func pkg",
		"pair func pkg",
		"id func pkg",
		"id func pkg",
		"M method",
		"M method",
		"M method",
		"Get method",
		"nil",
		"Get method",
	})
}

func nodeKind(n ast.Node) string {
	if n == nil {
		return "nil"
	}
	return strings.TrimPrefix(fmt.Sprintf("%T", n), "*ast.")
}

func findDef(ctx *goxlint.Context, name string) types.Object {
	var obj types.Object
	ast.Inspect(ctx.File, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || id.Name != name || obj != nil || ctx.TypesInfo == nil {
			return true
		}
		obj = ctx.TypesInfo.Defs[id]
		return true
	})
	return obj
}

func requireMessages(t *testing.T, diags []Diagnostic, want []string) {
	t.Helper()
	got := make([]string, 0, len(diags))
	for _, d := range diags {
		got = append(got, d.Message)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("messages:\n got %q\nwant %q", got, want)
	}
}

func lintOne(t *testing.T, src string, rules ...*goxlint.Rule) ([]File, []Diagnostic) {
	t.Helper()
	crs := make([]ConfiguredRule, len(rules))
	for i, rule := range rules {
		crs[i] = ConfiguredRule{ID: "t/" + rule.Meta.Name, Plugin: "t", Rule: rule, Severity: Error}
	}
	files, out := lintTyped(t, map[string]string{"p.go": src}, func(string) []ConfiguredRule { return crs })
	return files, out[0]
}

func lintTyped(t *testing.T, files map[string]string, rulesFor func(string) []ConfiguredRule) ([]File, [][]Diagnostic) {
	t.Helper()
	fset := token.NewFileSet()
	var parsed []File
	var syntax []*ast.File
	for _, name := range slices.Sorted(maps.Keys(files)) {
		af, err := parser.ParseFile(fset, name, files[name], parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		parsed = append(parsed, File{Fset: fset, AST: af, Filename: name, Source: []byte(files[name])})
		syntax = append(syntax, af)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	conf := types.Config{Importer: importer.Default(), Error: func(err error) {
		t.Errorf("type error: %v", err)
	}}
	pkg, err := conf.Check("p", fset, syntax, info)
	if t.Failed() {
		t.FailNow()
	}
	if err != nil {
		t.Fatal(err)
	}
	for i := range parsed {
		parsed[i].Pkg, parsed[i].Info = pkg, info
	}
	return parsed, LintAll(parsed, rulesFor, LintOptions{})
}
