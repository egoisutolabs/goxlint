// Package output formats diagnostics.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/egoisutolabs/goxlint"
	"github.com/egoisutolabs/goxlint/internal/engine"
)

// Formats lists the supported --format values.
var Formats = []string{"default", "unix", "json", "github", "sarif"}

// ValidateFormat checks the format before loading files or applying fixes.
func ValidateFormat(format string) error {
	if format == "" || slices.Contains(Formats, format) {
		return nil
	}
	return fmt.Errorf("unknown format %q (use %s)", format, strings.Join(Formats, ", "))
}

// Printer writes diagnostics in one format.
type Printer struct {
	W      io.Writer
	Format string
	Color  bool
	// Root shortens file paths in output.
	Root string
	// Sources maps filenames to contents, for code frames and SARIF columns.
	Sources map[string][]byte
	// Rules maps enabled rule IDs to metadata for SARIF output.
	Rules map[string]goxlint.Meta
}

// Print writes all diagnostics.
func (p *Printer) Print(diags []engine.Diagnostic) error {
	if err := ValidateFormat(p.Format); err != nil {
		return err
	}
	switch p.Format {
	case "sarif":
		return p.sarif(diags)
	case "", "default":
		for _, d := range diags {
			p.frame(d)
		}
	case "unix":
		for _, d := range diags {
			fmt.Fprintf(p.W, "%s:%d:%d: %s [%s/%s]\n", p.path(d.Filename), d.Start.Line, d.Start.Column, d.Message, capitalize(d.Severity.String()), d.RuleID)
		}
	case "github":
		for _, d := range diags {
			level := "warning"
			if d.Severity == engine.Error {
				level = "error"
			}
			fmt.Fprintf(p.W, "::%s file=%s,line=%d,endLine=%d,col=%d,endColumn=%d,title=%s::%s\n",
				level, p.path(d.Filename), d.Start.Line, d.End.Line, d.Start.Column, d.End.Column, d.RuleID, d.Message)
		}
	case "json":
		type jsonDiag struct {
			Rule      string `json:"rule"`
			Severity  string `json:"severity"`
			File      string `json:"file"`
			Line      int    `json:"line"`
			Column    int    `json:"column"`
			EndLine   int    `json:"endLine"`
			EndColumn int    `json:"endColumn"`
			Message   string `json:"message"`
			Help      string `json:"help,omitempty"`
			Fix       string `json:"fix,omitempty"`
		}
		out := make([]jsonDiag, 0, len(diags))
		for _, d := range diags {
			j := jsonDiag{
				Rule: d.RuleID, Severity: d.Severity.String(), File: p.path(d.Filename),
				Line: d.Start.Line, Column: d.Start.Column, EndLine: d.End.Line, EndColumn: d.End.Column,
				Message: d.Message, Help: d.Help,
			}
			if d.Fix != nil {
				j.Fix = d.Fix.Kind.String()
			}
			out = append(out, j)
		}
		enc := json.NewEncoder(p.W)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	return nil
}

const (
	red    = "\x1b[31m"
	yellow = "\x1b[33m"
	cyan   = "\x1b[36m"
	dim    = "\x1b[2m"
	bold   = "\x1b[1m"
	reset  = "\x1b[0m"
)

func (p *Printer) c(code, s string) string {
	if !p.Color {
		return s
	}
	return code + s + reset
}

// frame prints a diagnostic with a source excerpt:
//
//	⚠ anti-slop(no-any-parameters): Parameter `v` is typed as `any`.
//	   ╭─[main.go:3:8]
//	 2 │
//	 3 │ func f(v any) {}
//	   ·        ─────
//	   ╰────
//	  help: Accept a concrete type.
func (p *Printer) frame(d engine.Diagnostic) {
	icon, color := "⚠", yellow
	if d.Severity == engine.Error {
		icon, color = "×", red
	}
	plugin, rule, _ := strings.Cut(d.RuleID, "/")
	fmt.Fprintf(p.W, "\n  %s %s: %s\n", p.c(color, icon), p.c(color, plugin+"("+rule+")"), p.c(bold, d.Message))

	lines := strings.Split(string(p.Sources[d.Filename]), "\n")
	first := max(d.Start.Line-1, 1)
	last := min(d.Start.Line+1, len(lines))
	width := len(fmt.Sprint(last))
	gutter := strings.Repeat(" ", width+1)
	fmt.Fprintf(p.W, "%s%s\n", gutter, p.c(dim, fmt.Sprintf("╭─[%s:%d:%d]", p.path(d.Filename), d.Start.Line, d.Start.Column)))
	for n := first; n <= last && n <= len(lines); n++ {
		text := lines[n-1]
		fmt.Fprintf(p.W, "%s %s %s\n", p.c(dim, fmt.Sprintf("%*d", width, n)), p.c(dim, "│"), expandTabs(text))
		if n != d.Start.Line {
			continue
		}
		endCol := len(text) + 1
		if d.End.Line == d.Start.Line {
			endCol = d.End.Column
		}
		pad := displayWidth(text[:min(d.Start.Column-1, len(text))])
		span := max(displayWidth(text[min(d.Start.Column-1, len(text)):min(endCol-1, len(text))]), 1)
		fmt.Fprintf(p.W, "%s%s %s%s\n", gutter, p.c(dim, "·"), strings.Repeat(" ", pad), p.c(color, strings.Repeat("─", span)))
	}
	fmt.Fprintf(p.W, "%s%s\n", gutter, p.c(dim, "╰────"))
	if d.Help != "" {
		fmt.Fprintf(p.W, "  %s %s\n", p.c(cyan, "help:"), d.Help)
	}
}

const tabWidth = 4

func expandTabs(s string) string { return strings.ReplaceAll(s, "\t", strings.Repeat(" ", tabWidth)) }

func displayWidth(s string) int {
	return utf8.RuneCountInString(s) + strings.Count(s, "\t")*(tabWidth-1)
}

func (p *Printer) path(name string) string {
	if p.Root == "" {
		return filepath.ToSlash(name)
	}
	if rel, err := filepath.Rel(p.Root, name); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(name)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
