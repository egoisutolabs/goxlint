package engine

import (
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"

	"github.com/egoisutolabs/goxlint"
)

func suppressionFile(t *testing.T, src string) File {
	t.Helper()
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	return File{Fset: fset, AST: af, Filename: "x.go", Source: []byte(src)}
}

func TestUnusedSuppressions(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		unused    []string
		other     int
	}{
		{name: "own line", src: "package p\nvar bad = 1 //goxlint:ignore t/ident reason\n"},
		{name: "next line", src: "package p\n//goxlint:ignore t/ident reason\nvar bad = 1\n"},
		{name: "plugin", src: "package p\n//goxlint:ignore t\nvar bad = 1\n"},
		{name: "wildcard", src: "package p\n//goxlint:ignore *\nvar bad = 1\n"},
		{name: "file", src: "//goxlint:ignore-file t/ident\npackage p\nvar bad = 1\nvar _ = bad\n"},
		{name: "no diagnostics", src: "package p\n//goxlint:ignore t/ident reason\nvar good = 1\n", unused: []string{"t/ident"}},
		{name: "unused file", src: "//goxlint:ignore-file t\npackage p\n", unused: []string{"t"}},
		{name: "unused wildcard", src: "//goxlint:ignore-file *\npackage p\n", unused: []string{"*"}},
		{name: "unknown rule", src: "package p\n//goxlint:ignore t/typo\nvar bad = 1\n", unused: []string{"t/typo"}, other: 1},
		{name: "unknown plugin", src: "package p\n//goxlint:ignore typo\nvar bad = 1\n", unused: []string{"typo"}, other: 1},
		{name: "partial list", src: "package p\n//goxlint:ignore t/ident,t/typo explanation\nvar bad = 1\n", unused: []string{"t/typo"}},
		{name: "file partial list", src: "//goxlint:ignore-file t/typo,t/ident\npackage p\nvar bad = 1\n", unused: []string{"t/typo"}},
		{name: "duplicate target", src: "package p\n//goxlint:ignore t/ident,t/ident\nvar bad = 1\n", unused: []string{"t/ident"}},
		{name: "duplicate directive", src: "//goxlint:ignore-file t\npackage p\n//goxlint:ignore t/ident\nvar bad = 1\n", unused: []string{"t/ident"}},
		{name: "redundant wildcard", src: "package p\n//goxlint:ignore t/ident,*\nvar bad = 1\n", unused: []string{"*"}},
		{name: "out of scope", src: "package p\n//goxlint:ignore t/ident\n\nvar bad = 1\n", unused: []string{"t/ident"}, other: 1},
		{name: "does not suppress audit", src: "//goxlint:ignore-file *\npackage p\n//goxlint:ignore t/typo\nvar bad = 1\n", unused: []string{"t/typo"}},
		{name: "cannot suppress itself", src: "//goxlint:ignore-file goxlint/unused-suppression\npackage p\n", unused: []string{"goxlint/unused-suppression"}},
		{name: "multiple findings one target", src: "package p\n//goxlint:ignore t/ident\nvar bad = bad\n"},
		{name: "extra whitespace and CRLF", src: "package p\r\n//goxlint:ignore   t/typo reason\r\n", unused: []string{"t/typo"}},
		{name: "not a directive", src: "package p\n// goxlint:ignore t/ident\n/* //goxlint:ignore t/ident */\nvar bad = 1\n", other: 1},
		{name: "line directives", src: "package p\n//goxlint:ignore t/ident\n//line other.go:100\nvar bad = 1\n", unused: []string{"t/ident"}, other: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := suppressionFile(t, tc.src)
			rules := []ConfiguredRule{{ID: "t/ident", Plugin: "t", Rule: identRule, Severity: Error}}
			diags := LintFile(f, rules, LintOptions{ReportUnusedSuppressions: Warn})
			var unused []string
			other := 0
			for _, d := range diags {
				if d.RuleID != UnusedSuppressionRuleID {
					other++
					continue
				}
				target := string(f.Source[d.Start.Offset:d.End.Offset])
				unused = append(unused, target)
				if d.Severity != Warn || !strings.Contains(d.Message, target) || d.Help == "" || d.Fix != nil {
					t.Errorf("unexpected audit diagnostic: %+v", d)
				}
				if d.Filename != "x.go" || d.Start.Line < 1 || d.Start.Column < 1 {
					t.Errorf("invalid audit location: %+v", d)
				}
			}
			if !slices.Equal(unused, tc.unused) || other != tc.other {
				t.Fatalf("want unused %v and %d other diagnostics; got unused %v and %d: %+v", tc.unused, tc.other, unused, other, diags)
			}
		})
	}
}

