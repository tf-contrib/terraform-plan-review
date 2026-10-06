package github

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnnotationCommand(t *testing.T) {
	a := Annotation{Level: "warning", File: "infra/main.tf", Line: 7, Title: "a, b: c", Message: "50% done\nnext"}
	want := "::warning file=infra/main.tf,line=7,title=a%2C b%3A c::50%25 done%0Anext"
	if got := a.Command(); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestFromEnvPullRequest(t *testing.T) {
	event := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(event, []byte(`{
  "pull_request": {"number": 42, "head": {"sha": "abc123"}, "labels": [{"name": "destroy-approved"}]}
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_EVENT_PATH", event)
	t.Setenv("GITHUB_REPOSITORY", "example/infra")
	t.Setenv("GITHUB_SHA", "merge000")
	t.Setenv("GITHUB_RUN_ID", "99")
	t.Setenv("GITHUB_SERVER_URL", "")

	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.PR != 42 || c.Commit() != "abc123" || len(c.Labels) != 1 || c.Labels[0] != "destroy-approved" {
		t.Errorf("got %+v", c)
	}
	if got := c.BlobURL(); got != "https://github.com/example/infra/blob/abc123" {
		t.Errorf("BlobURL = %s", got)
	}
	if got := c.RunURL(); got != "https://github.com/example/infra/actions/runs/99" {
		t.Errorf("RunURL = %s", got)
	}
}

func TestFromEnvIssueComment(t *testing.T) {
	event := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(event, []byte(`{"issue": {"number": 7, "pull_request": {"url": "x"}, "labels": []}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_EVENT_PATH", event)
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.PR != 7 {
		t.Errorf("PR = %d", c.PR)
	}
}

func TestSetOutputMultiline(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	t.Setenv("GITHUB_OUTPUT", out)
	if err := SetOutput("x", "a\nb"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	if string(data) != "x<<__TPR_EOF__\na\nb\n__TPR_EOF__\n" {
		t.Errorf("got %q", data)
	}
}
