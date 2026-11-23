package goxlint_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/egoisutolabs/goxlint"
	"github.com/egoisutolabs/goxlint/goxlinttest"
)

func TestCompilePatternError(t *testing.T) {
	_, err := goxlint.CompilePattern("fmt.Println(")
	if err == nil || !strings.Contains(err.Error(), "pattern:1:13:") {
		t.Fatalf("got %v", err)
	}
	_, err = goxlint.CompilePattern("f($*)")
	if err == nil || !strings.Contains(err.Error(), "pattern:1:3:") {
		t.Fatalf("got %v", err)
	}
}

func TestMustPatternPanics(t *testing.T) {
	defer func() {
		err, ok := recover().(error)
		if !ok || !strings.Contains(err.Error(), "pattern:1:") {
			t.Fatalf("panic = %v", err)
		}
	}()
	goxlint.MustPattern("fmt.Println(")
	t.Fatal("MustPattern did not panic")
}

func TestInvalidException(t *testing.T) {
	p, err := goxlint.CompilePattern("f($x)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.CompileExcept("f(")
	if err == nil || !strings.Contains(err.Error(), "exception: pattern:1:3:") {
		t.Fatalf("got %v", err)
	}
	defer func() {
		err, ok := recover().(error)
		if !ok || !strings.Contains(err.Error(), "pattern:1:") {
			t.Fatalf("panic = %v", err)
		}
	}()
	goxlint.MustPattern("f($x)").Except("f(")
}

func TestExceptKeepsOriginal(t *testing.T) {
	base := goxlint.MustPattern(`f($*xs)`)
	filtered := base.Except(`f()`)
	goxlinttest.Run(t, hitRule(false, filtered), goxlinttest.Cases{
		Valid: []goxlinttest.Case{{Name: "zero", Code: `
func f(xs ...int) {}

func g() { f() }`}},
		Invalid: []goxlinttest.Case{{Name: "one", Code: `
func f(xs ...int) {}

func g() { f(1) }`, Errors: []goxlinttest.Error{{MessageID: "hit"}}}},
	})
	goxlinttest.Run(t, hitRule(false, base), goxlinttest.Cases{
		Invalid: []goxlinttest.Case{{Name: "zero still matches", Code: `
func f(xs ...int) {}

func g() { f() }`, Errors: []goxlinttest.Error{{MessageID: "hit"}}}},
	})
}

func TestExceptSharedName(t *testing.T) {
	// $x in the exception is the left operand captured by the main pattern.
	shared := goxlint.MustPattern(`$x == $y`).Except(`$x == $x`)
	// $x is local to the exception. It removes a match whose operands are equal.
	local := goxlint.MustPattern(`$_ == $_`).Except(`$x == $x`)
	for _, p := range []*goxlint.Pattern{shared, local} {
		goxlinttest.Run(t, hitRule(false, p), goxlinttest.Cases{
			Valid: []goxlinttest.Case{{
				Name: "equal",
				Code: `func f(a int) bool { return a == a }`,
			}},
			Invalid: []goxlinttest.Case{{
				Name:   "different",
				Code:   `func f(a, b int) bool { return a == b }`,
				Errors: []goxlinttest.Error{{MessageID: "hit"}},
			}},
		})
	}
}

func TestTypedPackageSelector(t *testing.T) {
	rule := hitRule(true, goxlint.MustPattern(`fmt.Println($*_)`))
	goxlinttest.Run(t, rule, goxlinttest.Cases{
		Valid: []goxlinttest.Case{{
			Name: "local value",
			Code: `
type fmtT struct{}

func (fmtT) Println(v ...int) {}

func f(fmt fmtT) { fmt.Println(1) }`,
		}},
		Invalid: []goxlinttest.Case{
			{
				Name: "renamed import",
				Code: `
import f "fmt"

func g() { f.Println(1) }`,
				Errors: []goxlinttest.Error{{MessageID: "hit"}},
			},
			{
				Name: "direct import",
				Code: `
import "fmt"

func g() { fmt.Println(1) }`,
				Errors: []goxlinttest.Error{{MessageID: "hit"}},
			},
		},
	})
}

func TestUntypedPackageSelector(t *testing.T) {
	rule := hitRule(false, goxlint.MustPattern(`fmt.Println($*_)`))
	goxlinttest.Run(t, rule, goxlinttest.Cases{
		Valid: []goxlinttest.Case{{
			Name: "renamed import",
			Code: `
import f "fmt"

func g() { f.Println(1) }`,
		}},
		Invalid: []goxlinttest.Case{
			{
				Name: "written name",
				Code: `
import "fmt"

func g() { fmt.Println(1) }`,
				Errors: []goxlinttest.Error{{MessageID: "hit"}},
			},
			{
				Name: "local value with that name",
				Code: `
type fmtT struct{}

func (fmtT) Println(v ...int) {}

func f(fmt fmtT) { fmt.Println(1) }`,
				Errors: []goxlinttest.Error{{MessageID: "hit"}},
			},
		},
	})
}

func TestStatementPatternInsideBlock(t *testing.T) {
	rule := hitRule(false, goxlint.MustPattern(`if $err != nil { return $err }`))
	goxlinttest.Run(t, rule, goxlinttest.Cases{
		Valid: []goxlinttest.Case{{
			Name: "different return",
			Code: `
func f(err error) error {
	if err != nil {
		return nil
	}
	return nil
}`,
		}},
		Invalid: []goxlinttest.Case{{
			Name: "inside function",
			Code: `
func f(err error) error {
	if err != nil {
		return err
	}
	return nil
}`,
			Errors: []goxlinttest.Error{{MessageID: "hit", Line: 3}},
		}},
	})
}

func TestReplaceMatch(t *testing.T) {
	pat := goxlint.MustPattern(`fmt.Println($*args)`)
	rule := &goxlint.Rule{
		Meta: goxlint.Meta{
			Name:     "rewrite",
			Fixable:  goxlint.FixSafe,
			Messages: map[string]string{"hit": "hit"},
		},
		Create: func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(goxlint.OnMatch(pat, func(m goxlint.Match) {
				ctx.Report(goxlint.Diagnostic{
					Node:      m.Node,
					MessageID: "hit",
					Fix: &goxlint.Fix{
						Kind:  goxlint.FixSafe,
						Edits: []goxlint.Edit{ctx.ReplaceMatch(m, "log.Println($*args)")},
					},
				})
			}))
		},
	}
	goxlinttest.Run(t, rule, goxlinttest.Cases{
		Invalid: []goxlinttest.Case{
			{
				Name:   "many",
				Code:   "import \"fmt\"\nfunc f() { fmt.Println(1, 2) }",
				Errors: []goxlinttest.Error{{MessageID: "hit"}},
				Output: "import \"fmt\"\nfunc f() { log.Println(1, 2) }",
			},
			{
				Name:   "zero",
				Code:   "import \"fmt\"\nfunc f() { fmt.Println() }",
				Errors: []goxlinttest.Error{{MessageID: "hit"}},
				Output: "import \"fmt\"\nfunc f() { log.Println() }",
			},
		},
	})
}

func TestReplaceMatchParenPattern(t *testing.T) {
	pat := goxlint.MustPattern(`sink($x)`)
	rule := &goxlint.Rule{
		Meta: goxlint.Meta{
			Name:     "rewrite",
			Fixable:  goxlint.FixSafe,
			Messages: map[string]string{"hit": "hit"},
		},
		Create: func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(goxlint.OnMatch(pat, func(m goxlint.Match) {
				ctx.Report(goxlint.Diagnostic{
					Node:      m.Node,
					MessageID: "hit",
					Fix:       &goxlint.Fix{Kind: goxlint.FixSafe, Edits: []goxlint.Edit{ctx.ReplaceMatch(m, "kept($x)")}},
				})
			}))
		},
	}
	goxlinttest.Run(t, rule, goxlinttest.Cases{
		Invalid: []goxlinttest.Case{{
			Code:   "func f(a, b int) { sink((a + b)) }\nfunc sink(int) {}",
			Errors: []goxlinttest.Error{{MessageID: "hit"}},
			Output: "func f(a, b int) { kept((a + b)) }\nfunc sink(int) {}",
		}},
	})
}

