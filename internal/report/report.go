// Package report turns a JSON plan into a redacted, render-ready model.
//
// A Report contains no value the plan marks sensitive, so unlike plan JSON
// it is safe to upload as a workflow artifact and combine across jobs.
package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"

	"github.com/tofu-contrib/terraform-plan-review/internal/diff"
	"github.com/tofu-contrib/terraform-plan-review/internal/plan"
	"github.com/tofu-contrib/terraform-plan-review/internal/policy"
	"github.com/tofu-contrib/terraform-plan-review/internal/source"
)

// Schema identifies the report file format.
const Schema = 1

type Action string

const (
	Create  Action = "create"
	Update  Action = "update"
	Replace Action = "replace"
	Delete  Action = "delete"
	Forget  Action = "forget"
	Move    Action = "move"
	Import  Action = "import"
	Read    Action = "read"
)

// Order is the display order of actions: most dangerous first.
var Order = []Action{Delete, Replace, Forget, Update, Create, Import, Move, Read}

func (a Action) rank() int { return slices.Index(Order, a) }

// Destructive reports whether the action destroys infrastructure.
func (a Action) Destructive() bool { return a == Delete || a == Replace }

type Report struct {
	Schema           int    `json:"terraform_plan_review"`
	Name             string `json:"name,omitempty"`
	Dir              string `json:"dir,omitempty"`
	TerraformVersion string `json:"terraform_version,omitempty"`
	Errored          bool   `json:"errored,omitempty"`
	// Incomplete is set for plans created with -target or -exclude.
	Incomplete    bool     `json:"incomplete,omitempty"`
	Changes       []Change `json:"changes"`
	Drift         []Change `json:"drift,omitempty"`
	Outputs       []Output `json:"outputs,omitempty"`
	Hidden        int      `json:"hidden,omitempty"`
	OverrideLabel string   `json:"override_label,omitempty"`
}

type Change struct {
	Address             string           `json:"address"`
	PreviousAddress     string           `json:"previous_address,omitempty"`
	Type                string           `json:"type"`
	Mode                string           `json:"mode,omitempty"`
	Action              Action           `json:"action"`
	Importing           bool             `json:"importing,omitempty"`
	CreateBeforeDestroy bool             `json:"create_before_destroy,omitempty"`
	Reason              string           `json:"reason,omitempty"`
	ReplacedBy          []string         `json:"replaced_by,omitempty"`
	Attrs               []diff.Attr      `json:"attrs,omitempty"`
	Source              *source.Location `json:"source,omitempty"`
	Findings            []Finding        `json:"findings,omitempty"`
	// Digest identifies the content of the change (action and redacted
	// attribute diff) so successive plans can be compared.
	Digest string `json:"digest"`
}

func (c Change) Moved() bool { return c.PreviousAddress != "" && c.PreviousAddress != c.Address }

type Finding struct {
	Rule     string          `json:"rule"`
	Severity policy.Severity `json:"severity"`
	Message  string          `json:"message,omitempty"`
}

type Output struct {
	Name   string      `json:"name"`
	Action Action      `json:"action"`
	Attrs  []diff.Attr `json:"attrs,omitempty"`
}

type Options struct {
	Name    string
	Dir     string
	Policy  *policy.Config
	Sources *source.Index
}

