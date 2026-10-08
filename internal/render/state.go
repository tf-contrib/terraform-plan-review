package render

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"regexp"
	"sort"

	"github.com/tf-contrib/terraform-plan-review/internal/report"
)

// State is embedded in the comment as a hidden HTML comment so the next run
// can tell reviewers what changed since the plan they last looked at. It
// holds addresses and content digests only, never values.
type State struct {
	Version int                          `json:"v"`
	Commit  string                       `json:"c,omitempty"`
	Roots   map[string]map[string]string `json:"r"`
}

// maxStateLen bounds the encoded state; very large plans skip the delta
// rather than eat into the comment budget.
const maxStateLen = 8 << 10

const stateVersion = 1

func NewState(commit string, reports []*report.Report) *State {
	s := &State{Version: stateVersion, Commit: commit, Roots: map[string]map[string]string{}}
	for _, r := range reports {
		m := map[string]string{}
		for _, c := range r.Changes {
			m[c.Address] = c.Digest
		}
		s.Roots[r.Name] = m
	}
	return s
}

func (s *State) encode() string {
	data, _ := json.Marshal(s)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(data)
	_ = zw.Close()
	enc := base64.StdEncoding.EncodeToString(buf.Bytes())
	if len(enc) > maxStateLen {
		return ""
	}
	return enc
}

var stateRe = regexp.MustCompile(`<!-- terraform-plan-review:state:([A-Za-z0-9+/=]+) -->`)

// ParseState extracts the state from a previously posted comment body.
func ParseState(body string) *State {
	m := stateRe.FindStringSubmatch(body)
	if m == nil {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		return nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(zr, 10<<20))
	if err != nil {
		return nil
	}
	var s State
	if json.Unmarshal(data, &s) != nil || s.Version != stateVersion {
		return nil
	}
	return &s
}

// Delta lists what changed between two plans of the same roots.
type Delta struct {
	Since     string
	New       []Ref
	Changed   []Ref
	Dropped   []Ref
	Unchanged bool
}

type Ref struct {
	Root    string
	Address string
}

// Compare computes the delta from prev to cur. Roots missing from either
// side are skipped: a root may simply not have been planned in this run.
func Compare(prev, cur *State) *Delta {
	if prev == nil || cur == nil {
		return nil
	}
	d := &Delta{Since: prev.Commit}
	for root, curChanges := range cur.Roots {
		prevChanges, ok := prev.Roots[root]
		if !ok {
			continue
		}
		for addr, dig := range curChanges {
			switch pd, ok := prevChanges[addr]; {
			case !ok:
				d.New = append(d.New, Ref{root, addr})
			case pd != dig:
				d.Changed = append(d.Changed, Ref{root, addr})
			}
		}
		for addr := range prevChanges {
			if _, ok := curChanges[addr]; !ok {
				d.Dropped = append(d.Dropped, Ref{root, addr})
			}
		}
	}
	for _, refs := range [][]Ref{d.New, d.Changed, d.Dropped} {
		sort.Slice(refs, func(i, j int) bool {
			if refs[i].Root != refs[j].Root {
				return refs[i].Root < refs[j].Root
			}
			return refs[i].Address < refs[j].Address
		})
	}
	d.Unchanged = len(d.New)+len(d.Changed)+len(d.Dropped) == 0
	return d
}
