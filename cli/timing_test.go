package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/egoisutolabs/goxlint"
)

func TestTimingTable(t *testing.T) {
	timings := map[string]time.Duration{}
	for i := range 12 {
		timings[fmt.Sprintf("t/rule%02d", i)] = time.Duration(i+1) * time.Millisecond
	}
	var out bytes.Buffer
	printTiming(&out, timings, 123*time.Millisecond, true)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 12 || strings.Join(strings.Fields(lines[0]), " ") != "Rule Time (ms) Relative" {
		t.Fatalf("wrong table: %s", &out)
	}
	for i, line := range lines[1:11] {
		fields := strings.Fields(line)
		wantID := fmt.Sprintf("t/rule%02d", 11-i)
		wantPercent := fmt.Sprintf("%.1f%%", 100*float64(12-i)/78)
		if len(fields) != 3 || fields[0] != wantID || fields[1] != fmt.Sprintf("%.1f", float64(12-i)) || fields[2] != wantPercent {
			t.Fatalf("wrong order, units, or denominator: %s", line)
		}
	}
	if strings.Join(strings.Fields(lines[11]), " ") != "Load (typed mode) 123.0" {
		t.Fatalf("wrong load row: %s", lines[11])
	}
}

func TestTimingEmptyAndEqualDurations(t *testing.T) {
	for _, timings := range []map[string]time.Duration{nil, {"t/b": 0, "t/a": 0}} {
		var out bytes.Buffer
		printTiming(&out, timings, 0, false)
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if len(lines) != len(timings)+2 || strings.Contains(out.String(), "NaN") || strings.Contains(out.String(), "Inf") || !strings.Contains(out.String(), "Load (syntax mode)") {
			t.Fatalf("invalid table: %s", &out)
		}
		if len(timings) > 0 && (strings.Fields(lines[1])[0] != "t/a" || strings.Fields(lines[2])[0] != "t/b") {
			t.Fatalf("ties are not stable: %s", &out)
		}
	}
}

func TestTimingKeepsStdout(t *testing.T) {
	for _, typed := range []bool{false, true} {
		for _, format := range []string{"unix", "json", "github", "sarif"} {
			t.Run(fmt.Sprintf("%s/typed=%t", format, typed), func(t *testing.T) {
				t.Chdir(t.TempDir())
				writeTestFile(t, "go.mod", "module test.local/timing\n\ngo 1.26.0\n")
				writeTestFile(t, ".goxlintrc.json", `{"rules":{"test/rename":"error"}}`)
				writeTestFile(t, "example.go", "package p\nvar bad = 1\n")
				var plain, timed, stderr bytes.Buffer
				args := []string{"--format=" + format, "."}
				if code := Run(args, &plain, &stderr, testPlugin(typed)); code != 1 || stderr.Len() != 0 {
					t.Fatalf("plain exit %d: %s", code, &stderr)
				}
				if code := Run(append(args, "--timing"), &timed, &stderr, testPlugin(typed)); code != 1 {
					t.Fatalf("timed exit %d: %s", code, &stderr)
				}
				if plain.String() != timed.String() {
					t.Fatalf("stdout changed:\nplain: %s\ntimed: %s", &plain, &timed)
				}
				if (format == "json" || format == "sarif") && !json.Valid(timed.Bytes()) {
					t.Fatalf("invalid JSON: %s", &timed)
				}
				if !strings.Contains(stderr.String(), "test/rename") || !strings.Contains(stderr.String(), "Load ("+mode(typed)+" mode)") {
					t.Fatalf("missing timing rows: %s", &stderr)
				}
			})
		}
	}
}

func TestTimingIncludesFixPasses(t *testing.T) {
	for _, replacement := range []string{"2", "2+3"} {
		t.Run(replacement, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				t.Chdir(t.TempDir())
				writeTestFile(t, ".goxlintrc.json", `{"rules":{"test/rename":"warn"}}`)
				writeTestFile(t, "example.go", "package p\n\nvar bad = 1\n")
				plugin := goxlint.Plugin{Name: "test", Rules: []*goxlint.Rule{expressionFixRule("rename", map[string]string{"1": replacement}, goxlint.FixSafe)}}
				create := plugin.Rules[0].Create
				var measured time.Duration
				calls := 0
				plugin.Rules[0].Create = func(ctx *goxlint.Context) goxlint.Visitor {
					start := time.Now()
					time.Sleep(3 * time.Millisecond)
					measured += time.Since(start)
					calls++
					return create(ctx)
				}
				var stdout, stderr bytes.Buffer
				if code := Run([]string{"--format=json", "--timing", "--fix", "."}, &stdout, &stderr, plugin); code != 0 {
					t.Fatalf("exit %d: %s", code, &stderr)
				}
				wantCalls := 2
				if replacement == "2+3" {
					wantCalls = 3
				}
				if calls != wantCalls || strings.TrimSpace(stdout.String()) != "[]" || !strings.HasPrefix(stderr.String(), "Applied 1 fix(es) in 1 pass(es).\n") {
					t.Fatalf("unexpected fix run: calls=%d; %s; %s", calls, &stdout, &stderr)
				}
				for _, line := range strings.Split(stderr.String(), "\n") {
					fields := strings.Fields(line)
					if len(fields) != 3 || fields[0] != "test/rename" {
						continue
					}
					if fields[1] != fmt.Sprintf("%.1f", float64(measured)/float64(time.Millisecond)) {
						t.Fatalf("fix-pass time lost: %s, measured %s", line, measured)
					}
					return
				}
				t.Fatalf("missing timing row: %s", &stderr)
			})
		})
	}
}

func TestTimingEmptyRunAndLoadError(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			t.Chdir(t.TempDir())
			writeTestFile(t, ".goxlintrc.json", `{}`)
			args := []string{"--timing", "--format=json", "."}
			wantCode := 0
			if missing {
				args[2] = "missing.go"
				wantCode = 2
			}
			var stdout, stderr bytes.Buffer
			if code := Run(args, &stdout, &stderr); code != wantCode || strings.TrimSpace(stdout.String()) != "[]" {
				t.Fatalf("exit %d: %s; %s", code, &stdout, &stderr)
			}
			if !strings.Contains(stderr.String(), "Time (ms)") || !strings.Contains(stderr.String(), "Load (syntax mode)") {
				t.Fatalf("missing table: %s", &stderr)
			}
		})
	}
}
