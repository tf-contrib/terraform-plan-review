package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/tf-contrib/terraform-plan-review/internal/diff"
	"github.com/tf-contrib/terraform-plan-review/internal/github"
	"github.com/tf-contrib/terraform-plan-review/internal/policy"
	"github.com/tf-contrib/terraform-plan-review/internal/report"
)

// GitHub shows at most 10 annotations of each level per step; anything
// beyond is dropped silently, so we choose which ones make the cut.
const maxAnnotationsPerLevel = 10

const maxAnnotationAttrs = 10

// annotations returns one annotation per change with a known source
// location, most severe first: blocked changes are errors, destructive
// changes warnings, everything else notices.
func annotations(reports []*report.Report, labels []string) []github.Annotation {
	byLevel := map[string][]github.Annotation{}
	for _, r := range reports {
		for _, c := range r.Changes {
			if c.Source == nil || c.Action == report.Read {
				continue
			}
			level := "notice"
			switch {
			case blocking(c) && !overridden(r, labels):
				level = "error"
			case c.Action.Destructive() || len(c.Findings) > 0:
				level = "warning"
			}
			byLevel[level] = append(byLevel[level], github.Annotation{
				Level:   level,
				File:    c.Source.File,
				Line:    c.Source.Line,
				Title:   fmt.Sprintf("%s will be %s", c.Address, pastTense(c.Action)),
				Message: annotationMessage(c),
			})
		}
	}
	var out []github.Annotation
	for _, level := range []string{"error", "warning", "notice"} {
		as := byLevel[level]
		if len(as) > maxAnnotationsPerLevel {
			fmt.Fprintf(os.Stderr, "terraform-plan-review: %d %s annotations not shown (GitHub limit is %d per step)\n",
				len(as)-maxAnnotationsPerLevel, level, maxAnnotationsPerLevel)
			as = as[:maxAnnotationsPerLevel]
		}
		out = append(out, as...)
	}
	return out
}

func blocking(c report.Change) bool {
	for _, f := range c.Findings {
		if f.Severity == policy.Block {
			return true
		}
	}
	return false
}

func pastTense(a report.Action) string {
	switch a {
	case report.Delete:
		return "destroyed"
	case report.Replace:
		return "replaced"
	case report.Update:
		return "updated in place"
	case report.Create:
		return "created"
	case report.Forget:
		return "removed from state"
	case report.Import:
		return "imported"
	case report.Move:
		return "moved"
	}
	return string(a)
}

func annotationMessage(c report.Change) string {
	var lines []string
	for _, f := range c.Findings {
		msg := f.Message
		if msg == "" {
			msg = "matched rule " + f.Rule
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", f.Severity, msg))
	}
	if c.Moved() {
		lines = append(lines, "Moved from "+c.PreviousAddress)
	}
	if len(c.ReplacedBy) > 0 {
		lines = append(lines, "Replacement forced by: "+strings.Join(c.ReplacedBy, ", "))
	}
	if c.Reason != "" {
		lines = append(lines, "Reason: "+c.Reason)
	}
	shown := 0
	for _, a := range c.Attrs {
		if a.After == diff.Unknown && !a.ForcesReplacement {
			continue
		}
		if shown == maxAnnotationAttrs {
			lines = append(lines, "…")
			break
		}
		shown++
		switch {
		case a.Lines != nil:
			lines = append(lines, fmt.Sprintf("~ %s (%d changed lines)", a.Path, changedLines(a.Lines)))
		case a.Kind == diff.Added:
			lines = append(lines, fmt.Sprintf("+ %s = %s", a.Path, a.After))
		case a.Kind == diff.Removed:
			lines = append(lines, fmt.Sprintf("- %s = %s", a.Path, a.Before))
		default:
			lines = append(lines, fmt.Sprintf("~ %s = %s -> %s", a.Path, a.Before, a.After))
		}
	}
	if len(lines) == 0 {
		return pastTense(c.Action)
	}
	return strings.Join(lines, "\n")
}

func changedLines(ls []diff.Line) int {
	n := 0
	for _, l := range ls {
		if l.Op == '+' || l.Op == '-' {
			n++
		}
	}
	return n
}
