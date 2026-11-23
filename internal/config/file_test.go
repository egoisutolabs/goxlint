package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoisutolabs/goxlint"
	"github.com/egoisutolabs/goxlint/internal/engine"
)

func TestRuleSettingJSON(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in, out string
		sev     engine.Severity
		options string
	}{
		{in: `"off"`, out: `"off"`, sev: engine.Off},
		{in: `"allow"`, out: `"off"`, sev: engine.Off},
		{in: `"warn"`, out: `"warn"`, sev: engine.Warn},
		{in: `"error"`, out: `"error"`, sev: engine.Error},
		{in: `"deny"`, out: `"error"`, sev: engine.Error},
		{in: `["error"]`, out: `"error"`, sev: engine.Error},
		{in: `["warn", {"max": 5}]`, out: `["warn",{"max":5}]`, sev: engine.Warn, options: `{"max":5}`},
		{in: `["allow", {"flag": true}]`, out: `["off",{"flag":true}]`, sev: engine.Off, options: `{"flag":true}`},
	} {
		t.Run(tc.in, func(t *testing.T) {
			var got RuleSetting
			if err := json.Unmarshal([]byte(tc.in), &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got.Severity != tc.sev {
				t.Fatalf("severity = %s, want %s", got.Severity, tc.sev)
			}
			if compactJSON(t, got.Options) != tc.options {
				t.Fatalf("options = %s, want %s", got.Options, tc.options)
			}
			b, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tc.out {
				t.Fatalf("Marshal = %s, want %s", b, tc.out)
			}
		})
	}

	for _, tc := range []struct{ in, want string }{
		{`1`, `rule setting must be a severity or [severity, options], got 1`},
		{`null`, `unknown severity "" (use off, warn or error)`},
		{`[]`, `rule setting must be a severity or [severity, options], got []`},
		{`["warn", {}, 1]`, `rule setting must be a severity or [severity, options], got ["warn", {}, 1]`},
		{`[1]`, `rule severity must be a string, got 1`},
		{`["loud"]`, `unknown severity "loud" (use off, warn or error)`},
		{`"loud"`, `unknown severity "loud" (use off, warn or error)`},
		{`""`, `unknown severity "" (use off, warn or error)`},
	} {
		t.Run("bad "+tc.in, func(t *testing.T) {
			var got RuleSetting
			err := json.Unmarshal([]byte(tc.in), &got)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Unmarshal error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestFindWalksUpToTheNearestConfig(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	nested := filepath.Join(root, "pkg", "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := Find(nested); got != "" {
		t.Fatalf("Find = %q, want no config file", got)
	}

	top := filepath.Join(root, FileName)
	if err := os.WriteFile(top, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Find(nested); got != top {
		t.Fatalf("Find(%s) = %q, want ancestor %q", nested, got, top)
	}
	if got := Find(root); got != top {
		t.Fatalf("Find(%s) = %q, want %q", root, got, top)
	}

	nearer := filepath.Join(root, "pkg", FileName)
	if err := os.WriteFile(nearer, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Find(nested); got != nearer {
		t.Fatalf("Find(%s) = %q, want nearest %q", nested, got, nearer)
	}
	if got := Find(root); got != top {
		t.Fatalf("Find(%s) = %q, want %q", root, got, top)
	}
}

func TestLoadRejectsInvalidFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if _, err := Load(path); !os.IsNotExist(err) {
		t.Fatalf("missing file error = %v, want not exist", err)
	}
	for _, tc := range []struct{ name, body, want string }{
		{"truncated", `{`, "unexpected EOF"},
		{"unknown field", `{"extra": true}`, `unknown field "extra"`},
		{"unknown override field", `{"overrides":[{"files":["a.go"],"extra":true}]}`, `unknown field "extra"`},
		{"category type", `{"categories":{"style":1}}`, "cannot unmarshal"},
		{"bad severity", `{"rules":{"demo/kept":"loud"}}`, `unknown severity "loud"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load error = %v, want path %q and %q", err, path, tc.want)
			}
		})
	}
}

func TestDefaultWarnsOnCorrectness(t *testing.T) {
	t.Parallel()
	reg := NewRegistry(goxlint.Plugin{Name: "demo", Rules: []*goxlint.Rule{
		{Meta: goxlint.Meta{Name: "correct", Category: goxlint.Correctness, NeedsTypes: true}},
		{Meta: goxlint.Meta{Name: "style", Category: goxlint.Style}},
	}})
	resolved, err := Resolve(Default(), reg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ReportUnusedSuppressions != engine.Warn {
		t.Fatalf("unused suppressions = %s, want warning", resolved.ReportUnusedSuppressions)
	}
	if resolved.Severity("demo/correct") != "warn" || resolved.Severity("demo/style") != "off" || resolved.Severity("other/missing") != "off" {
		t.Fatalf("severities correct=%s style=%s missing=%s", resolved.Severity("demo/correct"), resolved.Severity("demo/style"), resolved.Severity("other/missing"))
	}
	rules := resolved.AllRules()
	if len(rules) != 1 || rules[0].Meta.Name != "correct" || !rules[0].Meta.NeedsTypes {
		t.Fatalf("AllRules = %v", ruleNames(rules))
	}
	if resolved.AnyOverrideNeedsTypes(reg) {
		t.Fatal("default config enabled a typed rule from an override")
	}
}

func TestReportUnusedSuppressions(t *testing.T) {
	t.Parallel()
	reg := demoRegistry()
	root := t.TempDir()
	for _, tc := range []struct {
		raw  string
		want engine.Severity
	}{
		{raw: "", want: engine.Warn},
		{raw: "off", want: engine.Off},
		{raw: "allow", want: engine.Off},
		{raw: "warn", want: engine.Warn},
		{raw: "error", want: engine.Error},
		{raw: "deny", want: engine.Error},
	} {
		name := tc.raw
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			body := `{}`
			if tc.raw != "" {
				body = `{"reportUnusedSuppressions":"` + tc.raw + `"}`
			}
			resolved, err := Resolve(mustLoad(t, root, body), reg, root)
			if err != nil {
				t.Fatal(err)
			}
			if resolved.ReportUnusedSuppressions != tc.want {
				t.Fatalf("%q severity = %s, want %s", tc.raw, resolved.ReportUnusedSuppressions, tc.want)
			}
		})
	}
}

func TestResolveRejectsUnknownNames(t *testing.T) {
	t.Parallel()
	reg := demoRegistry()
	root := t.TempDir()
	for _, tc := range []struct{ name, body, want string }{
		{"category", `{"categories":{"nope":"warn"}}`, `unknown category "nope"`},
		{"category severity", `{"categories":{"style":"loud"}}`, `category style: unknown severity "loud" (use off, warn or error)`},
		{"rule", `{"rules":{"demo/missing":"error"}}`, "unknown rule \"demo/missing\" (run `goxlint rules` to list rules)"},
		{"override rule", `{"overrides":[{"files":["a.go"],"rules":{"demo/missing":"warn"}}]}`, "unknown rule \"demo/missing\" (run `goxlint rules` to list rules)"},
		{"unused", `{"reportUnusedSuppressions":"maybe"}`, `reportUnusedSuppressions: unknown severity "maybe" (use off, warn or error)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Resolve(mustLoad(t, root, tc.body), reg, root)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("Resolve error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestResolveAppliesCategoriesRulesAndOverrides(t *testing.T) {
	t.Parallel()
	reg := demoRegistry()
	root := t.TempDir()
	const body = `{
		"reportUnusedSuppressions": "error",
		"categories": {"correctness": "allow", "style": "warn"},
		"rules": {
			"demo/kept": "error",
			"demo/tuned": ["warn", {"max": 5}],
			"demo/disabled": "off"
		},
		"ignorePatterns": ["gen/**"],
		"overrides": [
			{
				"files": ["*_test.go"],
				"rules": {
					"demo/typed": "error",
					"demo/kept": "off",
					"demo/tuned": ["error", {"max": 9}]
				}
			},
			{"files": ["*_test.go"], "rules": {"demo/typed": "off"}}
		]
	}`
	resolved, err := Resolve(mustLoad(t, root, body), reg, root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ReportUnusedSuppressions != engine.Error {
		t.Fatalf("unused suppressions = %s, want error", resolved.ReportUnusedSuppressions)
	}
	for _, tc := range []struct{ id, severity string }{
		{"demo/kept", "error"},
		{"demo/tuned", "warn"},
		{"demo/style", "warn"},
		{"demo/correct", "off"},
		{"demo/disabled", "off"},
		{"demo/typed", "off"},
		{"missing/rule", "off"},
	} {
		if got := resolved.Severity(tc.id); got != tc.severity {
			t.Errorf("Severity(%s) = %s, want %s", tc.id, got, tc.severity)
		}
	}
	if got := ruleNames(resolved.AllRules()); !slices.Equal(got, []string{"kept", "style", "tuned"}) {
		t.Fatalf("AllRules = %v, want kept, style, tuned", got)
	}
	// The first override enables demo/typed. The second turns it off for the
	// file, but the load mode still has to see the earlier enablement.
	if !resolved.AnyOverrideNeedsTypes(reg) {
		t.Fatal("AnyOverrideNeedsTypes = false, want true")
	}

	source := rulesByID(resolved.RulesFor(filepath.Join(root, "widget.go"), reg))
	if ids := ruleIDs(source); !slices.Equal(ids, []string{"demo/kept", "demo/style", "demo/tuned"}) {
		t.Fatalf("RulesFor(widget.go) = %v", ids)
	}
	if source["demo/kept"].Severity != engine.Error || len(source["demo/kept"].Options) != 0 {
		t.Fatalf("demo/kept = %+v, want error and no options", source["demo/kept"])
	}
	if source["demo/tuned"].Severity != engine.Warn || compactJSON(t, source["demo/tuned"].Options) != `{"max":5}` {
		t.Fatalf("demo/tuned = severity %s options %s", source["demo/tuned"].Severity, source["demo/tuned"].Options)
	}

	tests := rulesByID(resolved.RulesFor(filepath.Join(root, "widget_test.go"), reg))
	if ids := ruleIDs(tests); !slices.Equal(ids, []string{"demo/style", "demo/tuned"}) {
		t.Fatalf("RulesFor(widget_test.go) = %v, want style and tuned", ids)
	}
	if tests["demo/tuned"].Severity != engine.Error || compactJSON(t, tests["demo/tuned"].Options) != `{"max":9}` {
		t.Fatalf("test override = severity %s options %s, want error and max 9", tests["demo/tuned"].Severity, tests["demo/tuned"].Options)
	}
	if !resolved.Ignored(filepath.Join(root, "gen", "file.go"), false) {
		t.Fatal("gen/file.go was not ignored")
	}
	if resolved.Ignored(filepath.Join(root, "widget.go"), false) {
		t.Fatal("widget.go was ignored")
	}

	t.Run("typed rule off", func(t *testing.T) {
		cfg := mustLoad(t, root, `{"overrides":[{"files":["*_test.go"],"rules":{"demo/typed":"off"}}]}`)
		off, err := Resolve(cfg, reg, root)
		if err != nil {
			t.Fatal(err)
		}
		if off.AnyOverrideNeedsTypes(reg) {
			t.Fatal("turning demo/typed off still requested types")
		}
	})
	t.Run("non-typed override", func(t *testing.T) {
		cfg := mustLoad(t, root, `{"overrides":[{"files":["*_test.go"],"rules":{"demo/kept":"error"}}]}`)
		plain, err := Resolve(cfg, reg, root)
		if err != nil {
			t.Fatal(err)
		}
		if plain.AnyOverrideNeedsTypes(reg) {
			t.Fatal("an override of demo/kept requested types")
		}
		if got := ruleNames(plain.AllRules()); len(got) != 0 {
			t.Fatalf("AllRules = %v, want none", got)
		}
		testFile := rulesByID(plain.RulesFor(filepath.Join(root, "widget_test.go"), reg))
		if ids := ruleIDs(testFile); !slices.Equal(ids, []string{"demo/kept"}) || testFile["demo/kept"].Severity != engine.Error {
			t.Fatalf("RulesFor(widget_test.go) = %v", ids)
		}
		if got := plain.RulesFor(filepath.Join(root, "widget.go"), reg); len(got) != 0 {
			t.Fatalf("RulesFor(widget.go) = %v, want no base rules", got)
		}
	})
}

func TestNewRegistryPanicsOnDuplicateRule(t *testing.T) {
	t.Parallel()
	defer func() {
		const want = "duplicate rule demo/same"
		if got := recover(); got != want {
			t.Fatalf("recover() = %v, want %q", got, want)
		}
	}()
	rule := &goxlint.Rule{Meta: goxlint.Meta{Name: "same"}}
	NewRegistry(
		goxlint.Plugin{Name: "demo", Rules: []*goxlint.Rule{rule}},
		goxlint.Plugin{Name: "demo", Rules: []*goxlint.Rule{rule}},
	)
	t.Fatal("NewRegistry returned")
}

func demoRegistry() *Registry {
	rule := func(name string, category goxlint.Category, needsTypes bool) *goxlint.Rule {
		return &goxlint.Rule{Meta: goxlint.Meta{Name: name, Category: category, NeedsTypes: needsTypes}}
	}
	return NewRegistry(goxlint.Plugin{Name: "demo", Rules: []*goxlint.Rule{
		rule("correct", goxlint.Correctness, false),
		rule("disabled", goxlint.Style, false),
		rule("kept", goxlint.Correctness, false),
		rule("style", goxlint.Style, false),
		rule("tuned", goxlint.Correctness, false),
		rule("typed", goxlint.Suspicious, true),
	}})
}

func mustLoad(t *testing.T, dir, body string) *Config {
	t.Helper()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func compactJSON(t *testing.T, b []byte) string {
	t.Helper()
	if len(b) == 0 {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		t.Fatalf("compact %s: %v", b, err)
	}
	return buf.String()
}

func ruleNames(rules []*goxlint.Rule) []string {
	names := make([]string, len(rules))
	for i, rule := range rules {
		names[i] = rule.Meta.Name
	}
	return names
}

func rulesByID(rules []engine.ConfiguredRule) map[string]engine.ConfiguredRule {
	out := make(map[string]engine.ConfiguredRule, len(rules))
	for _, rule := range rules {
		out[rule.ID] = rule
	}
	return out
}

func ruleIDs(rules map[string]engine.ConfiguredRule) []string {
	ids := make([]string, 0, len(rules))
	for id := range rules {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