// Analyze builds a report from a plan.
func Analyze(p *plan.Plan, opts Options) *Report {
	r := &Report{
		Schema:           Schema,
		Name:             opts.Name,
		Dir:              opts.Dir,
		TerraformVersion: p.TerraformVersion,
		Errored:          p.Errored,
		Incomplete:       p.Complete != nil && !*p.Complete,
	}
	for _, rc := range p.ResourceChanges {
		if c, ok := convert(rc, opts.Sources); ok {
			r.Changes = append(r.Changes, c)
		}
	}
	for _, rc := range p.ResourceDrift {
		if c, ok := convert(rc, opts.Sources); ok {
			r.Drift = append(r.Drift, c)
		}
	}
	for name, oc := range p.OutputChanges {
		a, ok := action(oc.Actions, false, false)
		if !ok {
			continue
		}
		r.Outputs = append(r.Outputs, Output{
			Name:   name,
			Action: a,
			Attrs:  diff.Values(oc.Before, oc.After, oc.BeforeSensitive, oc.AfterSensitive, oc.AfterUnknown),
		})
	}
	sort.Slice(r.Outputs, func(i, j int) bool { return r.Outputs[i].Name < r.Outputs[j].Name })

	newScrubber(p).report(r)

	if opts.Policy != nil {
		r.OverrideLabel = opts.Policy.OverrideLabel
		r.applyPolicy(opts.Policy)
	}
	for i := range r.Changes {
		r.Changes[i].Digest = digest(r.Changes[i])
	}
	for i := range r.Drift {
		r.Drift[i].Digest = digest(r.Drift[i])
	}
	sortChanges(r.Changes)
	sortChanges(r.Drift)
	return r
}

func convert(rc plan.ResourceChange, sources *source.Index) (Change, bool) {
	ch := rc.Change
	moved := rc.PreviousAddress != "" && rc.PreviousAddress != rc.Address
	a, ok := action(ch.Actions, moved, ch.Importing != nil)
	if !ok {
		return Change{}, false
	}
	c := Change{
		Address:             rc.Address,
		PreviousAddress:     rc.PreviousAddress,
		Type:                rc.Type,
		Mode:                rc.Mode,
		Action:              a,
		Importing:           ch.Importing != nil,
		CreateBeforeDestroy: slices.Equal(ch.Actions, []string{"create", "delete"}),
		Reason:              reason(rc.ActionReason, len(ch.ReplacePaths) > 0),
	}
	if rc.Deposed != "" {
		c.Address += " (deposed " + rc.Deposed + ")"
	}
	switch a {
	case Create, Update, Replace:
		before := ch.Before
		if a == Create {
			before = nil
		}
		c.Attrs = diff.Values(before, ch.After, ch.BeforeSensitive, ch.AfterSensitive, ch.AfterUnknown)
	}
	for _, p := range ch.ReplacePaths {
		c.ReplacedBy = append(c.ReplacedBy, diff.FormatPath(p))
	}
	for i := range c.Attrs {
		for _, rp := range c.ReplacedBy {
			if covers(rp, c.Attrs[i].Path) {
				c.Attrs[i].ForcesReplacement = true
			}
		}
	}
	if loc, ok := sources.Lookup(rc); ok {
		c.Source = &loc
	}
	return c, true
}

func covers(prefix, path string) bool {
	if path == prefix {
		return true
	}
	if len(path) > len(prefix) && path[:len(prefix)] == prefix {
		next := path[len(prefix)]
		return next == '.' || next == '['
	}
	return false
}

// action maps plan actions to a single review action. ok is false for
// changes that have nothing to review.
func action(actions []string, moved, importing bool) (Action, bool) {
	switch {
	case slices.Equal(actions, []string{"create"}):
		return Create, true
	case slices.Equal(actions, []string{"update"}):
		return Update, true
	case slices.Equal(actions, []string{"delete", "create"}), slices.Equal(actions, []string{"create", "delete"}):
		return Replace, true
	case slices.Equal(actions, []string{"delete"}):
		return Delete, true
	case slices.Contains(actions, "forget"):
		return Forget, true
	case slices.Equal(actions, []string{"read"}):
		return Read, true
	case importing:
		return Import, true
	case moved:
		return Move, true
	}
	return "", false
}

