package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoisutolabs/goxlint/cli"
	"github.com/egoisutolabs/goxlint/examples/goxlint/plugins/antislop"
)

func TestSARIFFromSubdirectory(t *testing.T) {
	const handler = "package service\n\nfunc Handler() {\n\tpanic(\"not implemented\")\n}\n"
	const config = `{"rules":{"anti-slop/no-placeholder-comments":"warn"},"reportUnusedSuppressions":"off"}`
	for _, tc := range []struct {
		name     string
		git      string
		fromRoot bool
		outside  bool
		want     string
	}{
		{name: "subdirectory", git: "dir", want: filepath.Join("service", "handler.go")},
		{name: "git-file", git: "file", want: filepath.Join("service", "handler.go")},
		{name: "repository-root", git: "dir", fromRoot: true, want: filepath.Join("service", "handler.go")},
		{name: "no-repository", want: "handler.go"},
		{name: "outside-repository", git: "dir", outside: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			switch tc.git {
			case "dir":
				if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
					t.Fatal(err)
				}
			case "file":
				if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: /var/empty/repo.git\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			writeAbsFile(t, filepath.Join(repo, ".goxlintrc.json"), config)
			service := filepath.Join(repo, "service")
			target := filepath.Join(service, "handler.go")
			writeAbsFile(t, target, handler)
			pattern := "."
			if tc.outside {
				target = filepath.Join(t.TempDir(), "a.go")
				writeAbsFile(t, target, handler)
				pattern = target
			}
			if tc.fromRoot {
				t.Chdir(repo)
				pattern = "./service"
			} else {
				t.Chdir(service)
			}
			var stdout, stderr bytes.Buffer
			if code := cli.Run([]string{"--format=sarif", pattern}, &stdout, &stderr, antislop.Plugin); code != 0 || stderr.Len() != 0 {
				t.Fatalf("exit %d: %s; %s", code, &stdout, &stderr)
			}
			uri := firstSARIFURI(t, stdout.Bytes())
			if tc.outside {
				slash := filepath.ToSlash(target)
				if !strings.HasPrefix(uri, "file:///") || !strings.Contains(uri, slash) {
					t.Fatalf("URI %q, want an absolute file: URI for %s", uri, slash)
				}
				return
			}
			want := filepath.ToSlash(tc.want)
			if uri != want {
				t.Fatalf("URI %q, want %q", uri, want)
			}
		})
	}
}

func firstSARIFURI(t *testing.T, stdout []byte) string {
	t.Helper()
	var log struct {
		Runs []struct {
			Results []struct {
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct{ URI string }
					}
				}
			}
		}
	}
	if err := json.Unmarshal(stdout, &log); err != nil {
		t.Fatalf("bad SARIF: %v; %s", err, stdout)
	}
	if len(log.Runs) != 1 || len(log.Runs[0].Results) != 1 || len(log.Runs[0].Results[0].Locations) != 1 {
		t.Fatalf("unexpected SARIF: %s", stdout)
	}
	return log.Runs[0].Results[0].Locations[0].PhysicalLocation.ArtifactLocation.URI
}

func writeAbsFile(t *testing.T, name, source string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}
