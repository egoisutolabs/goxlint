package output

import (
	"bytes"
	"encoding/json"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoisutolabs/goxlint/internal/engine"
)

func TestRelatedOutput(t *testing.T) {
	root := t.TempDir()
	primary, reference := filepath.Join(root, "b.go"), filepath.Join(root, "src", "a #.go")
	d := engine.Diagnostic{RuleID: "test/rule", Severity: engine.Warn, Filename: primary, Message: "duplicate", Start: token.Position{Line: 2, Column: 5}, End: token.Position{Line: 2, Column: 8}, Related: []engine.Related{
		{Filename: reference, Start: token.Position{Line: 2, Column: 15}, End: token.Position{Line: 2, Column: 18}, Message: "Original \"bad\"."},
		{Filename: reference, Start: token.Position{Line: 2, Column: 15}, End: token.Position{Line: 3, Column: 1}},
	}}
	for _, format := range []string{"default", "json", "sarif", "unix", "github"} {
		t.Run(format, func(t *testing.T) {
			var out bytes.Buffer
			p := Printer{W: &out, Format: format, Root: root, Sources: map[string][]byte{primary: []byte("package p\nvar bad = 1\n"), reference: []byte("package p\n\t/* 😀é */ bad\n")}}
			if err := p.Print([]engine.Diagnostic{d}); err != nil {
				t.Fatal(err)
			}
			switch format {
			case "default":
				for _, want := range []string{"[b.go:2:5]", "[src/a #.go:2:15]", "related: Original \"bad\".", "related: related location\n", "    /* 😀é */ bad", "·              ───"} {
					if !strings.Contains(out.String(), want) {
						t.Errorf("missing %q: %s", want, &out)
					}
				}
				if strings.Count(out.String(), "test(rule)") != 1 || strings.Count(out.String(), "related:") != 2 {
					t.Fatalf("extra diagnostics or missing frames: %s", &out)
				}
			case "json":
				var got []struct {
					Related []struct {
						File                             string
						Line, Column, EndLine, EndColumn int
						Message                          string
					}
				}
				if err := json.Unmarshal(out.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if len(got) != 1 || len(got[0].Related) != 2 {
					t.Fatalf("wrong locations: %s", &out)
				}
				r := got[0].Related[0]
				if r.File != "src/a #.go" || r.Line != 2 || r.Column != 15 || r.EndLine != 2 || r.EndColumn != 18 || r.Message != "Original \"bad\"." || got[0].Related[1].EndLine != 3 || got[0].Related[1].Message != "" || !strings.Contains(out.String(), `"message": ""`) {
					t.Fatalf("wrong range: %s", &out)
				}
			case "sarif":
				var log sarifLog
				if err := json.Unmarshal(out.Bytes(), &log); err != nil {
					t.Fatal(err)
				}
				results := log.Runs[0].Results
				if len(results) != 1 || len(results[0].RelatedLocations) != 2 {
					t.Fatalf("wrong locations: %s", &out)
				}
				r := results[0].RelatedLocations[0]
				region := r.PhysicalLocation.Region
				if r.ID != 1 || r.Message == nil || r.Message.Text != "Original \"bad\"." || r.PhysicalLocation.ArtifactLocation.URI != "src/a%20%23.go" || region.StartColumn != 11 || region.EndColumn != 14 || region.StartLine != 2 || region.EndLine != 2 {
					t.Fatalf("wrong SARIF location: %s", &out)
				}
				second := results[0].RelatedLocations[1]
				if second.ID != 2 || second.Message != nil || second.PhysicalLocation.Region.EndLine != 3 {
					t.Fatalf("wrong second location: %s", &out)
				}
			case "unix", "github":
				original := out.String()
				out.Reset()
				plain := d
				plain.Related = nil
				if err := p.Print([]engine.Diagnostic{plain}); err != nil {
					t.Fatal(err)
				}
				if out.String() != original || strings.Count(original, "\n") != 1 {
					t.Fatalf("line format changed: %s", original)
				}
			}
		})
	}
}

func TestRelatedOmittedWithoutLocations(t *testing.T) {
	for _, format := range []string{"json", "sarif"} {
		var out bytes.Buffer
		p := Printer{W: &out, Format: format}
		if err := p.Print([]engine.Diagnostic{{RuleID: "test/rule", Severity: engine.Warn, Filename: "a.go", Message: "finding", Start: token.Position{Line: 1, Column: 1}, End: token.Position{Line: 1, Column: 1}}}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "related") {
			t.Fatalf("new field in legacy output: %s", &out)
		}
	}
}
