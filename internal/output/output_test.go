package output

import (
	"bytes"
	"encoding/json"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
		for _, format := range Formats {
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
