package report

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/tf-contrib/terraform-plan-review/internal/diff"
	"github.com/tf-contrib/terraform-plan-review/internal/plan"
)

// minSecretLen is the shortest sensitive value scrubbed by content. Shorter
// values would redact unrelated text (e.g. a sensitive "on" or "1").
const minSecretLen = 4

// scrubber is a second line of defence behind the sensitivity masks.
//
// Masks only cover attributes the provider or configuration marks
// sensitive. A value derived from a sensitive one can be unmasked (e.g.
// terraform_data.output mirroring a sensitive input), and tofu itself
// prints it. We collect every masked value in the plan and redact any
// occurrence of it elsewhere in the report.
type scrubber struct {
	replacer *strings.Replacer
}

func newScrubber(p *plan.Plan) *scrubber {
	seen := map[string]bool{}
	add := func(v, mask any) {
		for _, s := range diff.SensitiveStrings(v, mask) {
			if len(s) >= minSecretLen {
				seen[s] = true
			}
		}
	}
	for _, rcs := range [][]plan.ResourceChange{p.ResourceChanges, p.ResourceDrift} {
		for _, rc := range rcs {
			add(rc.Change.Before, rc.Change.BeforeSensitive)
			add(rc.Change.After, rc.Change.AfterSensitive)
		}
	}
	for _, oc := range p.OutputChanges {
		add(oc.Before, oc.BeforeSensitive)
		add(oc.After, oc.AfterSensitive)
	}
	if len(seen) == 0 {
		return &scrubber{}
	}

	// Values appear raw in line diffs and JSON-escaped in compact values.
	forms := map[string]bool{}
	for s := range seen {
		forms[s] = true
		q, _ := json.Marshal(s)
		forms[string(q)] = true // a whole quoted value: drop the quotes too
		forms[string(q[1:len(q)-1])] = true
	}
	list := make([]string, 0, len(forms))
	for s := range forms {
		list = append(list, s)
	}
	// Longest first, so a secret containing another is replaced whole.
	sort.Slice(list, func(i, j int) bool {
		if len(list[i]) != len(list[j]) {
			return len(list[i]) > len(list[j])
		}
		return list[i] < list[j]
	})
	pairs := make([]string, 0, 2*len(list))
	for _, s := range list {
		pairs = append(pairs, s, diff.Sensitive)
	}
	return &scrubber{replacer: strings.NewReplacer(pairs...)}
}

func (s *scrubber) attrs(attrs []diff.Attr) {
	if s.replacer == nil {
		return
	}
	for i := range attrs {
		a := &attrs[i]
		before, after := s.replacer.Replace(a.Before), s.replacer.Replace(a.After)
		if before != a.Before || after != a.After {
			a.Sensitive = true
		}
		a.Before, a.After = before, after
		for j := range a.Lines {
			if t := s.replacer.Replace(a.Lines[j].Text); t != a.Lines[j].Text {
				a.Lines[j].Text = t
				a.Sensitive = true
			}
		}
	}
}

func (s *scrubber) report(r *Report) {
	for i := range r.Changes {
		s.attrs(r.Changes[i].Attrs)
	}
	for i := range r.Drift {
		s.attrs(r.Drift[i].Attrs)
	}
	for i := range r.Outputs {
		s.attrs(r.Outputs[i].Attrs)
	}
}
