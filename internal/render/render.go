// Package render formats reports as GitHub-flavoured Markdown.
package render

import (
	"fmt"
	"html"
	"slices"
	"strings"

	"github.com/tofu-contrib/tofu-plan-review/internal/diff"
	"github.com/tofu-contrib/tofu-plan-review/internal/policy"
	"github.com/tofu-contrib/tofu-plan-review/internal/report"
)

// CommentLimit is GitHub's maximum issue comment body length.
const CommentLimit = 65536

// SummaryLimit is GitHub's maximum job summary size per step.
const SummaryLimit = 1 << 20

type Options struct {
	// ID distinguishes independent comments on the same pull request,
	// e.g. one per workflow.
	ID    string
	Title string
	// BlobURL is the base for source links, e.g.
	// https://github.com/org/repo/blob/<sha>. Empty disables links.
	BlobURL string
	// DetailsURL points at the full report (the job summary) when details
	// had to be omitted.
	DetailsURL string
	Commit     string
	Previous   *State
	// Labels on the pull request; used for policy override labels.
	Labels []string
	// Limit is the maximum output length in bytes.
	Limit int
}

// Marker identifies comments created by this tool with the given ID.
func Marker(id string) string {
	if id == "" {
		id = "default"
	}
	return fmt.Sprintf("<!-- tofu-plan-review:id=%s -->", id)
}

// tier controls how much detail is rendered.
type tier int

const (
	tierFull      tier = iota // every attribute and line
	tierTrimmed               // drop "known after apply" noise, cap long diffs
	tierDangerous             // diffs only for destructive or flagged changes
	tierSummary               // no per-resource details
)

const (
	trimmedMaxLines = 60
	trimmedMaxAttrs = 40
)

// Markdown renders the reports, picking the most detailed tier that fits
// in opts.Limit.
func Markdown(reports []*report.Report, opts Options) string {
	if opts.Limit == 0 {
		opts.Limit = CommentLimit
	}
	state := NewState(opts.Commit, reports)
	r := &renderer{reports: reports, opts: opts, state: state, delta: Compare(opts.Previous, state)}
	var out string
	for t := tierFull; t <= tierSummary; t++ {
		out = r.render(t, -1)
		if len(out) <= opts.Limit {
			return out
		}
	}
	// Even the summary is too large: shrink the destructive table.
	for rows := 200; rows >= 0; rows /= 2 {
		out = r.render(tierSummary, rows)
		if len(out) <= opts.Limit || rows == 0 {
			break
		}
	}
	return truncate(out, opts.Limit)
}

type renderer struct {
	reports []*report.Report
	opts    Options
	state   *State
	delta   *Delta
}

func (r *renderer) multi() bool { return len(r.reports) > 1 }

func (r *renderer) overridden(rep *report.Report) bool {
	return rep.OverrideLabel != "" && slices.Contains(r.opts.Labels, rep.OverrideLabel)
}

func (r *renderer) render(t tier, maxRows int) string {
	var b strings.Builder
	b.WriteString(Marker(r.opts.ID))
	b.WriteString("\n")
	r.header(&b)
	r.alerts(&b)
	if r.multi() {
		r.rootTable(&b)
	}
	r.destructive(&b, maxRows)
	r.deltaSection(&b)
	if t < tierSummary {
		for _, rep := range r.reports {
			r.details(&b, rep, t)
		}
	}
	if t > tierFull {
		b.WriteString("\n> [!NOTE]\n> Some details were omitted to fit GitHub's comment size limit.")
		if r.opts.DetailsURL != "" {
			fmt.Fprintf(&b, " The full plan is in the [job summary](%s).", r.opts.DetailsURL)
		}
		b.WriteString("\n")
	}
	r.footer(&b)
	return b.String()
}

func (r *renderer) totals() report.Counts {
	total := report.Counts{}
	for _, rep := range r.reports {
		for a, n := range rep.Counts() {
			total[a] += n
		}
	}
	return total
}

