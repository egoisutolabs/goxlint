package output

import (
	"bytes"
	"encoding/json"
	"go/token"
	"maps"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/egoisutolabs/goxlint"
	"github.com/egoisutolabs/goxlint/internal/engine"
)

const sarifSchema = "https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/schemas/sarif-schema-2.1.0.json"

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool       sarifTool     `json:"tool"`
	ColumnKind string        `json:"columnKind"`
	Results    []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name  string      `json:"name"`
	Rules []sarifRule `json:"rules"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifRule struct {
	ID               string            `json:"id"`
	ShortDescription *sarifText        `json:"shortDescription,omitempty"`
	Properties       map[string]string `json:"properties"`
}

type sarifResult struct {
	RuleID           string            `json:"ruleId"`
	Level            string            `json:"level"`
	Message          sarifText         `json:"message"`
	Locations        []sarifLocation   `json:"locations"`
	Properties       map[string]string `json:"properties,omitempty"`
	RelatedLocations []sarifLocation   `json:"relatedLocations,omitempty"`
}

type sarifLocation struct {
	ID               int                   `json:"id,omitempty"`
	Message          *sarifText            `json:"message,omitempty"`
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
	EndLine     int `json:"endLine"`
	EndColumn   int `json:"endColumn,omitempty"`
}

func (p *Printer) sarif(diags []engine.Diagnostic) error {
	metadata := make(map[string]goxlint.Meta, len(p.Rules))
	maps.Copy(metadata, p.Rules)
	// Keep every result linked to a descriptor, also for engine diagnostics.
	for _, d := range diags {
		if _, ok := metadata[d.RuleID]; !ok {
			metadata[d.RuleID] = goxlint.Meta{}
		}
	}
	rules := make([]sarifRule, 0, len(metadata))
	for _, id := range slices.Sorted(maps.Keys(metadata)) {
		meta := metadata[id]
		rule := sarifRule{ID: id, Properties: map[string]string{"category": string(meta.Category)}}
		// SARIF requires non-empty text when shortDescription is present.
		if meta.Description != "" {
			rule.ShortDescription = &sarifText{Text: meta.Description}
		}
		rules = append(rules, rule)
	}
	results := make([]sarifResult, 0, len(diags))
	for _, d := range diags {
		result := sarifResult{
			RuleID: d.RuleID, Level: sarifLevel(d.Severity), Message: sarifText{Text: d.Message},
			Locations: []sarifLocation{p.sarifLocation(d.Filename, d.Start, d.End)},
		}
		for i, related := range d.Related {
			location := p.sarifLocation(related.Filename, related.Start, related.End)
			location.ID = i + 1
			if related.Message != "" {
				location.Message = &sarifText{Text: related.Message}
			}
			result.RelatedLocations = append(result.RelatedLocations, location)
		}
		if d.Help != "" {
			result.Properties = map[string]string{"help": d.Help}
		}
		results = append(results, result)
	}
	log := sarifLog{Schema: sarifSchema, Version: "2.1.0", Runs: []sarifRun{{
		Tool:       sarifTool{Driver: sarifDriver{Name: "goxlint", Rules: rules}},
		ColumnKind: "unicodeCodePoints", Results: results,
	}}}
	enc := json.NewEncoder(p.W)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}

func sarifLevel(s engine.Severity) string {
	switch s {
	case engine.Warn:
		return "warning"
	case engine.Error:
		return "error"
	default:
		return "note"
	}
}

func (p *Printer) sarifLocation(filename string, start, end token.Position) sarifLocation {
	return sarifLocation{PhysicalLocation: sarifPhysicalLocation{
		ArtifactLocation: sarifArtifactLocation{URI: p.sarifURI(filename)},
		Region: sarifRegion{
			StartLine: start.Line, StartColumn: sarifColumn(p.Sources[filename], start),
			EndLine: end.Line, EndColumn: sarifColumn(p.Sources[filename], end),
		},
	}}
}

func (p *Printer) sarifURI(name string) string {
	if p.Root != "" {
		if rel, err := filepath.Rel(p.Root, name); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			name = rel
		}
	}
	path := filepath.ToSlash(name)
	uri := url.URL{Path: path}
	// Absolute paths (outside Root, or on another Windows volume) become file: URIs.
	if filepath.IsAbs(name) {
		uri.Scheme = "file"
		if strings.HasPrefix(path, "//") {
			uri.Host, uri.Path, _ = strings.Cut(path[2:], "/")
			uri.Path = "/" + uri.Path
		} else if !strings.HasPrefix(path, "/") {
			uri.Path = "/" + path
		}
	}
	return uri.String()
}

// Go columns count bytes. SARIF columns count Unicode code points here.
// Without source text, omit columns rather than label byte counts as characters.
func sarifColumn(source []byte, pos token.Position) int {
	if source == nil || pos.Line < 1 || pos.Column < 1 {
		return 0
	}
	for line := 1; line < pos.Line; line++ {
		_, rest, found := bytes.Cut(source, []byte("\n"))
		if !found {
			return 0
		}
		source = rest
	}
	line, _, _ := bytes.Cut(source, []byte("\n"))
	if pos.Column-1 > len(line) {
		return 0
	}
	return utf8.RuneCount(line[:pos.Column-1]) + 1
}