func TestUnusedSuppressionSeverity(t *testing.T) {
	f := suppressionFile(t, "//goxlint:ignore-file t/ident\npackage p\n")
	for _, severity := range []Severity{Off, Warn, Error} {
		diags := LintFile(f, nil, LintOptions{ReportUnusedSuppressions: severity})
		if severity == Off {
			if len(diags) != 0 {
				t.Fatalf("disabled audit returned diagnostics: %+v", diags)
			}
		} else if len(diags) != 1 || diags[0].Severity != severity {
			t.Fatalf("want one %s diagnostic, got %+v", severity, diags)
		}
	}
}

func TestUnusedSuppressionsSkipIncompleteRules(t *testing.T) {
	typed := *identRule
	typed.Meta.NeedsTypes = true
	createPanic := &goxlint.Rule{Meta: goxlint.Meta{Name: "create-panic"}, Create: func(*goxlint.Context) goxlint.Visitor { panic("boom") }}
	for _, rule := range []*goxlint.Rule{panicRule, createPanic, &typed} {
		t.Run(rule.Meta.Name, func(t *testing.T) {
			f := suppressionFile(t, "package p\n//goxlint:ignore t/ident\nfunc good() {}\n")
			diags := LintFile(f, []ConfiguredRule{{ID: "t/" + rule.Meta.Name, Rule: rule, Severity: Error}}, LintOptions{ReportUnusedSuppressions: Warn})
			for _, d := range diags {
				if d.RuleID == UnusedSuppressionRuleID {
					t.Fatalf("audited incomplete rule execution: %+v", diags)
				}
			}
		})
	}
}

func TestUnusedSuppressionCreateOnlyRule(t *testing.T) {
	f := suppressionFile(t, "//goxlint:ignore-file t/create\npackage p\n")
	rule := &goxlint.Rule{Create: func(ctx *goxlint.Context) goxlint.Visitor {
		ctx.Report(goxlint.Diagnostic{Node: ctx.File, Message: "file finding"})
		return nil
	}}
	diags := LintFile(f, []ConfiguredRule{{ID: "t/create", Rule: rule, Severity: Warn}}, LintOptions{ReportUnusedSuppressions: Warn})
	if len(diags) != 0 {
		t.Fatalf("create-time finding should use suppression: %+v", diags)
	}
}

func TestUnusedSuppressionsAcrossRules(t *testing.T) {
	f := suppressionFile(t, "package p\n//goxlint:ignore t/ident,t/stmt\nfunc bad() { return }\n")
	rules := []ConfiguredRule{
		{ID: "t/ident", Rule: identRule, Severity: Error},
		{ID: "t/stmt", Rule: stmtRule, Severity: Warn},
	}
	if diags := LintFile(f, rules, LintOptions{ReportUnusedSuppressions: Warn}); len(diags) != 0 {
		t.Fatalf("both targets should be used: %+v", diags)
	}
}

