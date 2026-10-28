package engine

import (
	"fmt"
	"go/token"
	"strings"
)

// Suppression directives:
//
//	//goxlint:ignore <rules> [reason]       suppresses its own line and the next line
//	//goxlint:ignore-file <rules> [reason]  suppresses the whole file
//
// <rules> is a comma-separated list of "plugin/rule", "plugin", or "*".
const (
	ignoreDirective     = "//goxlint:ignore "
	ignoreFileDirective = "//goxlint:ignore-file "
)

// UnusedSuppressionRuleID identifies engine findings for unused suppression targets.
const UnusedSuppressionRuleID = "goxlint/unused-suppression"

type suppression struct {
	line  int // 0 for file-wide
	rules []suppressionTarget
}

type suppressionTarget struct {
	name     string
	pos, end token.Pos
	used     bool
}

func applySuppressions(f File, diags []Diagnostic, rules []ConfiguredRule, opts LintOptions) []Diagnostic {
	var sups []suppression
	for _, cg := range f.AST.Comments {
		for _, c := range cg.List {
			text, line := c.Text, f.Fset.PositionFor(c.Slash, false).Line
			var prefix string
			switch {
			case strings.HasPrefix(text, ignoreFileDirective):
				prefix, line = ignoreFileDirective, 0
			case strings.HasPrefix(text, ignoreDirective):
				prefix = ignoreDirective
			default:
				continue
			}
			rest := text[len(prefix):]
			fields := strings.Fields(rest)
			if len(fields) == 0 {
				continue
			}
			pos := c.Slash + token.Pos(len(prefix)+strings.Index(rest, fields[0]))
			s := suppression{line: line}
			for _, name := range strings.Split(fields[0], ",") {
				s.rules = append(s.rules, suppressionTarget{name: name, pos: pos, end: pos + token.Pos(len(name))})
				pos += token.Pos(len(name) + 1)
			}
			sups = append(sups, s)
		}
	}
	if len(sups) == 0 {
		return diags
	}
	tf := f.Fset.File(f.AST.FileStart)
	out := diags[:0]
	for _, d := range diags {
		line := tf.Line(tf.Pos(d.Start.Offset))
		if !suppressed(sups, d.RuleID, line) {
			out = append(out, d)
		}
	}
	if opts.ReportUnusedSuppressions == Off {
		return out
	}
	disabled := disabledSuppressionTargets(opts.KnownRules, rules)
	// Add these after filtering so directives cannot hide their own unused targets.
	for i, s := range sups {
		for j, target := range s.rules {
			if target.used {
				continue
			}
			if target.name != "*" && disabled[target.name] && !redundantDisabledTarget(sups, i, j, opts.KnownRules) {
				continue
			}
			start, end := f.Fset.PositionFor(target.pos, false), f.Fset.PositionFor(target.end, false)
			start.Filename, end.Filename = f.Filename, f.Filename
			out = append(out, Diagnostic{
				RuleID: UnusedSuppressionRuleID, Severity: opts.ReportUnusedSuppressions,
				Filename: f.Filename, Start: start, End: end,
				Message: fmt.Sprintf("Unused suppression for %q: no diagnostic was suppressed.", target.name),
				Help:    "Remove this target or correct its rule name. Suppressions are checked against the rules enabled for this file.",
			})
		}
	}
	return out
}

func disabledSuppressionTargets(known map[string]bool, rules []ConfiguredRule) map[string]bool {
	if len(known) == 0 {
		return nil
	}
	disabled := map[string]bool{}
	for id, registered := range known {
		if registered {
			disabled[id] = true
			plugin, _, _ := strings.Cut(id, "/")
			disabled[plugin] = true
		}
	}
	for _, cr := range rules {
		if cr.Severity != Off {
			delete(disabled, cr.ID)
			plugin, _, _ := strings.Cut(cr.ID, "/")
			delete(disabled, plugin)
		}
	}
	return disabled
}

// A disabled target is still redundant if earlier targets cover every known
// rule it names across its full scope. Partial line overlap is not enough.
func redundantDisabledTarget(sups []suppression, directive, target int, known map[string]bool) bool {
	current := sups[directive]
	for id, registered := range known {
		if !registered || !suppressionMatches(current.rules[target].name, id) {
			continue
		}
		covered := false
		for i, earlier := range sups[:directive+1] {
			if earlier.line != 0 && earlier.line != current.line {
				continue
			}
			targets := earlier.rules
			if i == directive {
				targets = targets[:target]
			}
			for _, previous := range targets {
				if suppressionMatches(previous.name, id) {
					covered = true
					break
				}
			}
			if covered {
				break
			}
		}
		if !covered {
			return false
		}
	}
	return true
}

func suppressionMatches(target, ruleID string) bool {
	return target == "*" || target == ruleID || strings.HasPrefix(ruleID, target+"/")
}

// The first matching directive and target receives credit. Later duplicates
// are redundant, and removing a reported unused target leaves suppression intact.
func suppressed(sups []suppression, ruleID string, line int) bool {
	for _, s := range sups {
		if s.line != 0 && line != s.line && line != s.line+1 {
			continue
		}
		for i := range s.rules {
			target := &s.rules[i]
			if suppressionMatches(target.name, ruleID) {
				target.used = true
				return true
			}
		}
	}
	return false
}
