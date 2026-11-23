package goxlinttest_test

import (
	"go/ast"
	"os"
	"testing"

	"github.com/egoisutolabs/goxlint"
	"github.com/egoisutolabs/goxlint/goxlinttest"
)

func renameBadRule() *goxlint.Rule {
	return &goxlint.Rule{
		Meta: goxlint.Meta{Fixable: goxlint.FixSafe},
		Create: func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(goxlint.On(func(id *ast.Ident) {
				if id.Name != "bad" {
					return
				}
				ctx.Report(goxlint.Diagnostic{
					Node: id, Message: "rename bad",
					Fix: &goxlint.Fix{Kind: goxlint.FixSafe, Edits: []goxlint.Edit{goxlint.ReplaceNode(id, "good")}},
				})
			}))
		},
	}
}

func TestRunAcceptsValidCodeAndFixes(t *testing.T) {
	t.Parallel()
	goxlinttest.Run(t, renameBadRule(), goxlinttest.Cases{
		Valid: []goxlinttest.Case{
			{Name: "clean", Code: "var good = 1"},
			{Name: "comment only", Code: "// no package clause"},
		},
		Invalid: []goxlinttest.Case{{
			Name:   "renames",
			Code:   "var bad = 1",
			Errors: []goxlinttest.Error{{Message: "rename bad", Line: 1, Column: 5}},
			Output: "var good = 1",
		}},
	})
}

func TestRunFailures(t *testing.T) {
	if scenario := os.Getenv("GOXLINT_RUN_FAILURE"); scenario != "" {
		runRunFailure(t, scenario)
		return
	}
	runIsolatedFailures(t, "TestRunFailures", "GOXLINT_RUN_FAILURE", []struct{ name, message string }{
		{"unexpected diagnostic", "unexpected diagnostic at 1:5: rename bad"},
		{"no errors", "invalid case must list Errors"},
		{"missing fix", "expected a fix producing:\nvar good = 1\nbut the rule reported no fix"},
		{"unset output", "rule reported a fix; set Output to:\nvar good = 1"},
		{"output mismatch", "fixed output mismatch\n--- got ---\nvar good = 1\n--- want ---\nvar other = 1"},
		{"parse", "parse error:"},
		{"unclosed template", `message "A 1 C" does not come from message ID "broken"`},
		{"middle placeholder", `message "A 1 C" does not come from message ID "two"`},
	})
}

func runRunFailure(t *testing.T, scenario string) {
	t.Helper()
	rule := renameBadRule()
	c := goxlinttest.Case{
		Code:   "var bad = 1",
		Errors: []goxlinttest.Error{{Message: "rename bad", Line: 1, Column: 5}},
	}
	switch scenario {
	case "unexpected diagnostic":
		goxlinttest.Run(t, rule, goxlinttest.Cases{Valid: []goxlinttest.Case{{Code: c.Code}}})
		return
	case "no errors":
		c.Errors = nil
	case "missing fix":
		rule = &goxlint.Rule{Create: func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(goxlint.On(func(id *ast.Ident) {
				if id.Name == "bad" {
					ctx.Report(goxlint.Diagnostic{Node: id, Message: "rename bad"})
				}
			}))
		}}
		c.Output = "var good = 1"
	case "unset output":
		c.Output = ""
	case "output mismatch":
		c.Output = "var other = 1"
	case "parse":
		c.Code = "func {"
	case "unclosed template", "middle placeholder":
		messageID := "broken"
		messages := map[string]string{"broken": "Hello {{name"}
		if scenario == "middle placeholder" {
			messageID = "two"
			messages = map[string]string{"two": "A {{x}} B {{y}} C"}
		}
		rule = &goxlint.Rule{
			Meta: goxlint.Meta{Messages: messages},
			Create: func(ctx *goxlint.Context) goxlint.Visitor {
				return goxlint.Visit(goxlint.On(func(id *ast.Ident) {
					if id.Name == "bad" {
						ctx.Report(goxlint.Diagnostic{Node: id, Message: "A 1 C"})
					}
				}))
			},
		}
		c.Errors = []goxlinttest.Error{{MessageID: messageID}}
	default:
		t.Fatalf("unknown failure scenario %q", scenario)
	}
	goxlinttest.Run(t, rule, goxlinttest.Cases{Invalid: []goxlinttest.Case{c}})
}
