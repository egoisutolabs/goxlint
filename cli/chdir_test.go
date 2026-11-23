package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChdirFromNestedModule(t *testing.T) {
	root := t.TempDir()
	lintDir := filepath.Join(root, "tools", "lint")
	if err := os.MkdirAll(filepath.Join(root, "internal", "util"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(lintDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "go.mod"), "module example.com/root\n\ngo 1.26.0\n")
	writeTestFile(t, filepath.Join(root, ".goxlintrc.json"), `{"rules":{"test/rename":"error"}}`)
	writeTestFile(t, filepath.Join(root, "pkg.go"), "package root\nvar bad = 1\n")
	writeTestFile(t, filepath.Join(root, "internal", "util", "util.go"), "package util\nvar bad = 1\n")
	writeTestFile(t, filepath.Join(lintDir, "go.mod"), "module example.com/root/tools/lint\n\ngo 1.26.0\n")
	// The nested module's config would hide the root files if discovery started here.
	writeTestFile(t, filepath.Join(lintDir, ".goxlintrc.json"), `{"rules":{"test/rename":"off"}}`)
	writeTestFile(t, filepath.Join(lintDir, "main.go"), "package main\nvar bad = 1\n")

	t.Chdir(lintDir)
	start, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	for _, typed := range []bool{false, true} {
		t.Run(fmt.Sprintf("typed=%t", typed), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{"-C", filepath.Join("..", ".."), "--format=unix", "./..."}, &stdout, &stderr, testPlugin(typed))
			if code != 1 || stderr.Len() != 0 {
				t.Fatalf("exit %d: %s\n%s", code, &stderr, &stdout)
			}
			for _, name := range []string{"pkg.go", "internal/util/util.go"} {
				want := name + ":2:5: rename bad [Error/test/rename]"
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("missing %s\n%s", want, &stdout)
				}
			}
			const nested = "tools/lint/main.go:2:5: rename bad [Error/test/rename]"
			if typed && strings.Contains(stdout.String(), "main.go") {
				t.Fatalf("typed ./... entered the nested module:\n%s", &stdout)
			}
			if !typed && !strings.Contains(stdout.String(), nested) {
				t.Fatalf("syntax mode did not lint the nested module:\n%s", &stdout)
			}
			wantCount := 2
			if !typed {
				wantCount = 3
			}
			if got := strings.Count(stdout.String(), "rename bad"); got != wantCount {
				t.Fatalf("got %d diagnostics, want %d:\n%s", got, wantCount, &stdout)
			}
			if got, err := os.Getwd(); err != nil || got != start {
				t.Fatalf("working directory changed from %s to %s: %v", start, got, err)
			}
		})
	}

	t.Run("without -C", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run([]string{"-D", "test/rename", "--format=unix", "./..."}, &stdout, &stderr, testPlugin(true))
		if code != 1 || stderr.Len() != 0 {
			t.Fatalf("exit %d: %s\n%s", code, &stderr, &stdout)
		}
		if !strings.Contains(stdout.String(), "main.go:2:5: rename bad [Error/test/rename]") ||
			strings.Contains(stdout.String(), "pkg.go") || strings.Contains(stdout.String(), "util.go") {
			t.Fatalf("without -C, ./... did not stay in the nested module:\n%s", &stdout)
		}
	})
}

