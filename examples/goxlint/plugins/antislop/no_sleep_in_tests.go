package antislop

import (
	"go/ast"

	"github.com/egoisutolabs/goxlint"
)

// timeSleep matches time.Sleep with any argument list. The previous file
// tested the call with IsPkgFunc inside a CallExpr handler and was 43 lines.
// This file is 46 lines. The pattern replaces that type test.
var timeSleep = goxlint.MustPattern(`time.Sleep($*_)`)

// NoSleepInTests rejects time.Sleep in _test.go files, where it is used to
// wait for something to happen and makes tests slow and flaky. Sleeps inside
// a testing/synctest bubble are allowed, because time there is virtual.
var NoSleepInTests = &goxlint.Rule{
	Meta: goxlint.Meta{
		Name:        "no-sleep-in-tests",
		Description: "Disallow time.Sleep in tests outside synctest bubbles.",
		Category:    goxlint.Suspicious,
		NeedsTypes:  true,
		Messages: map[string]string{
			"sleep": "`time.Sleep` in a test guesses how long something takes, which makes the test slow and flaky.",
		},
	},
	Create: func(ctx *goxlint.Context) goxlint.Visitor {
		if !ctx.IsTestFile() {
			return nil
		}
		return goxlint.Visit(
			goxlint.OnMatch(timeSleep, func(m goxlint.Match) {
				for _, a := range ctx.Ancestors() {
					if outer, ok := a.(*ast.CallExpr); ok && ctx.IsPkgFunc(outer, "testing/synctest", "Test", "Run") {
						return
					}
				}
				ctx.Report(goxlint.Diagnostic{
					Node:      m.Node,
					MessageID: "sleep",
					Help:      "Wait on a channel or sync.WaitGroup, poll the condition with a deadline, or run the test in a testing/synctest bubble.",
				})
			}),
		)
	},
}
