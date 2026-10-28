package cli

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"
	"time"
)

func printTiming(w io.Writer, timings map[string]time.Duration, load time.Duration, typed bool) {
	type ruleTime struct {
		id   string
		time time.Duration
	}
	var rules []ruleTime
	var total time.Duration
	for id, elapsed := range timings {
		rules = append(rules, ruleTime{id, elapsed})
		total += elapsed
	}
	slices.SortFunc(rules, func(a, b ruleTime) int {
		if order := cmp.Compare(b.time, a.time); order != 0 {
			return order
		}
		return strings.Compare(a.id, b.id)
	})
	tw := tabwriter.NewWriter(w, 0, 4, 3, ' ', 0)
	fmt.Fprintln(tw, "Rule\tTime (ms)\tRelative")
	for _, rule := range rules[:min(10, len(rules))] {
		relative := 0.0
		if total > 0 {
			relative = 100 * float64(rule.time) / float64(total)
		}
		fmt.Fprintf(tw, "%s\t%.1f\t%.1f%%\n", rule.id, float64(rule.time)/float64(time.Millisecond), relative)
	}
	fmt.Fprintf(tw, "Load (%s mode)\t%.1f\n", mode(typed), float64(load)/float64(time.Millisecond))
	tw.Flush()
}
