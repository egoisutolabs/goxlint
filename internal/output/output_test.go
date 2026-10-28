package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoisutolabs/goxlint"
	"github.com/egoisutolabs/goxlint/internal/engine"
)

func TestDiagnosticPathsUseForwardSlashes(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside", "file.go")
	cases := []struct{ name, root, filename, want string }{
		{"inside root", root, filepath.Join(root, "src", "nested", "file.go"), "src/nested/file.go"},
		{"dot-dot directory", root, filepath.Join(root, "..foo", "file.go"), "..foo/file.go"},
		{"dot-dot filename", root, filepath.Join(root, "..file.go"), "..file.go"},
		{"parent component", filepath.Join(root, "nested"), root, filepath.ToSlash(root)},
		{"without root", "", filepath.Join("src", "nested", "file.go"), "src/nested/file.go"},
		{"outside root", root, outside, filepath.ToSlash(outside)},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases, struct{ name, root, filename, want string }{
			"different volume", `C:\repo`, `D:\src\file.go`, "D:/src/file.go",
		})
	}
	for _, tc := range cases {
		for _, format := range []string{"default", "unix", "json", "github"} {
			t.Run(tc.name+"/"+format, func(t *testing.T) {
				var out bytes.Buffer
				printer := Printer{
					W: &out, Format: format, Root: tc.root,
					Sources: map[string][]byte{tc.filename: []byte("package p\n")},
				}
				diag := engine.Diagnostic{
					RuleID: "test/name", Severity: engine.Warn, Filename: tc.filename, Message: "package name",
					Start: token.Position{Line: 1, Column: 9}, End: token.Position{Line: 1, Column: 10},
				}
				if err := printer.Print([]engine.Diagnostic{diag}); err != nil {
					t.Fatal(err)
				}
				switch format {
				case "json":
					var diags []struct{ File string }
					if err := json.Unmarshal(out.Bytes(), &diags); err != nil {
						t.Fatal(err)
					}
					if len(diags) != 1 || diags[0].File != tc.want {
						t.Fatalf("want file %q, got %s", tc.want, &out)
					}
				case "github":
					if !strings.Contains(out.String(), "file="+tc.want+",line=1,") {
						t.Fatalf("wrong annotation path: %s", &out)
					}
				case "unix":
					if !strings.HasPrefix(out.String(), tc.want+":1:9:") {
						t.Fatalf("wrong unix path: %s", &out)
					}
				case "default":
					if !strings.Contains(out.String(), "["+tc.want+":1:9]") || !strings.Contains(out.String(), "package p") {
						t.Fatalf("wrong code-frame path or missing native-path source: %s", &out)
					}
				}
			})
		}
	}
}

func TestSARIFSample(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "src", "a #%.go")
	var out bytes.Buffer
	printer := Printer{
		W: &out, Format: "sarif", Root: root,
		Sources: map[string][]byte{name: []byte("package p\nvar café = 1\nvar bad = café\n")},
		Rules: map[string]goxlint.Meta{
			"test/warning": {Description: "Avoid bad names.", Category: goxlint.Style},
			"test/error":   {Description: "Check declarations.", Category: goxlint.Correctness},
			"test/quiet":   {Category: goxlint.Pedantic},
		},
	}
	diags := []engine.Diagnostic{
		{RuleID: "test/warning", Severity: engine.Warn, Filename: name, Message: "Rename bad.", Help: "Use a clear name.\nKeep it short.", Start: token.Position{Line: 3, Column: 5}, End: token.Position{Line: 3, Column: 8}},
		{RuleID: "test/error", Severity: engine.Error, Filename: name, Message: "Check both declarations.", Start: token.Position{Line: 2, Column: 5}, End: token.Position{Line: 3, Column: 16}},
		{RuleID: engine.UnusedSuppressionRuleID, Severity: engine.Warn, Filename: name, Message: "Unused suppression.", Start: token.Position{Line: 1, Column: 9}, End: token.Position{Line: 1, Column: 10}},
	}
	if err := printer.Print(diags); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/sample.sarif.json")
	if err != nil {
		t.Fatal(err)
	}
	var compactGot, compactWant bytes.Buffer
	if err := json.Compact(&compactGot, out.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := json.Compact(&compactWant, want); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(compactGot.Bytes(), compactWant.Bytes()) {
		t.Fatalf("SARIF sample changed:\n%s", &out)
	}
	if len(printer.Rules) != 3 {
		t.Fatal("printing mutated the supplied rule metadata")
	}
}

