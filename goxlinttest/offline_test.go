package goxlinttest

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestPrivateModuleStaysOffline checks that a module matched by GOPRIVATE is
// not fetched with git. GOPROXY=off does not cover that path on its own.
func TestPrivateModuleStaysOffline(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "git.log")
	binDir := t.TempDir()
	writeFakeGit(t, binDir)
	t.Setenv("GITLOG", logPath)
	t.Setenv("GOPRIVATE", "github.com/example")
	t.Setenv("GONOPROXY", "github.com/example")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	mod := t.TempDir()
	gomod := "module example.com/harness\n\ngo 1.26.0\n\nrequire github.com/example/private v0.1.0\n"
	if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := "github.com/example/private v0.1.0 h1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa=\n" +
		"github.com/example/private v0.1.0/go.mod h1:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb=\n"
	if err := os.WriteFile(filepath.Join(mod, "go.sum"), []byte(sum), 0o644); err != nil {
		t.Fatal(err)
	}

	old := startupDir
	startupDir = mod
	t.Cleanup(func() { startupDir = old })

	mu.Lock()
	defer mu.Unlock()
	pkgs, err := loadExportPackages([]string{"github.com/example/private"})
	if data, readErr := os.ReadFile(logPath); readErr == nil && len(bytes.TrimSpace(data)) > 0 {
		t.Fatalf("go command ran git:\n%s\nload error: %v", data, err)
	}
	if err == nil {
		pkg := pkgs["github.com/example/private"]
		if pkg == nil || exportError(pkg) == nil {
			t.Fatalf("private module loaded: %#v", pkg)
		}
	}
}

func writeFakeGit(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		body := "@echo off\r\necho GIT %*>>%GITLOG%\r\nexit /b 1\r\n"
		if err := os.WriteFile(filepath.Join(dir, "git.bat"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	body := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$GITLOG\"\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}