func (r *renderer) blocked() bool {
	for _, rep := range r.reports {
		if len(rep.Blocked()) > 0 && !r.overridden(rep) {
			return true
		}
	}
	return false
}

func (r *renderer) errored() bool {
	return slices.ContainsFunc(r.reports, func(rep *report.Report) bool { return rep.Errored })
}

func (r *renderer) header(b *strings.Builder) {
	title := r.opts.Title
	if title == "" {
		title = "OpenTofu plan"
	}
	total := r.totals()
	icon := "🟢"
	switch {
	case r.errored():
		icon = "❌"
	case r.blocked():
		icon = "⛔"
	case total[report.Delete]+total[report.Replace] > 0:
		icon = "🔴"
	case total.Total() > 0:
		icon = "🟡"
	}
	fmt.Fprintf(b, "### %s %s: %s\n\n", icon, html.EscapeString(title), summary(total, true))
}

var actionLabels = map[report.Action]string{
	report.Delete:  "to destroy",
	report.Replace: "to replace",
	report.Forget:  "to forget",
	report.Update:  "to change",
	report.Create:  "to add",
	report.Import:  "to import",
	report.Move:    "to move",
	report.Read:    "to read",
}

var actionIcons = map[report.Action]string{
	report.Delete:  "🗑️",
	report.Replace: "♻️",
	report.Forget:  "👋",
	report.Update:  "✏️",
	report.Create:  "➕",
	report.Import:  "📥",
	report.Move:    "🚚",
	report.Read:    "📖",
}

// actionName is the word OpenTofu uses for an action in its own output.
func actionName(a report.Action) string {
	if a == report.Delete {
		return "destroy"
	}
	return string(a)
}

