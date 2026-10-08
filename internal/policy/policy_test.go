package policy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.hcl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want a not-exist error, got %v", err)
	}
}

func TestFind(t *testing.T) {
	root := t.TempDir()
	write := func(name string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	check := func(wantFile string) {
		t.Helper()
		file, err := Find(root)
		if err != nil {
			t.Fatal(err)
		}
		if wantFile != "" {
			wantFile = filepath.Join(root, wantFile)
		}
		if file != wantFile {
			t.Errorf("got %q, want %q", file, wantFile)
		}
	}

	check("")
	write(DefaultFile)
	check(DefaultFile)
}

func TestLoadValidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.hcl")
	err := os.WriteFile(path, []byte(`
rule "bad" {
  severity = "fatal"
  actions  = ["destroy"]
}
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Load(path)
	if err == nil {
		t.Fatal("expected validation errors")
	}
	for _, want := range []string{`severity must be block, warn or ignore`, `unknown action "destroy"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestMatches(t *testing.T) {
	r := Rule{ResourceTypes: []string{"aws_db_*"}, Actions: []string{"delete", "replace"}}
	if !r.MatchesResource("aws_db_instance", "aws_db_instance.main", "replace") {
		t.Error("should match")
	}
	if r.MatchesResource("aws_db_instance", "aws_db_instance.main", "update") {
		t.Error("action filter ignored")
	}
	if (Rule{}).MatchesResource("x", "data.x.y", "read") {
		t.Error("reads should only match rules that list read")
	}

	attrs := Rule{Attributes: []string{"tags_all", "ingress[*].cidr_blocks"}}
	for path, want := range map[string]bool{
		"tags_all":                  true,
		"tags_all.env":              true,
		`tags_all["k8s.io/x"]`:      true,
		"tags_allx":                 false,
		"ingress[0].cidr_blocks":    true,
		"ingress[0].cidr_blocks[1]": true,
		"egress[0].cidr_blocks":     false,
	} {
		if got := attrs.MatchesAttribute(path); got != want {
			t.Errorf("MatchesAttribute(%q) = %v", path, got)
		}
	}
}

func TestGlob(t *testing.T) {
	for _, tt := range []struct {
		pattern, s string
		want       bool
	}{
		{"aws_*", "aws_instance", true},
		{"aws_*", "google_x", false},
		{"module.prod.*", `module.prod.aws_db_instance.main["a"]`, true},
		{"*.main", "aws_db_instance.main", true},
		{"aws_?b_instance", "aws_db_instance", true},
		{"x[0]", "x[0]", true},
		{"x[*]", "x[12]", true},
		{"", "", true},
		{"a", "", false},
	} {
		if got := glob(tt.pattern, tt.s); got != tt.want {
			t.Errorf("glob(%q, %q) = %v", tt.pattern, tt.s, got)
		}
	}
}
