package engine

import (
	"go/ast"
	"time"
)

// Install wrappers only when timing is enabled, so normal walks call handlers
// directly. Each map belongs to one file worker; LintAll merges after the join.
func timedHandler(fn func(ast.Node), id string, timings map[string]time.Duration) func(ast.Node) {
	return func(n ast.Node) {
		start := time.Now()
		defer func() { timings[id] += time.Since(start) }()
		fn(n)
	}
}