// summary renders counts like "1 to destroy, 2 to change".
func summary(c report.Counts, bold bool) string {
	var parts []string
	for _, a := range report.Order {
		if a == report.Read || c[a] == 0 {
			continue
		}
		s := fmt.Sprintf("%d %s", c[a], actionLabels[a])
		if bold && a.Destructive() {
			s = "**" + s + "**"
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return "no changes"
	}
	return strings.Join(parts, ", ")
}

func (r *renderer) alerts(b *strings.Builder) {
	for _, rep := range r.reports {
		name := r.rootPrefix(rep)
		if rep.Errored {
			fmt.Fprintf(b, "> [!CAUTION]\n> %sThe plan failed. The changes below are incomplete.\n\n", name)
		}
		if rep.Incomplete {
			fmt.Fprintf(b, "> [!WARNING]\n> %sThis plan is partial (`-target` or `-exclude`). Other changes may be pending.\n\n", name)
		}
	}

	var blocked, overridden, warned []string
	for _, rep := range r.reports {
		for _, c := range rep.Changes {
			for _, f := range c.Findings {
				line := fmt.Sprintf("> - %s%s will be **%s**: %s", r.rootPrefix(rep), r.link(c), verb(c.Action), findingText(f))
				switch {
				case f.Severity == policy.Block && r.overridden(rep):
					overridden = append(overridden, line)
				case f.Severity == policy.Block:
					blocked = append(blocked, line)
				default:
					warned = append(warned, line)
				}
			}
		}
	}
	if len(blocked) > 0 {
		b.WriteString("> [!CAUTION]\n> **Blocked by policy.**")
		if labels := r.overrideLabels(); len(labels) > 0 {
			fmt.Fprintf(b, " Add the %s label to approve.", strings.Join(labels, " or "))
		}
		b.WriteString("\n" + strings.Join(blocked, "\n") + "\n\n")
	}
	if len(overridden) > 0 {
		b.WriteString("> [!WARNING]\n> **Blocking rules overridden by label.**\n" + strings.Join(overridden, "\n") + "\n\n")
	}
	if len(warned) > 0 {
		b.WriteString("> [!WARNING]\n" + strings.Join(warned, "\n") + "\n\n")
	}
}

func (r *renderer) overrideLabels() []string {
	var out []string
	for _, rep := range r.reports {
		l := "`" + rep.OverrideLabel + "`"
		if rep.OverrideLabel != "" && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out
}

func findingText(f report.Finding) string {
	if f.Message != "" {
		return fmt.Sprintf("%s (`%s`)", f.Message, f.Rule)
	}
	return fmt.Sprintf("matched rule `%s`", f.Rule)
}

func verb(a report.Action) string {
	switch a {
	case report.Delete:
		return "destroyed"
	case report.Replace:
		return "replaced"
	case report.Update:
		return "updated"
	case report.Create:
		return "created"
	case report.Forget:
		return "forgotten"
	case report.Import:
		return "imported"
	case report.Move:
		return "moved"
	}
	return string(a)
}

func (r *renderer) rootPrefix(rep *report.Report) string {
	if !r.multi() || rep.Name == "" {
		return ""
	}
	return "`" + rep.Name + "` "
}

func (r *renderer) rootTable(b *strings.Builder) {
	b.WriteString("| Root | Destroy | Replace | Change | Add | Other | |\n|---|--:|--:|--:|--:|--:|---|\n")
	for _, rep := range r.reports {
		c := rep.Counts()
		other := c[report.Forget] + c[report.Import] + c[report.Move]
		status := ""
		switch {
		case rep.Errored:
			status = "❌ failed"
		case len(rep.Blocked()) > 0 && !r.overridden(rep):
			status = "⛔ blocked"
		case c[report.Delete]+c[report.Replace] > 0:
			status = "🔴"
		case c.Total() == 0:
			status = "no changes"
		}
		fmt.Fprintf(b, "| `%s` | %s | %s | %s | %s | %s | %s |\n", rep.Name,
			num(c[report.Delete], true), num(c[report.Replace], true), num(c[report.Update], false),
			num(c[report.Create], false), num(other, false), status)
	}
	b.WriteString("\n")
}

func num(n int, bold bool) string {
	if n == 0 {
		return "·"
	}
	if bold {
		return fmt.Sprintf("**%d**", n)
	}
	return fmt.Sprint(n)
}

func (r *renderer) destructive(b *strings.Builder, maxRows int) {
	var rows []string
	for _, rep := range r.reports {
		for _, c := range rep.Changes {
			if !c.Action.Destructive() {
				continue
			}
			root := ""
			if r.multi() {
				root = "`" + rep.Name + "` | "
			}
			rows = append(rows, fmt.Sprintf("| %s %s | %s%s | %s |", actionIcons[c.Action], actionName(c.Action), root, r.link(c), why(c)))
		}
	}
	if len(rows) == 0 {
		return
	}
	b.WriteString("#### Destructive changes\n\n")
	if r.multi() {
		b.WriteString("| | Root | Resource | Why |\n|---|---|---|---|\n")
	} else {
		b.WriteString("| | Resource | Why |\n|---|---|---|\n")
	}
	shown := rows
	if maxRows >= 0 && len(rows) > maxRows {
		shown = rows[:maxRows]
	}
	b.WriteString(strings.Join(shown, "\n"))
	b.WriteString("\n")
	if len(shown) < len(rows) {
		fmt.Fprintf(b, "\n_…and %d more._\n", len(rows)-len(shown))
	}
	b.WriteString("\n")
}

// why explains a destructive change in one line.
func why(c report.Change) string {
	var parts []string
	if len(c.ReplacedBy) > 0 {
		attrs := make([]string, len(c.ReplacedBy))
		for i, p := range c.ReplacedBy {
			attrs[i] = "`" + p + "`"
		}
		verb := "forces"
		if len(attrs) > 1 {
			verb = "force"
		}
		parts = append(parts, strings.Join(attrs, ", ")+" "+verb+" replacement")
	}
	if c.Reason != "" {
		parts = append(parts, c.Reason)
	}
	if c.CreateBeforeDestroy {
		parts = append(parts, "create before destroy")
	}
	return strings.Join(parts, "; ")
}

func (r *renderer) deltaSection(b *strings.Builder) {
	d := r.delta
	if d == nil {
		return
	}
	since := ""
	if d.Since != "" {
		since = " since `" + short(d.Since) + "`"
	}
	if d.Unchanged {
		fmt.Fprintf(b, "_Plan unchanged%s._\n\n", since)
		return
	}
	fmt.Fprintf(b, "<details open><summary><b>What changed%s</b></summary>\n\n", since)
	write := func(icon, what string, refs []Ref) {
		for _, ref := range refs {
			root := ""
			if r.multi() {
				root = "`" + ref.Root + "` "
			}
			fmt.Fprintf(b, "- %s %s`%s` %s\n", icon, root, ref.Address, what)
		}
	}
	write("🆕", "is now planned", d.New)
	write("🔀", "has a different diff", d.Changed)
	write("✅", "is no longer planned", d.Dropped)
	b.WriteString("\n</details>\n\n")
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func (r *renderer) details(b *strings.Builder, rep *report.Report, t tier) {
	counts := rep.Counts()
	title := "Plan details"
	if rep.Name != "" {
		title = "<code>" + html.EscapeString(rep.Name) + "</code>"
	}
	open := " open"
	if r.multi() || counts.Total() > 25 {
		open = ""
	}
	if counts.Total() == 0 && len(rep.Drift) == 0 && len(rep.Outputs) == 0 {
		if r.multi() {
			return // the root table already says "no changes"
		}
		b.WriteString("No changes. Your infrastructure matches the configuration.\n\n")
		return
	}
	fmt.Fprintf(b, "<details%s><summary><b>%s</b>: %s</summary>\n\n", open, title, summary(counts, false))

	var reads []report.Change
	for _, c := range rep.Changes {
		if c.Action == report.Read {
			reads = append(reads, c)
			continue
		}
		full := t <= tierTrimmed || c.Action.Destructive() || len(c.Findings) > 0
		r.change(b, c, t, full)
	}
	if len(reads) > 0 {
		fmt.Fprintf(b, "<details><summary>%s read during apply</summary>\n\n", plural(len(reads), "data source"))
		for _, c := range reads {
			fmt.Fprintf(b, "- %s %s\n", r.link(c), c.Reason)
		}
		b.WriteString("\n</details>\n\n")
	}
	if len(rep.Outputs) > 0 && t <= tierTrimmed {
		b.WriteString("**Outputs**\n\n")
		var lines []string
		for _, o := range rep.Outputs {
			for _, a := range o.Attrs {
				name := o.Name
				if a.Path != "" {
					name += "." + a.Path
				}
				a.Path = name
				lines = append(lines, attrLines(a, t)...)
			}
		}
		writeFence(b, lines)
	}
	if len(rep.Drift) > 0 {
		fmt.Fprintf(b, "<details><summary>⚠️ %s changed outside of OpenTofu</summary>\n\n", plural(len(rep.Drift), "resource"))
		b.WriteString("These differences were found while refreshing state. They are not caused by this change, but applying it will reconcile them.\n\n")
		for _, c := range rep.Drift {
			r.change(b, c, t, t <= tierTrimmed)
		}
		b.WriteString("</details>\n\n")
	}
	if rep.Hidden > 0 {
		fmt.Fprintf(b, "_%s hidden by ignore rules._\n\n", plural(rep.Hidden, "change"))
	}
	b.WriteString("</details>\n\n")
}

func (r *renderer) change(b *strings.Builder, c report.Change, t tier, full bool) {
	fmt.Fprintf(b, "%s **%s** %s", actionIcons[c.Action], actionName(c.Action), r.link(c))
	switch {
	case c.Moved():
		fmt.Fprintf(b, " (moved from `%s`)", c.PreviousAddress)
	case c.Importing && c.Action != report.Import:
		b.WriteString(" (imported)")
	}
	if c.Action == report.Forget {
		b.WriteString(": removed from state, the real resource is kept")
	} else if w := why(c); w != "" {
		b.WriteString(": " + w)
	}
	b.WriteString("\n\n")
	if !full || len(c.Attrs) == 0 {
		return
	}
	attrs := orderAttrs(c.Attrs)
	var lines []string
	omitted := 0
	for i, a := range attrs {
		if t >= tierTrimmed && (lowValue(a) || i >= trimmedMaxAttrs) {
			omitted++
			continue
		}
		lines = append(lines, attrLines(a, t)...)
	}
	if omitted > 0 {
		lines = append(lines, fmt.Sprintf("  # %s not shown", plural(omitted, "more attribute")))
	}
	if len(lines) > 0 {
		writeFence(b, lines)
	}
}

// orderAttrs puts attributes that force replacement first and values that
// merely become unknown last.
func orderAttrs(attrs []diff.Attr) []diff.Attr {
	out := slices.Clone(attrs)
	rank := func(a diff.Attr) int {
		switch {
		case a.ForcesReplacement:
			return 0
		case lowValue(a):
			return 2
		}
		return 1
	}
	slices.SortStableFunc(out, func(x, y diff.Attr) int { return rank(x) - rank(y) })
	return out
}

func lowValue(a diff.Attr) bool {
	return a.After == diff.Unknown && !a.ForcesReplacement
}

func attrLines(a diff.Attr, t tier) []string {
	path := a.Path
	if path == "" {
		path = "(value)"
	}
	suffix := ""
	if a.ForcesReplacement {
		suffix = "  # forces replacement"
	}
	if a.Lines == nil {
		switch a.Kind {
		case diff.Added:
			return []string{fmt.Sprintf("+ %s = %s%s", path, a.After, suffix)}
		case diff.Removed:
			return []string{fmt.Sprintf("- %s = %s%s", path, a.Before, suffix)}
		default:
			return []string{fmt.Sprintf("! %s = %s -> %s%s", path, a.Before, a.After, suffix)}
		}
	}
	op := "!"
	switch a.Kind {
	case diff.Added:
		op = "+"
	case diff.Removed:
		op = "-"
	}
	format := ""
	if a.Format == "json" {
		format = " (JSON)"
	}
	out := []string{fmt.Sprintf("%s %s%s:%s", op, path, format, suffix)}
	for i, l := range a.Lines {
		if t >= tierTrimmed && i >= trimmedMaxLines {
			out = append(out, fmt.Sprintf("      … %d more lines", len(a.Lines)-i))
			break
		}
		if l.Op == '~' {
			out = append(out, "      …")
			continue
		}
		out = append(out, fmt.Sprintf("%c     %s", l.Op, l.Text))
	}
	return out
}

// writeFence writes a diff code block whose fence cannot be closed by
// backticks inside the content.
func writeFence(b *strings.Builder, lines []string) {
	fence := "```"
	for _, l := range lines {
		for strings.Contains(l, fence) {
			fence += "`"
		}
	}
	fmt.Fprintf(b, "%sdiff\n%s\n%s\n\n", fence, strings.Join(lines, "\n"), fence)
}

func (r *renderer) link(c report.Change) string {
	addr := "`" + c.Address + "`"
	if c.Source == nil || r.opts.BlobURL == "" {
		return addr
	}
	return fmt.Sprintf("[%s](%s/%s#L%d)", addr, r.opts.BlobURL, c.Source.File, c.Source.Line)
}

func (r *renderer) footer(b *strings.Builder) {
	var parts []string
	parts = append(parts, "tofu-plan-review")
	if len(r.reports) > 0 && r.reports[0].TofuVersion != "" {
		parts = append(parts, "OpenTofu "+r.reports[0].TofuVersion)
	}
	if r.opts.Commit != "" {
		parts = append(parts, "planned at `"+short(r.opts.Commit)+"`")
	}
	fmt.Fprintf(b, "<sub>%s</sub>\n", strings.Join(parts, " · "))
	if s := r.state.encode(); s != "" {
		fmt.Fprintf(b, "<!-- tofu-plan-review:state:%s -->\n", s)
	}
}

// truncate is the last resort when even the smallest tier does not fit.
func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	const note = "\n\n_Truncated._\n"
	cut := limit - len(note)
	for cut > 0 && (s[cut]&0xC0) == 0x80 { // don't split a UTF-8 sequence
		cut--
	}
	return s[:cut] + note
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
