package cli

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestSARIFEnabledRules(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		args         []string
		want         []string
	}{
		{"defaults", `{}`, nil, []string{"goxlint/unused-suppression"}},
		{"base", `{"rules":{"demo/alpha":"warn"}}`, nil, []string{"demo/alpha", "goxlint/unused-suppression"}},
		{"flag off", `{"rules":{"demo/alpha":"warn"}}`, []string{"-A", "demo/alpha"}, []string{"goxlint/unused-suppression"}},
		{"flag on", `{}`, []string{"-W", "demo/alpha"}, []string{"demo/alpha", "goxlint/unused-suppression"}},
		{"override on", `{"overrides":[{"files":["a.go"],"rules":{"demo/both":"warn"}}]}`, nil, []string{"demo/both", "goxlint/unused-suppression"}},
		{"override off everywhere", `{"rules":{"demo/alpha":"warn"},"overrides":[{"files":["*.go"],"rules":{"demo/alpha":"off"}}]}`, nil, []string{"goxlint/unused-suppression"}},
		{"override off one file", `{"rules":{"demo/alpha":"warn"},"overrides":[{"files":["a.go"],"rules":{"demo/alpha":"off"}}]}`, nil, []string{"demo/alpha", "goxlint/unused-suppression"}},
		{"unmatched override", `{"overrides":[{"files":["missing.go"],"rules":{"demo/both":"warn"}}]}`, nil, []string{"goxlint/unused-suppression"}},
		{"audit off", `{"reportUnusedSuppressions":"off"}`, nil, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeTestFile(t, ".goxlintrc.json", tc.config)
			writeTestFile(t, "a.go", "package p\n")
			writeTestFile(t, "b.go", "package p\n")
			var stdout, stderr bytes.Buffer
			args := append([]string{"--format=sarif", "."}, tc.args...)
			plugin := rulesTestPlugin()
			if code := Run(args, &stdout, &stderr, plugin); code != 0 || stderr.Len() != 0 {
				t.Fatalf("exit %d: %s; %s", code, &stdout, &stderr)
			}
			var log struct {
				Version string
				Runs    []struct {
					Tool struct {
						Driver struct {
							Name  string
							Rules []struct {
								ID               string
								ShortDescription struct{ Text string }
								Properties       struct{ Category string }
							}
						}
					}
					Results []json.RawMessage
				}
			}
			if err := json.Unmarshal(stdout.Bytes(), &log); err != nil {
				t.Fatal(err)
			}
			if log.Version != "2.1.0" || len(log.Runs) != 1 || log.Runs[0].Results == nil || len(log.Runs[0].Results) != 0 || log.Runs[0].Tool.Driver.Name != "goxlint" {
				t.Fatalf("invalid empty log: %s", &stdout)
			}
			var ids []string
			for _, rule := range log.Runs[0].Tool.Driver.Rules {
				ids = append(ids, rule.ID)
				for _, registered := range plugin.Rules {
					if rule.ID == plugin.Name+"/"+registered.Meta.Name && (rule.ShortDescription.Text != registered.Meta.Description || rule.Properties.Category != string(registered.Meta.Category)) {
						t.Fatalf("wrong metadata: %+v", rule)
					}
				}
			}
			if !slices.Equal(ids, tc.want) {
				t.Fatalf("got rules %v, want %v", ids, tc.want)
			}
		})
	}
}

func TestSARIFDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name          string
		args          []string
		code, results int
	}{
		{"warning", nil, 0, 2},
		{"error", []string{"-D", "test/rename"}, 1, 2},
		{"quiet", []string{"--quiet", "--deny-warnings"}, 1, 0},
		{"fix", []string{"--fix"}, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeTestFile(t, ".goxlintrc.json", `{"rules":{"test/rename":"warn"}}`)
			writeTestFile(t, "a #%.go", "package p\n//line virtual.go:100\nvar bad = 1\n//goxlint:ignore test/typo reason\nvar good = 2\n")
			var stdout, stderr bytes.Buffer
			args := append([]string{"--format=sarif", "."}, tc.args...)
			if code := Run(args, &stdout, &stderr, testPlugin(false)); code != tc.code {
				t.Fatalf("exit %d, want %d: %s; %s", code, tc.code, &stdout, &stderr)
			}
			var log struct {
				Runs []struct {
					Tool struct {
						Driver struct{ Rules []struct{ ID string } }
					}
					Results []struct {
						RuleID, Level string
						Message       struct{ Text string }
						Properties    struct{ Help string }
						Locations     []struct {
							PhysicalLocation struct {
								ArtifactLocation struct{ URI string }
								Region           struct{ StartLine, StartColumn, EndLine, EndColumn int }
							}
						}
					}
				}
			}
			if err := json.Unmarshal(stdout.Bytes(), &log); err != nil {
				t.Fatal(err)
			}
			if len(log.Runs) != 1 || len(log.Runs[0].Results) != tc.results {
				t.Fatalf("wrong results: %s", &stdout)
			}
			var ids []string
			for _, rule := range log.Runs[0].Tool.Driver.Rules {
				ids = append(ids, rule.ID)
			}
			for _, result := range log.Runs[0].Results {
				if !slices.Contains(ids, result.RuleID) || result.Message.Text == "" || len(result.Locations) != 1 {
					t.Fatalf("incomplete result: %+v", result)
				}
				location := result.Locations[0].PhysicalLocation
				if location.ArtifactLocation.URI != "a%20%23%25.go" {
					t.Fatalf("wrong URI: %+v", location)
				}
				if result.RuleID == "test/rename" {
					level := "warning"
					if tc.name == "error" {
						level = "error"
					}
					if result.Level != level || location.Region.StartLine != 3 || location.Region.EndLine != 3 || location.Region.StartColumn != 5 || location.Region.EndColumn != 8 {
						t.Fatalf("wrong physical range or severity: %+v", result)
					}
				} else if result.RuleID != "goxlint/unused-suppression" || result.Properties.Help == "" {
					t.Fatalf("missing audit help: %+v", result)
				}
			}
			if tc.name == "fix" && !strings.Contains(stderr.String(), "Applied 1 fix(es)") {
				t.Fatalf("missing fix summary: %s", &stderr)
			}
		})
	}
}