func TestWildNotCaptured(t *testing.T) {
	pat := goxlint.MustPattern(`f($_, $x)`)
	rule := &goxlint.Rule{
		Meta: goxlint.Meta{Name: "cap", Messages: map[string]string{"hit": "hit"}},
		Create: func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(goxlint.OnMatch(pat, func(m goxlint.Match) {
				if _, ok := m.Captures["_"]; ok || len(m.Captures["x"]) != 1 {
					panic("bad captures")
				}
				ctx.Report(goxlint.Diagnostic{Node: m.Node, MessageID: "hit"})
			}))
		},
	}
	goxlinttest.Run(t, rule, goxlinttest.Cases{
		Invalid: []goxlinttest.Case{{
			Code: `
func f(a, b int) {}

func g() { f(1, 2) }`,
			Errors: []goxlinttest.Error{{MessageID: "hit"}},
		}},
	})
}

func TestParallelTypedMatches(t *testing.T) {
	rule := hitRule(true, goxlint.MustPattern(`fmt.Println($*_)`))
	goxlinttest.RunPackage(t, rule, goxlinttest.PackageCases{
		Invalid: []goxlinttest.PackageCase{{
			Files: map[string]string{
				"a.go": "import f \"fmt\"\nfunc A() { f.Println(1) }\n",
				"b.go": "type fmtT struct{}\nfunc (fmtT) Println(v ...int) {}\nfunc B(fmt fmtT) { fmt.Println(2) }\n",
			},
			Errors: []goxlinttest.PackageError{{Filename: "a.go", MessageID: "hit"}},
		}},
	})
}