func TestChdirResolvesPathsFromTarget(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestFile(t, "go.mod", "module example.com/here\n\ngo 1.26.0\n")
	writeTestFile(t, ".goxlintrc.json", `{"rules":{"test/rename":"off"}}`)
	const here = "package here\nvar bad = 1\n"
	writeTestFile(t, "here.go", here)
	if err := os.Mkdir("target", 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join("target", "go.mod"), "module example.com/target\n\ngo 1.26.0\n")
	writeTestFile(t, filepath.Join("target", ".goxlintrc.json"), `{"rules":{"test/rename":"error"}}`)
	const there = "package target\nvar bad = 1\n"
	const fixed = "package target\nvar good = 1\n"
	want := "there.go:2:5: rename bad [Error/test/rename]\n"

	start, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	absTarget, err := filepath.Abs("target")
	if err != nil {
		t.Fatal(err)
	}

	for _, typed := range []bool{false, true} {
		t.Run(fmt.Sprintf("typed=%t", typed), func(t *testing.T) {
			plugin := testPlugin(typed)
			check := func(t *testing.T, args []string) {
				t.Helper()
				writeTestFile(t, filepath.Join("target", "there.go"), there)
				var stdout, stderr bytes.Buffer
				code := Run(append(args, "--format=unix"), &stdout, &stderr, plugin)
				if code != 1 || stderr.Len() != 0 || stdout.String() != want {
					t.Fatalf("exit %d: %s\n%s", code, &stderr, &stdout)
				}
				if got, err := os.Getwd(); err != nil || got != start {
					t.Fatalf("working directory changed from %s to %s: %v", start, got, err)
				}
			}
			t.Run("before patterns", func(t *testing.T) { check(t, []string{"-C", "target", "./..."}) })
			t.Run("after patterns", func(t *testing.T) { check(t, []string{"./...", "-C", "target"}) })
			t.Run("equals form", func(t *testing.T) { check(t, []string{"-C=target", "./..."}) })
			t.Run("long form", func(t *testing.T) { check(t, []string{"--C=target", "there.go"}) })
			t.Run("relative config", func(t *testing.T) {
				check(t, []string{"-C", "target", "-c", ".goxlintrc.json", "there.go"})
			})
			t.Run("absolute directory", func(t *testing.T) { check(t, []string{"-C", absTarget, "./..."}) })
			t.Run("config outside target", func(t *testing.T) {
				writeTestFile(t, filepath.Join("target", "there.go"), there)
				var stdout, stderr bytes.Buffer
				args := []string{"-C", "target", "-c", filepath.Join("..", ".goxlintrc.json"), "--format=unix", "./..."}
				code := Run(args, &stdout, &stderr, plugin)
				if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
					t.Fatalf("exit %d: %s\n%s", code, &stderr, &stdout)
				}
			})
			t.Run("fix", func(t *testing.T) {
				writeTestFile(t, filepath.Join("target", "there.go"), there)
				var stdout, stderr bytes.Buffer
				code := Run([]string{"-C", "target", "--fix", "--format=unix", "there.go"}, &stdout, &stderr, plugin)
				if code != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "Applied 1 fix(es) in 1 pass(es).") {
					t.Fatalf("exit %d: %s\n%s", code, &stderr, &stdout)
				}
				if got, err := os.ReadFile(filepath.Join("target", "there.go")); err != nil || string(got) != fixed {
					t.Fatalf("target file: %q: %v", got, err)
				}
				if got, err := os.ReadFile("here.go"); err != nil || string(got) != here {
					t.Fatalf("current directory file changed: %q: %v", got, err)
				}
			})
		})
	}
}

func TestChdirRejectsBadDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestFile(t, "file.go", "package p\n")
	for _, tc := range []struct {
		name, dir, want string
	}{
		{name: "missing", dir: "missing", want: "-C:"},
		{name: "file", dir: "file.go", want: "not a directory"},
		{name: "empty", dir: "", want: "directory is empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run([]string{"-C", tc.dir, "./..."}, &stdout, &stderr, testPlugin(false)); code != 2 || !strings.Contains(stderr.String(), tc.want) || stdout.Len() != 0 {
				t.Fatalf("exit %d: %s\n%s", code, &stderr, &stdout)
			}
		})
	}
}

func TestChdirHelpDiffersFromConfigFlag(t *testing.T) {
	t.Chdir(t.TempDir())
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"-h"}, &stdout, &stderr); code != 0 || stdout.Len() != 0 {
		t.Fatalf("exit %d: %s\n%s", code, &stderr, &stdout)
	}
	if !strings.Contains(stderr.String(), "-C dir") || !strings.Contains(stderr.String(), "selects the config file") {
		t.Fatalf("help does not describe -C:\n%s", &stderr)
	}
	if !strings.Contains(stderr.String(), "-c string") || !strings.Contains(stderr.String(), "path to config file") {
		t.Fatalf("help does not describe -c:\n%s", &stderr)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"rules", "-h"}, &stdout, &stderr); code != 0 || !strings.Contains(stderr.String(), "-C dir") {
		t.Fatalf("rules help exit %d:\n%s", code, &stderr)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"init", "-h"}, &stdout, &stderr); code != 0 || !strings.Contains(stderr.String(), "-C dir") {
		t.Fatalf("init help exit %d:\n%s", code, &stderr)
	}
	if _, err := os.Stat(".goxlintrc.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("init -h created a config file: %v", err)
	}
}

func TestChdirRulesUsesTargetConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	writeTestFile(t, ".goxlintrc.json", `{"categories":{"style":"off"}}`)
	if err := os.Mkdir("target", 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join("target", ".goxlintrc.json"), `{"categories":{"style":"warn"}}`)

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"rules", "--format=json", "-C", "target"}, &stdout, &stderr, rulesTestPlugin()); code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	var got []ruleJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	enabled := map[string]string{}
	for _, rule := range got {
		enabled[rule.ID] = rule.Enabled
	}
	if enabled["demo/zeta"] != "warn" || enabled["demo/alpha"] != "off" || enabled["demo/both"] != "off" {
		t.Fatalf("resolved the wrong config: %#v", enabled)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"rules", "--format=json", "-C", "target", "-c", ".goxlintrc.json"}, &stdout, &stderr, rulesTestPlugin()); code != 0 {
		t.Fatalf("relative -c exit %d: %s", code, &stderr)
	}
	got = nil
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	for _, rule := range got {
		if rule.ID == "demo/zeta" && rule.Enabled != "warn" {
			t.Fatalf("relative -c did not start in -C: %#v", rule)
		}
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"rules", "-C", "target"}, &stdout, &stderr, rulesTestPlugin()); code != 0 || !strings.Contains(stdout.String(), "demo/zeta") {
		t.Fatalf("table exit %d: %s\n%s", code, &stderr, &stdout)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"rules", "--format=json", "-C", "missing"}, &stdout, &stderr, rulesTestPlugin()); code != 2 || !strings.Contains(stderr.String(), "-C:") {
		t.Fatalf("missing dir exit %d: %s", code, &stderr)
	}
}

func TestChdirInitWritesInTarget(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("target", 0o755); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"init", "-C", "target"}, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	if _, err := os.Stat(".goxlintrc.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created config in the current directory: %v", err)
	}
	targetPath := filepath.Join("target", ".goxlintrc.json")
	first, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"init", "-C", "target"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), ".goxlintrc.json already exists") {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	second, err := os.ReadFile(targetPath)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("existing config changed: %q: %v", second, err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"init", "-C", "missing"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "-C:") {
		t.Fatalf("missing dir exit %d: %s", code, &stderr)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"init", "extra"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "unexpected argument") {
		t.Fatalf("extra arg exit %d: %s", code, &stderr)
	}
	if _, err := os.Stat(".goxlintrc.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("init with an extra argument created a file: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"init"}, &stdout, &stderr); code != 0 || stderr.Len() != 0 {
		t.Fatalf("init in the current directory exit %d: %s", code, &stderr)
	}
	if _, err := os.Stat(".goxlintrc.json"); err != nil {
		t.Fatal(err)
	}
}

func TestChdirStdinFilename(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll(filepath.Join("target", "internal", "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join("target", "go.mod"), "module example.com/target\n\ngo 1.26.0\n")
	writeTestFile(t, filepath.Join("target", ".goxlintrc.json"), `{"rules":{"test/rename":"error"}}`)
	src := "package api\nvar bad = 1\n"
	abs, err := filepath.Abs(filepath.Join("target", "internal", "api", "handler.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, typed := range []bool{false, true} {
		for _, tc := range []struct{ name, filename string }{
			{name: "relative", filename: filepath.Join("internal", "api", "handler.go")},
			{name: "absolute", filename: abs},
		} {
			t.Run(fmt.Sprintf("typed=%t/%s", typed, tc.name), func(t *testing.T) {
				var stdout, stderr bytes.Buffer
				args := []string{"-C", "target", "--stdin-filename", tc.filename, "--format=json"}
				code := run(args, strings.NewReader(src), &stdout, &stderr, testPlugin(typed))
				if code != 1 || stderr.Len() != 0 {
					t.Fatalf("exit %d: %s\n%s", code, &stderr, &stdout)
				}
				var diags []struct {
					File         string
					Line, Column int
				}
				if err := json.Unmarshal(stdout.Bytes(), &diags); err != nil {
					t.Fatal(err)
				}
				if len(diags) != 1 || diags[0].File != "internal/api/handler.go" || diags[0].Line != 2 || diags[0].Column != 5 {
					t.Fatalf("wrong buffer path: %s", &stdout)
				}
			})
		}
	}
}
