// Package diff computes attribute-level differences between the before and
// after values of a planned change.
//
// Every value is passed through redact (driven by the plan's sensitivity
// masks) before it is formatted, so the output of this package never
// contains a value the plan marks as sensitive.
package diff

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

const (
	Sensitive = "(sensitive value)"
	Unknown   = "(known after apply)"
)

type Kind string

const (
	Added   Kind = "add"
	Removed Kind = "remove"
	Changed Kind = "change"
)

// Attr is a change to a single attribute path.
type Attr struct {
	Path   string `json:"path"`
	Kind   Kind   `json:"kind"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	// Lines holds a line diff for structured (JSON) or multi-line strings.
	// When set, Before and After are empty.
	Lines             []Line `json:"lines,omitempty"`
	Format            string `json:"format,omitempty"` // "json" or "text" when Lines is set
	Sensitive         bool   `json:"sensitive,omitempty"`
	ForcesReplacement bool   `json:"forces_replacement,omitempty"`
}

// Context is the number of unchanged lines shown around changes in
// structured and multi-line string diffs.
const Context = 3

// absent marks a key that does not exist on one side of the change. JSON
// plans omit unknown values from "after", so absence is distinct from null.
type absentT struct{}

var absent = absentT{}

// Values diffs before and after. Pass nil for a side that does not exist
// (before of a create). The masks are the plan's before_sensitive,
// after_sensitive and after_unknown structures.
func Values(before, after, beforeSens, afterSens, afterUnknown any) []Attr {
	w := &walker{}
	var b, a any = before, after
	if before == nil {
		b = absent
	}
	if after == nil {
		a = absent
	}
	w.walk(nil, b, a, beforeSens, afterSens, afterUnknown)
	return w.out
}

type walker struct {
	out []Attr
}

func (w *walker) emit(path []any, attr Attr) {
	attr.Path = FormatPath(path)
	w.out = append(w.out, attr)
}

func isNull(v any) bool { return v == nil || v == absent }

func (w *walker) walk(path []any, b, a, bs, as, au any) {
	unknown := au == true
	if bs == true || as == true {
		w.walkSensitive(path, b, a, unknown)
		return
	}
	if unknown {
		attr := Attr{Kind: Changed, After: Unknown}
		if isNull(b) {
			attr.Kind = Added
		} else {
			attr.Before = compact(redact(b, bs))
		}
		w.emit(path, attr)
		return
	}

	bm, bIsMap := b.(map[string]any)
	am, aIsMap := a.(map[string]any)
	bl, bIsList := b.([]any)
	al, aIsList := a.([]any)

	switch {
	case (bIsMap || isNull(b)) && (aIsMap || isNull(a)) && (bIsMap || aIsMap):
		keys := map[string]bool{}
		for k := range bm {
			keys[k] = true
		}
		for k := range am {
			keys[k] = true
		}
		// Keys only present in after_unknown are values that will be known
		// after apply; they are absent from "after".
		if aum, ok := au.(map[string]any); ok {
			for k, v := range aum {
				if v == true {
					keys[k] = true
				}
			}
		}
		for _, k := range sortedKeys(keys) {
			cb, ok := bm[k]
			if !ok {
				cb = absent
			}
			ca, ok := am[k]
			if !ok {
				ca = absent
			}
			w.walk(append(path, k), cb, ca, child(bs, k), child(as, k), child(au, k))
		}

	case (bIsList || isNull(b)) && (aIsList || isNull(a)) && (bIsList || aIsList):
		if allScalar(bl) && allScalar(al) && !anyMasked(bs) && !anyMasked(as) && !anyMasked(au) {
			w.scalarList(path, bl, al)
			return
		}
		for i := 0; i < max(len(bl), len(al)); i++ {
			var cb, ca any = absent, absent
			if i < len(bl) {
				cb = bl[i]
			}
			if i < len(al) {
				ca = al[i]
			}
			w.walk(append(path, i), cb, ca, child(bs, i), child(as, i), child(au, i))
		}

	default:
		w.leaf(path, redact(b, bs), redact(a, as))
	}
}

func (w *walker) walkSensitive(path []any, b, a any, unknown bool) {
	switch {
	case unknown && isNull(b):
		w.emit(path, Attr{Kind: Added, After: Unknown, Sensitive: true})
	case unknown:
		w.emit(path, Attr{Kind: Changed, Before: Sensitive, After: Unknown, Sensitive: true})
	case reflect.DeepEqual(b, a), isNull(b) && isNull(a):
	case isNull(b):
		w.emit(path, Attr{Kind: Added, After: Sensitive, Sensitive: true})
	case isNull(a):
		w.emit(path, Attr{Kind: Removed, Before: Sensitive, Sensitive: true})
	default:
		w.emit(path, Attr{Kind: Changed, Before: Sensitive, After: Sensitive, Sensitive: true})
	}
}

func (w *walker) leaf(path []any, b, a any) {
	if reflect.DeepEqual(b, a) || (isNull(b) && isNull(a)) {
		return
	}
	bs, bStr := b.(string)
	as, aStr := a.(string)
	if (bStr || isNull(b)) && (aStr || isNull(a)) {
		if lines, format, ok := stringLines(bs, as); ok {
			attr := Attr{Kind: Changed, Lines: lines, Format: format}
			if isNull(b) {
				attr.Kind = Added
			} else if isNull(a) {
				attr.Kind = Removed
			}
			w.emit(path, attr)
			return
		}
	}
	switch {
	case isNull(b):
		w.emit(path, Attr{Kind: Added, After: compact(a)})
	case isNull(a):
		w.emit(path, Attr{Kind: Removed, Before: compact(b)})
	default:
		w.emit(path, Attr{Kind: Changed, Before: compact(b), After: compact(a)})
	}
}

// scalarList diffs lists of scalars as sequences, so inserting or removing
// one element does not show every following element as changed.
func (w *walker) scalarList(path []any, b, a []any) {
	bt := make([]string, len(b))
	for i, v := range b {
		bt[i] = compact(v)
	}
	at := make([]string, len(a))
	for i, v := range a {
		at[i] = compact(v)
	}
	// Pair each run of removals with the additions that follow it, so a
	// replaced element reads as one change rather than a remove and an add.
	var dels, adds []Line
	bi, ai := 0, 0
	flush := func() {
		n := min(len(dels), len(adds))
		for i := 0; i < n; i++ {
			w.emit(append(path, ai-len(adds)+i), Attr{Kind: Changed, Before: dels[i].Text, After: adds[i].Text})
		}
		for i := n; i < len(dels); i++ {
			w.emit(append(path, bi-len(dels)+i), Attr{Kind: Removed, Before: dels[i].Text})
		}
		for i := n; i < len(adds); i++ {
			w.emit(append(path, ai-len(adds)+i), Attr{Kind: Added, After: adds[i].Text})
		}
		dels, adds = nil, nil
	}
	for _, l := range lcsOps(bt, at) {
		switch l.Op {
		case ' ':
			flush()
			bi++
			ai++
		case '-':
			if len(adds) > 0 {
				flush()
			}
			dels = append(dels, l)
			bi++
		case '+':
			adds = append(adds, l)
			ai++
		}
	}
	flush()
}

// stringLines returns a line diff when either side is structured JSON or
// spans multiple lines; plain one-line strings are shown as before -> after.
func stringLines(b, a string) ([]Line, string, bool) {
	bj, bOK := prettyJSON(b)
	aj, aOK := prettyJSON(a)
	if (bOK || b == "") && (aOK || a == "") && (bOK || aOK) {
		return Lines(splitLines(bj), splitLines(aj), Context), "json", true
	}
	if strings.Contains(b, "\n") || strings.Contains(a, "\n") {
		return Lines(splitLines(b), splitLines(a), Context), "text", true
	}
	return nil, "", false
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// prettyJSON reformats s if it is a JSON object or array. Object keys are
// sorted so that provider-side key reordering does not show up as a change.
func prettyJSON(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if t == "" || (t[0] != '{' && t[0] != '[') {
		return "", false
	}
	dec := json.NewDecoder(strings.NewReader(t))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil || dec.More() {
		return "", false
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "", false
	}
	return strings.TrimSuffix(buf.String(), "\n"), true
}

// redact replaces every subtree of v marked sensitive by mask.
func redact(v, mask any) any {
	if v == absent {
		return v
	}
	if mask == true {
		return Sensitive
	}
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, cv := range t {
			out[k] = redact(cv, child(mask, k))
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, cv := range t {
			out[i] = redact(cv, child(mask, i))
		}
		return out
	}
	return v
}

func child(mask any, key any) any {
	switch m := mask.(type) {
	case bool:
		if m {
			return true
		}
	case map[string]any:
		if k, ok := key.(string); ok {
			return m[k]
		}
	case []any:
		if i, ok := key.(int); ok && i < len(m) {
			return m[i]
		}
	}
	return nil
}

func anyMasked(mask any) bool {
	switch m := mask.(type) {
	case bool:
		return m
	case map[string]any:
		for _, v := range m {
			if anyMasked(v) {
				return true
			}
		}
	case []any:
		for _, v := range m {
			if anyMasked(v) {
				return true
			}
		}
	}
	return false
}

func allScalar(l []any) bool {
	for _, v := range l {
		switch v.(type) {
		case map[string]any, []any:
			return false
		}
	}
	return true
}

const maxCompact = 200

// compact formats a (redacted) value on one line.
func compact(v any) string {
	if v == absent || v == nil {
		return "null"
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Sprint(v)
	}
	s := strings.TrimSuffix(buf.String(), "\n")
	// The redaction marker is a plain string; don't show it quoted.
	s = strings.ReplaceAll(s, `"`+Sensitive+`"`, Sensitive)
	if r := []rune(s); len(r) > maxCompact {
		s = string(r[:maxCompact]) + "…"
	}
	return s
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// FormatPath renders a path as it would be written in HCL, e.g.
// ingress[0].cidr_blocks or tags["kubernetes.io/role"].
func FormatPath(path []any) string {
	var sb strings.Builder
	for i, step := range path {
		switch s := step.(type) {
		case string:
			if identRe.MatchString(s) {
				if i > 0 {
					sb.WriteByte('.')
				}
				sb.WriteString(s)
			} else {
				q, _ := json.Marshal(s)
				fmt.Fprintf(&sb, "[%s]", q)
			}
		case int:
			fmt.Fprintf(&sb, "[%d]", s)
		case float64: // replace_paths decode indexes as JSON numbers
			fmt.Fprintf(&sb, "[%d]", int(s))
		default:
			fmt.Fprintf(&sb, "[%v]", s)
		}
	}
	return sb.String()
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SensitiveStrings returns the string values of v that mask marks as
// sensitive. Numbers and booleans are skipped: they are too short to be
// matched safely elsewhere.
func SensitiveStrings(v, mask any) []string {
	var out []string
	var collect func(v any)
	collect = func(v any) {
		switch t := v.(type) {
		case string:
			out = append(out, t)
		case map[string]any:
			for _, cv := range t {
				collect(cv)
			}
		case []any:
			for _, cv := range t {
				collect(cv)
			}
		}
	}
	var walk func(v, mask any)
	walk = func(v, mask any) {
		if mask == true {
			collect(v)
			return
		}
		switch t := v.(type) {
		case map[string]any:
			for k, cv := range t {
				walk(cv, child(mask, k))
			}
		case []any:
			for i, cv := range t {
				walk(cv, child(mask, i))
			}
		}
	}
	walk(v, mask)
	return out
}
