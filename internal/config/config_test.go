package config

import (
	"path/filepath"
	"testing"

	"github.com/egoisutolabs/goxlint"
	"github.com/egoisutolabs/goxlint/internal/engine"
)

func TestSlashPatternsMatchNativePaths(t *testing.T) {
	root := t.TempDir()
	reg := NewRegistry(goxlint.Plugin{Name: "test", Rules: []*goxlint.Rule{{Meta: goxlint.Meta{Name: "rule"}}}})
	cfg := &Config{
		Rules:          map[string]RuleSetting{"test/rule": {Severity: engine.Error}},
		IgnorePatterns: []string{"generated/**", "**/*.pb.go"},
		Overrides: []Override{
			{Files: []string{"src/**/*.go"}, Rules: map[string]RuleSetting{"test/rule": {Severity: engine.Warn}}},
			{Files: []string{"src/**/skip.go"}, Rules: map[string]RuleSetting{"test/rule": {Severity: engine.Off}}},
		},
	}
	resolved, err := Resolve(cfg, reg, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path               string
		directory, ignored bool
	}{
		{"generated", true, true},
		{"generated/nested/file.go", false, true},
		{"src/nested/file.pb.go", false, true},
		{"src/generated/file.go", false, false},
		{"src/nested/file.go", false, false},
	} {
		if got := resolved.Ignored(filepath.Join(root, filepath.FromSlash(tc.path)), tc.directory); got != tc.ignored {
			t.Errorf("Ignored(%q, %t) = %t, want %t", tc.path, tc.directory, got, tc.ignored)
		}
	}
	for _, tc := range []struct {
		path     string
		severity engine.Severity
	}{
		{"src/file.go", engine.Warn},
		{"src/nested/file.go", engine.Warn},
		{"src/nested/skip.go", engine.Off},
		{"other/file.go", engine.Error},
	} {
		rules := resolved.RulesFor(filepath.Join(root, filepath.FromSlash(tc.path)), reg)
		if tc.severity == engine.Off {
			if len(rules) != 0 {
				t.Errorf("RulesFor(%q) did not disable the rule: %+v", tc.path, rules)
			}
		} else if len(rules) != 1 || rules[0].Severity != tc.severity {
			t.Errorf("RulesFor(%q) = %+v, want severity %s", tc.path, rules, tc.severity)
		}
	}
}
