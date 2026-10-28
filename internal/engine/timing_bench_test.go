package engine_test

import (
	"go/ast"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoisutolabs/goxlint"
	"github.com/egoisutolabs/goxlint/internal/engine"
)

// Set GOXLINT_BENCH_ROOT to use the same repository snapshot across revisions.
func BenchmarkLintAllRepository(b *testing.B) {
	root := os.Getenv("GOXLINT_BENCH_ROOT")
	if root == "" {
		root = filepath.Join("..", "..")
	}
	files, errs := engine.Load(engine.LoadOptions{Dir: root, Patterns: []string{"./..."}})
	if len(errs) != 0 {
		b.Fatal(errs)
	}
	rule := &goxlint.Rule{
		Create: func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(goxlint.On(func(n *ast.Ident) {
				if n.Name == "err" {
					ctx.Report(goxlint.Diagnostic{Node: n, Message: "benchmark diagnostic"})
				}
			}))
		},
	}
	rules := []engine.ConfiguredRule{{ID: "benchmark/error-identifiers", Rule: rule, Severity: engine.Warn}}
	rulesFor := func(string) []engine.ConfiguredRule { return rules }
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		engine.LintAll(files, rulesFor, engine.LintOptions{})
	}
}
