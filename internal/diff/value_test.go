package diff

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func decode(t *testing.T, s string) any {
	t.Helper()
	if s == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestValues(t *testing.T) {
	tests := []struct {
		name                      string
		before, after, bs, as, au string
		want                      []Attr
	}{
		{
			name:   "nested map change, add and remove",
			before: `{"size":"small","tags":{"env":"dev","owner":"a"}}`,
			after:  `{"size":"large","tags":{"env":"dev","team":"b"}}`,
			want: []Attr{
				{Path: "size", Kind: Changed, Before: `"small"`, After: `"large"`},
				{Path: "tags.owner", Kind: Removed, Before: `"a"`},
				{Path: "tags.team", Kind: Added, After: `"b"`},
			},
		},
		{
			name:   "unknown values are absent from after",
			before: `{"id":"x","output":"v"}`,
			after:  `{"id":"x"}`,
			au:     `{"output":true}`,
			want:   []Attr{{Path: "output", Kind: Changed, Before: `"v"`, After: Unknown}},
		},
		{
			name:  "create with unknown",
			after: `{"name":"a"}`,
			au:    `{"id":true}`,
			want: []Attr{
				{Path: "id", Kind: Added, After: Unknown},
				{Path: "name", Kind: Added, After: `"a"`},
			},
		},
		{
			name:   "sensitive leaf",
			before: `{"password":"p1"}`,
			after:  `{"password":"p2"}`,
			bs:     `{"password":true}`,
			as:     `{"password":true}`,
			want:   []Attr{{Path: "password", Kind: Changed, Before: Sensitive, After: Sensitive, Sensitive: true}},
		},
		{
			name:   "unchanged sensitive leaf is omitted",
			before: `{"password":"p1"}`,
			after:  `{"password":"p1"}`,
			bs:     `{"password":true}`,
			as:     `{"password":true}`,
		},
		{
			name:   "sensitive parent becoming unknown",
			before: `{"conn":{"user":"u","pass":"p"}}`,
			after:  `{}`,
			bs:     `{"conn":{"pass":true}}`,
			au:     `{"conn":true}`,
			want:   []Attr{{Path: "conn", Kind: Changed, Before: `{"pass":(sensitive value),"user":"u"}`, After: Unknown}},
		},
		{
			name:   "type change redacts nested sensitive values",
			before: `{"v":{"secret":"s3cr3t"}}`,
			after:  `{"v":"plain"}`,
			bs:     `{"v":{"secret":true}}`,
			want:   []Attr{{Path: "v", Kind: Changed, Before: `{"secret":(sensitive value)}`, After: `"plain"`}},
		},
		{
			name:   "scalar list insert and delete",
			before: `{"cidrs":["a","b","c"]}`,
			after:  `{"cidrs":["a","c","d"]}`,
			want: []Attr{
				{Path: "cidrs[1]", Kind: Removed, Before: `"b"`},
				{Path: "cidrs[2]", Kind: Added, After: `"d"`},
			},
		},
		{
			name:   "scalar list replaced element",
			before: `{"t":["v1"]}`,
			after:  `{"t":["v2"]}`,
			want:   []Attr{{Path: "t[0]", Kind: Changed, Before: `"v1"`, After: `"v2"`}},
		},
		{
			name:   "list of objects by index",
			before: `{"rule":[{"port":80},{"port":443}]}`,
			after:  `{"rule":[{"port":80},{"port":8443}]}`,
			want:   []Attr{{Path: "rule[1].port", Kind: Changed, Before: "443", After: "8443"}},
		},
		{
			name:   "keys needing quotes",
			before: `{"tags":{}}`,
			after:  `{"tags":{"kubernetes.io/role":"x"}}`,
			want:   []Attr{{Path: `tags["kubernetes.io/role"]`, Kind: Added, After: `"x"`}},
		},
		{
			name:   "null to value is an add",
			before: `{"a":null}`,
			after:  `{"a":1}`,
			want:   []Attr{{Path: "a", Kind: Added, After: "1"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Values(decode(t, tt.before), decode(t, tt.after), decode(t, tt.bs), decode(t, tt.as), decode(t, tt.au))
			if !reflect.DeepEqual(got, tt.want) {
				gj, _ := json.MarshalIndent(got, "", "  ")
				wj, _ := json.MarshalIndent(tt.want, "", "  ")
				t.Errorf("got\n%s\nwant\n%s", gj, wj)
			}
		})
	}
}

func TestValuesJSONString(t *testing.T) {
	before := map[string]any{"policy": `{"Statement":[{"Action":"s3:GetObject","Effect":"Allow"}]}`}
	// Same document, keys reordered, one value changed.
	after := map[string]any{"policy": `{"Statement":[{"Effect":"Allow","Action":["s3:GetObject","s3:PutObject"]}]}`}
	got := Values(before, after, nil, nil, nil)
	if len(got) != 1 || got[0].Format != "json" {
		t.Fatalf("want one JSON attr, got %+v", got)
	}
	var added, removed []string
	for _, l := range got[0].Lines {
		switch l.Op {
		case '+':
			added = append(added, strings.TrimSpace(l.Text))
		case '-':
			removed = append(removed, strings.TrimSpace(l.Text))
		}
	}
	if !reflect.DeepEqual(removed, []string{`"Action": "s3:GetObject",`}) {
		t.Errorf("removed = %q", removed)
	}
	if !reflect.DeepEqual(added, []string{`"Action": [`, `"s3:GetObject",`, `"s3:PutObject"`, `],`}) {
		t.Errorf("added = %q", added)
	}
}

func TestValuesJSONKeyOrderOnly(t *testing.T) {
	got := Values(
		map[string]any{"p": `{"a":1,"b":2}`},
		map[string]any{"p": `{"b":2,"a":1}`},
		nil, nil, nil)
	for _, a := range got {
		for _, l := range a.Lines {
			if l.Op == '+' || l.Op == '-' {
				t.Fatalf("key reordering should not produce changed lines: %+v", got)
			}
		}
	}
}

func TestValuesMultilineString(t *testing.T) {
	got := Values(
		map[string]any{"script": "line1\nline2\nline3\n"},
		map[string]any{"script": "line1\nline2 changed\nline3\n"},
		nil, nil, nil)
	if len(got) != 1 || got[0].Format != "text" {
		t.Fatalf("want one text attr, got %+v", got)
	}
	want := []Line{{' ', "line1"}, {'-', "line2"}, {'+', "line2 changed"}, {' ', "line3"}}
	if !reflect.DeepEqual(got[0].Lines, want) {
		t.Errorf("lines = %+v", got[0].Lines)
	}
}

func TestSensitiveStrings(t *testing.T) {
	v := decode(t, `{"a":"x","b":{"c":"secret","d":["s1","s2"]},"e":3}`)
	mask := decode(t, `{"b":{"c":true,"d":true}}`)
	got := SensitiveStrings(v, mask)
	want := map[string]bool{"secret": true, "s1": true, "s2": true}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for _, s := range got {
		if !want[s] {
			t.Errorf("unexpected %q", s)
		}
	}
}

func TestLinesContext(t *testing.T) {
	a := strings.Split("1 2 3 4 5 6 7 8 9 10", " ")
	b := strings.Split("1 2 3 4 5 six 7 8 9 10", " ")
	got := Lines(a, b, 1)
	want := []Line{{Op: '~'}, {' ', "5"}, {'-', "6"}, {'+', "six"}, {' ', "7"}, {Op: '~'}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v", got)
	}
}