func TestSuppressionsForKnownDisabledRules(t *testing.T) {
	known := map[string]bool{"t/ident": true, "t/stmt": true, "other/ident": true, "t/unregistered": false}
	for _, tc := range []struct {
		name, body                   string
		enabled, explicitOff, legacy bool
		unused                       []string
	}{
		{name: "disabled rule", body: "//goxlint:ignore t/ident\nvar bad = 1"},
		{name: "explicit off severity", body: "//goxlint:ignore t/ident\nvar bad = 1", explicitOff: true},
		{name: "disabled plugin", body: "//goxlint:ignore-file t\nvar bad = 1"},
		{name: "different plugin off", body: "//goxlint:ignore other\nvar good = 1", enabled: true},
		{name: "disabled rule in active plugin", body: "//goxlint:ignore t/stmt\nvar good = 1", enabled: true},
		{name: "unknown rule", body: "//goxlint:ignore t/typo\nvar good = 1", unused: []string{"t/typo"}},
		{name: "false registration", body: "//goxlint:ignore t/unregistered\nvar good = 1", unused: []string{"t/unregistered"}},
		{name: "unknown plugin", body: "//goxlint:ignore typo\nvar good = 1", unused: []string{"typo"}},
		{name: "enabled stale rule", body: "//goxlint:ignore t/ident\nvar good = 1", enabled: true, unused: []string{"t/ident"}},
		{name: "partly enabled stale plugin", body: "//goxlint:ignore t\nvar good = 1", enabled: true, unused: []string{"t"}},
		{name: "enabled used rule", body: "//goxlint:ignore t/ident\nvar bad = 1", enabled: true},
		{name: "enabled used plugin", body: "//goxlint:ignore t\nvar bad = 1", enabled: true},
		{name: "mixed targets", body: "//goxlint:ignore t/stmt,t/ident,t/typo\nvar bad = 1", enabled: true, unused: []string{"t/typo"}},
		{name: "redundant enabled target", body: "//goxlint:ignore t/ident,t/ident\nvar bad = 1", enabled: true, unused: []string{"t/ident"}},
		{name: "redundant disabled target", body: "//goxlint:ignore t/ident,t/ident\nvar good = 1", unused: []string{"t/ident"}},
		{name: "disabled rule covered by plugin", body: "//goxlint:ignore t,t/ident\nvar good = 1", unused: []string{"t/ident"}},
		{name: "disabled plugin covered by rules", body: "//goxlint:ignore t/ident,t/stmt,t\nvar good = 1", unused: []string{"t"}},
		{name: "disabled plugin partly covered", body: "//goxlint:ignore t/ident,t\nvar good = 1"},
		{name: "redundant disabled directive", body: "//goxlint:ignore-file t\n//goxlint:ignore t/ident\nvar good = 1", unused: []string{"t/ident"}},
		{name: "partly overlapping disabled directives", body: "//goxlint:ignore t/ident\n//goxlint:ignore t/ident\nvar good = 1"},
		{name: "line does not cover file", body: "//goxlint:ignore t\n//goxlint:ignore-file t\nvar good = 1"},
		{name: "unused wildcard stays unused", body: "//goxlint:ignore *\nvar good = 1", unused: []string{"*"}},
		{name: "off target does not credit wildcard", body: "//goxlint:ignore *,t/ident\nvar good = 1", unused: []string{"*", "t/ident"}},
		{name: "used wildcard", body: "//goxlint:ignore *\nvar bad = 1", enabled: true},
		{name: "legacy disabled rule", body: "//goxlint:ignore t/ident\nvar good = 1", legacy: true, unused: []string{"t/ident"}},
		{name: "legacy disabled plugin", body: "//goxlint:ignore t\nvar good = 1", legacy: true, unused: []string{"t"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := suppressionFile(t, "package p\n"+tc.body+"\n")
			var rules []ConfiguredRule
			if tc.enabled || tc.explicitOff {
				severity := Off
				if tc.enabled {
					severity = Error
				}
				rules = []ConfiguredRule{{ID: "t/ident", Rule: identRule, Severity: severity}}
			}
			opts := LintOptions{KnownRules: known, ReportUnusedSuppressions: Warn}
			if tc.legacy {
				opts.KnownRules = nil
			}
			var unused []string
			for _, d := range LintFile(f, rules, opts) {
				if d.RuleID != UnusedSuppressionRuleID {
					t.Fatalf("unexpected rule diagnostic: %+v", d)
				}
				unused = append(unused, string(f.Source[d.Start.Offset:d.End.Offset]))
			}
			if !slices.Equal(unused, tc.unused) {
				t.Fatalf("got unused %v, want %v", unused, tc.unused)
			}
		})
	}
}

func TestKnownRulesWithPackageAnalysis(t *testing.T) {
	files := []File{
		packageFile(t, "p/off.go", "//goxlint:ignore-file t/package,t/typo\npackage p\n"),
		packageFile(t, "p/stale.go", "//goxlint:ignore-file t/package,t/typo\npackage p\n"),
		packageFile(t, "p/used.go", "//goxlint:ignore-file t/package,t/typo\npackage p\n"),
	}
	rule := &goxlint.Rule{AnalyzePackage: func(pkg *goxlint.PackageContext) {
		for _, file := range pkg.Files {
			if !strings.HasSuffix(file.Filename, "stale.go") {
				file.Report(goxlint.Diagnostic{Node: file.File.Name, Message: "package finding"})
			}
		}
	}}
	known := map[string]bool{"t/package": true}
	out := LintAll(files, func(name string) []ConfiguredRule {
		if strings.HasSuffix(name, "off.go") {
			return nil
		}
		return []ConfiguredRule{{ID: "t/package", Rule: rule, Severity: Error}}
	}, LintOptions{KnownRules: known, ReportUnusedSuppressions: Warn})
	for i, want := range [][]string{{"t/typo"}, {"t/package", "t/typo"}, {"t/typo"}} {
		var got []string
		for _, d := range out[i] {
			if d.RuleID != UnusedSuppressionRuleID {
				t.Fatalf("unexpected package diagnostic: %+v", d)
			}
			got = append(got, string(files[i].Source[d.Start.Offset:d.End.Offset]))
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s: got unused %v, want %v", files[i].Filename, got, want)
		}
	}
	if len(known) != 1 || !known["t/package"] {
		t.Fatalf("linting mutated registered rules: %v", known)
	}
}
