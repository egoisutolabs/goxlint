package engine

import (
	"fmt"
	"go/ast"
	"reflect"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/egoisutolabs/goxlint"
)

func TestTimingRulePhases(t *testing.T) {
	for _, phase := range []string{"create", "enter", "exit", "interface enter", "interface exit", "package", "combined", "panic create", "panic handler", "panic package"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				f := packageFile(t, "p/a.go", "package p\nfunc f() { return }\n")
				var elapsed time.Duration
				work := func() {
					start := time.Now()
					time.Sleep(time.Millisecond)
					elapsed += time.Since(start)
				}
				rule := &goxlint.Rule{}
				switch phase {
				case "create", "panic create":
					rule.Create = func(*goxlint.Context) goxlint.Visitor {
						work()
						if phase == "panic create" {
							panic("test")
						}
						return nil
					}
				case "enter", "exit", "panic handler":
					rule.Create = func(*goxlint.Context) goxlint.Visitor {
						fn := func(*ast.Ident) {
							work()
							if phase == "panic handler" {
								panic("test")
							}
						}
						if phase == "exit" {
							return goxlint.Visit(goxlint.OnExit(fn))
						}
						return goxlint.Visit(goxlint.On(fn))
					}
				case "interface enter", "interface exit":
					rule.Create = func(*goxlint.Context) goxlint.Visitor {
						fn := func(ast.Stmt) { work() }
						if phase == "interface exit" {
							return goxlint.Visit(goxlint.OnExit(fn))
						}
						return goxlint.Visit(goxlint.On(fn))
					}
				case "package", "panic package":
					rule.AnalyzePackage = func(*goxlint.PackageContext) {
						work()
						if phase == "panic package" {
							panic("test")
						}
					}
				case "combined":
					rule.Create = func(*goxlint.Context) goxlint.Visitor {
						work()
						return goxlint.Visit(goxlint.On(func(*ast.Ident) { work() }))
					}
					rule.AnalyzePackage = func(*goxlint.PackageContext) { work() }
				}
				var measured map[string]time.Duration
				calls := 0
				diags := LintFile(f, []ConfiguredRule{{ID: "t/measured", Rule: rule, Severity: Warn}}, LintOptions{
					Timing: true, OnTiming: func(timings map[string]time.Duration) { calls++; measured = timings },
				})
				if calls != 1 || len(measured) != 1 || elapsed <= 0 || measured["t/measured"] != elapsed {
					t.Fatalf("missing %s time: callbacks=%d, measured=%v, work=%s", phase, calls, measured, elapsed)
				}
				wantErrors := 0
				if phase == "panic create" || phase == "panic handler" || phase == "panic package" {
					wantErrors = 1
				}
				if len(diags) != wantErrors || (wantErrors == 1 && !diags[0].internal) {
					t.Fatalf("panic handling changed: %+v", diags)
				}
			})
		})
	}
}

func TestTimingConcurrentAggregation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var files []File
		for i := range 24 {
			files = append(files, packageFile(t, fmt.Sprintf("p%d/f%d.go", i/2, i), "package p\nvar value = 1\n"))
		}
		var calls atomic.Int64
		var elapsed atomic.Int64
		work := func() {
			start := time.Now()
			time.Sleep(time.Millisecond)
			elapsed.Add(int64(time.Since(start)))
			calls.Add(1)
		}
		rule := &goxlint.Rule{
			Create: func(*goxlint.Context) goxlint.Visitor {
				work()
				return goxlint.Visit(goxlint.On(func(n *ast.Ident) {
					if n.Name == "value" {
						work()
					}
				}))
			},
			AnalyzePackage: func(*goxlint.PackageContext) { work() },
		}
		rulesFor := func(string) []ConfiguredRule { return []ConfiguredRule{{ID: "t/shared", Rule: rule, Severity: Warn}} }
		for range 2 {
			calls.Store(0)
			elapsed.Store(0)
			callbacks := 0
			LintAll(files, rulesFor, LintOptions{Timing: true, OnTiming: func(timings map[string]time.Duration) {
				callbacks++
				if calls.Load() != 60 || len(timings) != 1 || timings["t/shared"] != time.Duration(elapsed.Load()) {
					t.Errorf("incomplete aggregation: calls=%d, timings=%v, work=%s", calls.Load(), timings, time.Duration(elapsed.Load()))
				}
			}})
			if callbacks != 1 {
				t.Fatalf("got %d callbacks", callbacks)
			}
		}
	})
}

func TestTimingSkipsUnrunRules(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		f := packageFile(t, "p/a.go", "package p\n")
		f.Incomplete = incomplete
		rules := []ConfiguredRule{
			{ID: "t/off", Severity: Off, Rule: &goxlint.Rule{Create: func(*goxlint.Context) goxlint.Visitor { t.Fatal("off rule ran"); return nil }}},
			{ID: "t/typed", Severity: Warn, Rule: &goxlint.Rule{Meta: goxlint.Meta{NeedsTypes: true}, Create: func(*goxlint.Context) goxlint.Visitor { t.Fatal("typed rule ran"); return nil }}},
			{ID: "t/package", Severity: Warn, Rule: &goxlint.Rule{AnalyzePackage: func(*goxlint.PackageContext) {}}},
		}
		LintFile(f, rules, LintOptions{Timing: true, OnTiming: func(timings map[string]time.Duration) {
			want := 1
			if incomplete {
				want = 0
			}
			_, hasPackage := timings["t/package"]
			if len(timings) != want || hasPackage == incomplete {
				t.Errorf("unexpected timing entries: %v", timings)
			}
		}})
	}
}

func TestTimingOptionalAndDiagnosticsUnchanged(t *testing.T) {
	f := packageFile(t, "p/a.go", "package p\nvar bad = 1\n")
	rules := []ConfiguredRule{{ID: "t/ident", Rule: identRule, Severity: Warn}}
	want := LintFile(f, rules, LintOptions{OnTiming: func(map[string]time.Duration) { t.Fatal("timing disabled") }})
	got := LintFile(f, rules, LintOptions{Timing: true})
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("timing changed diagnostics: got %+v, want %+v", got, want)
	}
	calls := 0
	LintAll(nil, nil, LintOptions{Timing: true, OnTiming: func(timings map[string]time.Duration) {
		calls++
		if timings == nil || len(timings) != 0 {
			t.Fatalf("unexpected empty-run timings: %v", timings)
		}
	}})
	if calls != 1 {
		t.Fatalf("got %d callbacks", calls)
	}
}