func TestReplaceMatchUnknownCapture(t *testing.T) {
	fset := token.NewFileSet()
	src := []byte("package p\nvar x = 1\n")
	file, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx := goxlint.NewContext(nil, "t/t", fset, file, "p.go", src, nil, nil, nil, nil, func(goxlint.Diagnostic) {}, nil)
	defer func() {
		err, ok := recover().(error)
		if !ok || !strings.Contains(err.Error(), "unknown capture") {
			t.Fatalf("panic = %v", err)
		}
	}()
	ctx.ReplaceMatch(goxlint.Match{Node: file.Name, Captures: map[string][]ast.Node{}}, "g($x)")
}

func TestReplaceMatchDollarEscape(t *testing.T) {
	fset := token.NewFileSet()
	src := []byte("package p\nvar x = 1\n")
	file, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx := goxlint.NewContext(nil, "t/t", fset, file, "p.go", src, nil, nil, nil, nil, func(goxlint.Diagnostic) {}, nil)
	edit := ctx.ReplaceMatch(goxlint.Match{
		Node:     file.Name,
		Captures: map[string][]ast.Node{"x": {file.Name}},
	}, "$$name $x")
	if edit.NewText != "$name p" {
		t.Fatalf("got %q", edit.NewText)
	}
}

func hitRule(types bool, p *goxlint.Pattern) *goxlint.Rule {
	return &goxlint.Rule{
		Meta: goxlint.Meta{
			Name:       "hit",
			NeedsTypes: types,
			Messages:   map[string]string{"hit": "hit"},
		},
		Create: func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(goxlint.OnMatch(p, func(m goxlint.Match) {
				ctx.Report(goxlint.Diagnostic{Node: m.Node, MessageID: "hit"})
			}))
		},
	}
}
