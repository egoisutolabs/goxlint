package engine

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/egoisutolabs/goxlint"
)

func TestOnMatchSharesTheWalk(t *testing.T) {
	var files int
	var seq []string
	pat := goxlint.MustPattern(`fmt.Println($*_)`)
	rule := &goxlint.Rule{
		Meta: goxlint.Meta{Name: "walk"},
		Create: func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(
				goxlint.On(func(*ast.File) { files++ }),
				goxlint.On(func(call *ast.CallExpr) {
					sel, _ := ast.Unparen(call.Fun).(*ast.SelectorExpr)
					seq = append(seq, "on:"+sel.Sel.Name)
				}),
				goxlint.OnMatch(pat, func(goxlint.Match) { seq = append(seq, "match") }),
			)
		},
	}
	src := "package p\nimport \"fmt\"\nfunc f() {\n\tfmt.Println(1)\n\tfmt.Printf(\"%d\", 1)\n\tfmt.Println(2)\n}\n"
	lintSource(t, src, rule)
	if files != 1 {
		t.Fatalf("file visits = %d, want 1", files)
	}
	want := []string{"on:Println", "match", "on:Printf", "on:Println", "match"}
	if len(seq) != len(want) {
		t.Fatalf("sequence = %v, want %v", seq, want)
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("sequence = %v, want %v", seq, want)
		}
	}
}

func BenchmarkOnMatchSingleWalk(b *testing.B) {
	pat := goxlint.MustPattern(`fmt.Println($*_)`)
	src := "package p\nimport \"fmt\"\nfunc f() {\n\tfmt.Println(1)\n\tfmt.Println(2)\n}\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, parser.SkipObjectResolution)
	if err != nil {
		b.Fatal(err)
	}
	var files int
	rule := &goxlint.Rule{
		Create: func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(
				goxlint.On(func(*ast.File) { files++ }),
				goxlint.OnMatch(pat, func(goxlint.Match) {}),
			)
		},
	}
	rules := []ConfiguredRule{{ID: "t/walk", Plugin: "t", Rule: rule, Severity: Error}}
	input := File{Fset: fset, AST: file, Filename: "x.go", Source: []byte(src)}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		files = 0
		LintFile(input, rules, LintOptions{})
	}
	if files != 1 {
		b.Fatalf("walks = %d, want 1", files)
	}
	b.ReportMetric(float64(files), "walks/op")
}
