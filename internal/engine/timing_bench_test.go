package engine_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoisutolabs/goxlint/examples/goxlint/plugins/antislop"
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
	var rules []engine.ConfiguredRule
	for _, rule := range antislop.Plugin.Rules {
		if !rule.Meta.NeedsTypes {
			rules = append(rules, engine.ConfiguredRule{ID: "anti-slop/" + rule.Meta.Name, Rule: rule, Severity: engine.Warn})
		}
	}
	rulesFor := func(string) []engine.ConfiguredRule { return rules }
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		engine.LintAll(files, rulesFor, engine.LintOptions{})
	}
}