func TestSARIFEmptyResults(t *testing.T) {
	for _, diags := range [][]engine.Diagnostic{nil, {}} {
		var out bytes.Buffer
		printer := Printer{W: &out, Format: "sarif"}
		if err := printer.Print(diags); err != nil {
			t.Fatal(err)
		}
		var log sarifLog
		if err := json.Unmarshal(out.Bytes(), &log); err != nil {
			t.Fatal(err)
		}
		if log.Version != "2.1.0" || log.Schema != sarifSchema || len(log.Runs) != 1 {
			t.Fatalf("invalid log: %s", &out)
		}
		run := log.Runs[0]
		if run.Results == nil || len(run.Results) != 0 || run.Tool.Driver.Rules == nil || run.Tool.Driver.Name != "goxlint" {
			t.Fatalf("missing tool or empty arrays: %s", &out)
		}
	}
}

func TestSARIFPaths(t *testing.T) {
	root := t.TempDir()
	cases := []struct{ root, name, want string }{
		{root, filepath.Join(root, "src", "a #%.go"), "src/a%20%23%25.go"},
		{root, filepath.Join(root, "..foo", "a.go"), "..foo/a.go"},
		{root, filepath.Join(filepath.Dir(root), "outside", "a.go"), "../outside/a.go"},
		{root, filepath.Join("src", "a.go"), "src/a.go"},
		{"", filepath.Join("src", "café.go"), "src/caf%C3%A9.go"},
		{"", "a:b.go", "./a:b.go"},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases, struct{ root, name, want string }{`C:\repo`, `D:\src\a b.go`, "file:///D:/src/a%20b.go"})
		cases = append(cases, struct{ root, name, want string }{`C:\repo`, `\\server\share\a.go`, "file://server/share/a.go"})
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			var out bytes.Buffer
			printer := Printer{W: &out, Format: "sarif", Root: tc.root}
			diag := engine.Diagnostic{RuleID: "test/rule", Severity: engine.Warn, Filename: tc.name, Message: "finding", Start: token.Position{Line: 1}, End: token.Position{Line: 1}}
			if err := printer.Print([]engine.Diagnostic{diag}); err != nil {
				t.Fatal(err)
			}
			var log sarifLog
			if err := json.Unmarshal(out.Bytes(), &log); err != nil {
				t.Fatal(err)
			}
			location := log.Runs[0].Results[0].Locations[0].PhysicalLocation
			if location.ArtifactLocation.URI != tc.want {
				t.Fatalf("want URI %q, got %q", tc.want, location.ArtifactLocation.URI)
			}
			if strings.Contains(out.String(), "Column") {
				t.Fatalf("columns without source: %s", &out)
			}
		})
	}
}

func TestSARIFUnicodeColumns(t *testing.T) {
	source := []byte("package p\r\n\t/* 😀é */ bad\r\n")
	for _, tc := range []struct{ line, column, want int }{
		{2, 15, 11}, {2, 18, 14}, {1, 1, 1}, {3, 1, 1},
		{0, 1, 0}, {1, 0, 0}, {4, 1, 0}, {1, 100, 0},
	} {
		if got := sarifColumn(source, token.Position{Line: tc.line, Column: tc.column}); got != tc.want {
			t.Errorf("%d:%d: got %d, want %d", tc.line, tc.column, got, tc.want)
		}
	}
}

func TestSARIFWriteError(t *testing.T) {
	printer := Printer{W: failingWriter{}, Format: "sarif"}
	if err := printer.Print(nil); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write error lost: %v", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
