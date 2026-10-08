package render

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tf-contrib/terraform-plan-review/internal/diff"
	"github.com/tf-contrib/terraform-plan-review/internal/plan"
	"github.com/tf-contrib/terraform-plan-review/internal/policy"
	"github.com/tf-contrib/terraform-plan-review/internal/report"
	"github.com/tf-contrib/terraform-plan-review/internal/source"
)

var update = flag.Bool("update", false, "rewrite golden files")

const blobURL = "https://github.com/example/infra/blob/0123456789abcdef"

func load(t *testing.T, name, dir, rules string) *report.Report {
	t.Helper()
	p, err := plan.Load("../../testdata/plans/"+name+".json", "", "")
	if err != nil {
		t.Fatal(err)
	}
	idx, err := source.Build(dir, "../..", p.Configuration.RootModule)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &policy.Config{}
	if rules != "" {
		if cfg, err = policy.Load(rules); err != nil {
			t.Fatal(err)
		}
	}
	return report.Analyze(p, report.Options{Name: name, Policy: cfg, Sources: idx})
}

func basic(t *testing.T) *report.Report {
	return load(t, "basic", "../../testdata/scenarios/basic/v2", "")
}

func database(t *testing.T) *report.Report {
	return load(t, "database", "../../testdata/handwritten/database", "../../testdata/handwritten/database/rules.hcl")
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("../../testdata/golden", name+".md")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/render -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from golden file; run go test ./internal/render -update and review the diff\n\n%s", path, got)
	}
}

func TestGolden(t *testing.T) {
	opts := Options{BlobURL: blobURL, Commit: "0123456789abcdef"}
	golden(t, "basic", Markdown([]*report.Report{basic(t)}, opts))
	golden(t, "database", Markdown([]*report.Report{database(t)}, opts))
	golden(t, "multi-root", Markdown([]*report.Report{basic(t), database(t)}, opts))

	overridden := opts
	overridden.Labels = []string{"destroy-approved"}
	golden(t, "database-overridden", Markdown([]*report.Report{database(t)}, overridden))
}

func TestNoSecretsInMarkdown(t *testing.T) {
	out := Markdown([]*report.Report{basic(t), database(t)}, Options{})
	for _, secret := range []string{"not-a-real-secret", "fake-db-password"} {
		if strings.Contains(out, secret) {
			t.Errorf("markdown contains sensitive value %q", secret)
		}
	}
}

func TestTiers(t *testing.T) {
	reports := []*report.Report{basic(t)}
	full := Markdown(reports, Options{})
	if strings.Contains(full, "omitted to fit") {
		t.Fatal("full render should not be trimmed")
	}
	for _, limit := range []int{len(full) - 1, 2500, 1200, 400} {
		out := Markdown(reports, Options{Limit: limit, DetailsURL: "https://example.com/run"})
		if len(out) > limit {
			t.Errorf("limit %d: got %d bytes", limit, len(out))
		}
		if !strings.HasPrefix(out, Marker("")) {
			t.Errorf("limit %d: marker must survive trimming", limit)
		}
	}
	// Trimming drops low-value attributes before dropping destructive diffs.
	trimmed := Markdown(reports, Options{Limit: len(full) - 1})
	if !strings.Contains(trimmed, `triggers_replace[0] = "v1" -> "v2"`) {
		t.Error("trimmed render should keep the replacement diff")
	}
	if strings.Contains(trimmed, "output = {") {
		t.Error("trimmed render should drop values that only become unknown")
	}
}

func TestDelta(t *testing.T) {
	r := basic(t)
	prev := NewState("aaaaaaa1111", []*report.Report{r})
	// Simulate the next push: one change dropped, one edited, one added.
	cur := *r
	cur.Changes = nil
	for _, c := range r.Changes {
		switch c.Address {
		case "terraform_data.old":
			continue
		case "terraform_data.list":
			c.Digest = "changed"
		}
		cur.Changes = append(cur.Changes, c)
	}
	cur.Changes = append(cur.Changes, report.Change{Address: "terraform_data.extra", Action: report.Create, Digest: "x"})

	body := Markdown([]*report.Report{r}, Options{Commit: "aaaaaaa1111"})
	parsed := ParseState(body)
	if parsed == nil || parsed.Commit != prev.Commit || len(parsed.Roots["basic"]) != len(r.Changes) {
		t.Fatalf("state did not round-trip: %+v", parsed)
	}
	out := Markdown([]*report.Report{&cur}, Options{Previous: parsed})
	for _, want := range []string{
		"What changed since `aaaaaaa`",
		"`terraform_data.extra` is now planned",
		"`terraform_data.list` has a different diff",
		"`terraform_data.old` is no longer planned",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}

	same := Markdown([]*report.Report{r}, Options{Previous: parsed})
	if !strings.Contains(same, "_Plan unchanged since `aaaaaaa`._") {
		t.Error("identical plan should say unchanged")
	}
}

func TestFenceEscaping(t *testing.T) {
	r := &report.Report{Schema: report.Schema, Changes: []report.Change{{
		Address: "terraform_data.md",
		Action:  report.Update,
		Attrs:   []diff.Attr{{Path: "input", Kind: diff.Changed, Before: "\"```\"", After: "\"````\""}},
	}}}
	out := Markdown([]*report.Report{r}, Options{})
	if !strings.Contains(out, "`````diff\n") {
		t.Errorf("fence should be longer than any backtick run in the content:\n%s", out)
	}
}

func TestHTMLInNamesIsEscaped(t *testing.T) {
	r := &report.Report{Schema: report.Schema, Name: "<img src=x>", Changes: []report.Change{{Address: "a.b", Action: report.Create}}}
	out := Markdown([]*report.Report{r}, Options{Title: "<script>"})
	if strings.Contains(out, "<img") || strings.Contains(out, "<script>") {
		t.Errorf("user-provided names must be escaped:\n%s", out)
	}
}

func TestFooter(t *testing.T) {
	db := database(t)
	db.TerraformVersion = "1.12.0"
	out := Markdown([]*report.Report{basic(t), db}, Options{
		Commit:      "0123456789abcdef",
		CommitURL:   "https://github.com/example/infra/commit/0123456789abcdef",
		ToolVersion: "0.2.0",
	})
	want := "<sub>[terraform-plan-review](" + ProjectURL + ") v0.2.0 · Terraform/OpenTofu 1.13.1, 1.12.0 · " +
		"plan for [`0123456`](https://github.com/example/infra/commit/0123456789abcdef)</sub>"
	if !strings.Contains(out, want) {
		t.Errorf("footer missing:\n%s", want)
	}
	if out := Markdown([]*report.Report{basic(t)}, Options{ToolVersion: "dev"}); !strings.Contains(out, "<sub>[terraform-plan-review]("+ProjectURL+") · Terraform/OpenTofu 1.13.1</sub>") {
		t.Error("dev builds should not show a version, and no commit means no SHA")
	}
}