var reasons = map[string]string{
	"replace_because_tainted":           "resource is tainted",
	"replace_by_request":                "replacement requested with -replace",
	"replace_by_triggers":               "replace_triggered_by",
	"delete_because_no_resource_config": "removed from configuration",
	"delete_because_wrong_repetition":   "count/for_each changed",
	"delete_because_count_index":        "count index out of range",
	"delete_because_each_key":           "for_each key no longer exists",
	"delete_because_no_module":          "module removed from configuration",
	"delete_because_no_move_target":     "moved target does not exist",
	"read_because_config_unknown":       "configuration depends on values known after apply",
	"read_because_dependency_pending":   "depends on a pending change",
	"read_because_check_nested":         "nested in a check block",
}

func reason(code string, hasReplacePaths bool) string {
	if code == "replace_because_cannot_update" {
		if hasReplacePaths {
			return "" // the attributes that force replacement say more
		}
		return "provider cannot update in place"
	}
	return reasons[code]
}

func (r *Report) applyPolicy(cfg *policy.Config) {
	var kept []Change
	for _, c := range r.Changes {
		var ignore []policy.Rule
		for _, rule := range cfg.Rules {
			if !rule.MatchesResource(c.Type, c.Address, string(c.Action)) {
				continue
			}
			if rule.Severity == policy.Ignore {
				ignore = append(ignore, rule)
				continue
			}
			if len(rule.Attributes) > 0 && !slices.ContainsFunc(c.Attrs, func(a diff.Attr) bool { return rule.MatchesAttribute(a.Path) }) {
				continue
			}
			c.Findings = append(c.Findings, Finding{Rule: rule.Name, Severity: rule.Severity, Message: rule.Message})
		}

		hide := false
		for _, rule := range ignore {
			if len(rule.Attributes) == 0 {
				hide = true
				continue
			}
			c.Attrs = slices.DeleteFunc(c.Attrs, func(a diff.Attr) bool { return rule.MatchesAttribute(a.Path) })
			if len(c.Attrs) == 0 && c.Action == Update && !c.Moved() && !c.Importing {
				hide = true
			}
		}
		// A change with findings is never hidden: ignore rules reduce
		// noise, they must not silence a warning or a block.
		if hide && len(c.Findings) == 0 {
			r.Hidden++
			continue
		}
		kept = append(kept, c)
	}
	r.Changes = kept
}

func digest(c Change) string {
	h := sha256.New()
	_ = json.NewEncoder(h).Encode(struct {
		Action Action
		Attrs  []diff.Attr
	}{c.Action, c.Attrs})
	return hex.EncodeToString(h.Sum(nil))[:12]
}

func sortChanges(cs []Change) {
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Action != cs[j].Action {
			return cs[i].Action.rank() < cs[j].Action.rank()
		}
		return cs[i].Address < cs[j].Address
	})
}

// Counts summarizes changes by action.
type Counts map[Action]int

func (r *Report) Counts() Counts {
	c := Counts{}
	for _, ch := range r.Changes {
		c[ch.Action]++
	}
	return c
}

func (c Counts) Total() int {
	n := 0
	for a, v := range c {
		if a != Read {
			n += v
		}
	}
	return n
}

// Blocked returns the changes with blocking findings.
func (r *Report) Blocked() []Change {
	var out []Change
	for _, c := range r.Changes {
		if slices.ContainsFunc(c.Findings, func(f Finding) bool { return f.Severity == policy.Block }) {
			out = append(out, c)
		}
	}
	return out
}

// Write saves the report as JSON.
func (r *Report) Write(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := r.Encode(f); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Encode writes the report as JSON.
func (r *Report) Encode(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// IsReport reports whether data is a report file rather than a plan.
func IsReport(data []byte) bool {
	var probe struct {
		Schema int `json:"terraform_plan_review"`
	}
	return json.Unmarshal(data, &probe) == nil && probe.Schema > 0
}

// Decode parses a report file.
func Decode(data []byte) (*Report, error) {
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("decoding report: %w", err)
	}
	if r.Schema != Schema {
		return nil, fmt.Errorf("unsupported report schema %d (this build supports %d)", r.Schema, Schema)
	}
	return &r, nil
}
