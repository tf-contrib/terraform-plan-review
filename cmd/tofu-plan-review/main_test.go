package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	basicPlan    = "../../testdata/plans/basic.json"
	basicDir     = "../../testdata/scenarios/basic/v2"
	databasePlan = "../../testdata/plans/database.json"
	databaseDir  = "../../testdata/handwritten/database"
	rules        = "../../testdata/handwritten/database/rules.hcl"
)

type comment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// fakeGitHub stores the comments of PR #1 in memory.
type fakeGitHub struct {
	mu       sync.Mutex
	comments []comment
	server   *httptest.Server
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var in struct{ Body string }
		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/o/r/issues/1/comments":
			_ = json.NewEncoder(w).Encode(f.comments)
		case r.Method == "POST" && r.URL.Path == "/repos/o/r/issues/1/comments":
			_ = json.NewDecoder(r.Body).Decode(&in)
			f.comments = append(f.comments, comment{ID: int64(len(f.comments) + 1), Body: in.Body})
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, "{}")
		case r.Method == "PATCH" && strings.HasPrefix(r.URL.Path, "/repos/o/r/issues/comments/"):
			_ = json.NewDecoder(r.Body).Decode(&in)
			var id int64
			fmt.Sscan(strings.TrimPrefix(r.URL.Path, "/repos/o/r/issues/comments/"), &id)
			f.comments[id-1].Body = in.Body
			_, _ = io.WriteString(w, "{}")
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

// actionsEnv sets up the environment of a pull_request workflow run and
// returns the paths of the output and summary files.
func actionsEnv(t *testing.T, api string, labels ...string) (outputs, summary string) {
	dir := t.TempDir()
	var ls []map[string]string
	for _, l := range labels {
		ls = append(ls, map[string]string{"name": l})
	}
	event, _ := json.Marshal(map[string]any{"pull_request": map[string]any{
		"number": 1, "head": map[string]string{"sha": "0123456789abcdef"}, "labels": ls,
	}})
	eventPath := filepath.Join(dir, "event.json")
	if err := os.WriteFile(eventPath, event, 0o644); err != nil {
		t.Fatal(err)
	}
	outputs, summary = filepath.Join(dir, "outputs"), filepath.Join(dir, "summary")
	for k, v := range map[string]string{
		"GITHUB_EVENT_PATH":   eventPath,
		"GITHUB_REPOSITORY":   "o/r",
		"GITHUB_API_URL":      api,
		"GITHUB_SERVER_URL":   "https://github.com",
		"GITHUB_RUN_ID":       "42",
		"GITHUB_TOKEN":        "test-token",
		"GITHUB_OUTPUT":       outputs,
		"GITHUB_STEP_SUMMARY": summary,
	} {
		t.Setenv(k, v)
	}
	return outputs, summary
}

func readOutputs(t *testing.T, path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	lines := strings.Split(string(data), "\n")
	for i := 0; i+1 < len(lines); i++ {
		if name, ok := strings.CutSuffix(lines[i], "<<__TPR_EOF__"); ok {
			out[name] = lines[i+1]
		}
	}
	return out
}

// run executes the CLI and returns what it wrote to stdout, where
// annotations and dry-run output go.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var stdout bytes.Buffer
	app := NewApp()
	app.Writer = &stdout
	err := app.Run(context.Background(), append([]string{"tofu-plan-review"}, args...))
	return stdout.String(), err
}

func TestCommentCreatesThenUpdates(t *testing.T) {
	gh := newFakeGitHub(t)
	outputs, summary := actionsEnv(t, gh.server.URL)
	args := []string{"--dir", basicDir, "--repo-root", "../..", "--name", "basic", basicPlan}

	stdout, err := run(t, append([]string{"comment"}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	if len(gh.comments) != 1 {
		t.Fatalf("want 1 comment, got %d", len(gh.comments))
	}
	first := gh.comments[0].Body
	for _, want := range []string{
		"<!-- tofu-plan-review:id=default -->",
		"https://github.com/o/r/blob/0123456789abcdef/testdata/scenarios/basic/v2/main.tf#L7",
		"tofu-plan-review:state:",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("comment missing %q", want)
		}
	}
	if !strings.Contains(stdout, "::warning file=testdata/scenarios/basic/v2/main.tf,line=7,") {
		t.Errorf("missing replacement annotation in:\n%s", stdout)
	}
	out := readOutputs(t, outputs)
	if out["to-destroy"] != "1" || out["to-replace"] != "1" || out["destructive"] != "true" || out["blocked"] != "false" {
		t.Errorf("outputs = %v", out)
	}
	if s, _ := os.ReadFile(summary); !strings.Contains(string(s), "Destructive changes") {
		t.Error("job summary not written")
	}

	// Second run on the same PR: the comment is updated, not duplicated,
	// and says the plan did not change.
	if _, err := run(t, append([]string{"comment"}, args...)...); err != nil {
		t.Fatal(err)
	}
	if len(gh.comments) != 1 {
		t.Fatalf("want the comment updated in place, got %d comments", len(gh.comments))
	}
	if !strings.Contains(gh.comments[0].Body, "_Plan unchanged since `0123456`._") {
		t.Error("updated comment should report the plan as unchanged")
	}
	if s, _ := os.ReadFile(summary); !strings.Contains(string(s), "Plan unchanged") {
		t.Error("job summary should include the delta too")
	}
}

func TestCommentBlocked(t *testing.T) {
	gh := newFakeGitHub(t)
	outputs, _ := actionsEnv(t, gh.server.URL)
	args := []string{"--dir", databaseDir, "--repo-root", "../..", "--config", rules, databasePlan}

	stdout, err := run(t, append([]string{"comment"}, args...)...)
	var blocked errBlocked
	if !errors.As(err, &blocked) || blocked.n != 1 {
		t.Fatalf("err = %v", err)
	}
	if len(gh.comments) != 1 {
		t.Error("the comment must be posted even when blocked")
	}
	if !strings.Contains(stdout, "::error file=testdata/handwritten/database/main.tf,line=3,") {
		t.Errorf("blocked change should be an error annotation:\n%s", stdout)
	}
	if readOutputs(t, outputs)["blocked"] != "true" {
		t.Error("blocked output should be true")
	}
}

func TestCommentOverrideLabel(t *testing.T) {
	gh := newFakeGitHub(t)
	outputs, _ := actionsEnv(t, gh.server.URL, "destroy-approved")
	args := []string{"--dir", databaseDir, "--repo-root", "../..", "--config", rules, databasePlan}

	if _, err := run(t, append([]string{"comment"}, args...)...); err != nil {
		t.Fatalf("override label should unblock: %v", err)
	}
	if readOutputs(t, outputs)["blocked"] != "false" {
		t.Error("blocked output should be false")
	}
	if !strings.Contains(gh.comments[0].Body, "overridden by label") {
		t.Error("comment should say the block was overridden")
	}
}

func TestCommentWithoutPullRequest(t *testing.T) {
	t.Setenv("GITHUB_EVENT_PATH", "")
	t.Setenv("GITHUB_OUTPUT", "")
	t.Setenv("GITHUB_STEP_SUMMARY", "")
	_, err := run(t, "comment", "--dir", basicDir, "--repo-root", "../..", "--annotate=false", basicPlan)
	if err != nil {
		t.Fatalf("outside a PR the comment is skipped, not an error: %v", err)
	}
}

func TestAnalyzeThenCombine(t *testing.T) {
	dir := t.TempDir()
	reports := map[string][]string{
		"basic":    {"--dir", basicDir, "--name", "basic", basicPlan},
		"database": {"--dir", databaseDir, "--name", "database", "--config", rules, databasePlan},
	}
	var paths []string
	for name, args := range reports {
		out := filepath.Join(dir, name+".json")
		if _, err := run(t, append([]string{"analyze", "--repo-root", "../..", "--out", out}, args...)...); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(out)
		if strings.Contains(string(data), "not-a-real-secret") || strings.Contains(string(data), "fake-db-password") {
			t.Fatalf("%s report contains a sensitive value", name)
		}
		paths = append(paths, out)
	}
	out, err := run(t, append([]string{"render"}, paths...)...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| `basic` |") || !strings.Contains(out, "| `database` |") {
		t.Errorf("combined render should have a row per root:\n%s", out)
	}
}

func TestDuplicateRootNames(t *testing.T) {
	_, err := (&analyzer{dir: basicDir, repoRoot: "../..", name: "same"}).load([]string{basicPlan, basicPlan})
	if err == nil || !strings.Contains(err.Error(), "duplicate root name") {
		t.Errorf("err = %v", err)
	}
}

func TestNoInputs(t *testing.T) {
	if _, err := (&analyzer{}).load(nil); err == nil {
		t.Error("expected an error")
	}
}

func TestEnvironmentVariables(t *testing.T) {
	t.Setenv("TOFU_PLAN_REVIEW_DIR", basicDir)
	t.Setenv("TOFU_PLAN_REVIEW_REPO_ROOT", "../..")
	t.Setenv("TOFU_PLAN_REVIEW_TITLE", "From env")
	out, err := run(t, "render", "--", basicPlan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "### 🔴 From env:") {
		t.Errorf("title from TOFU_PLAN_REVIEW_TITLE not applied:\n%s", out[:min(len(out), 300)])
	}
	if !strings.Contains(out, "testdata/scenarios/basic/v2/main.tf") && !strings.Contains(out, "terraform_data.server") {
		t.Error("dir from TOFU_PLAN_REVIEW_DIR not applied")
	}
}

func TestAnalyzeRequiresOnePlan(t *testing.T) {
	if _, err := run(t, "analyze", basicPlan, basicPlan); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Errorf("err = %v", err)
	}
}

func TestDefaultConfigPath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".github", "tofu"), 0o755); err != nil {
		t.Fatal(err)
	}
	rules := `rule "no-deletes" {
  severity = "block"
  actions  = ["delete"]
}
`
	if err := os.WriteFile(filepath.Join(root, ".github", "tofu", "review.hcl"), []byte(rules), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, _ := filepath.Abs(basicPlan)
	out, err := run(t, "render", "--dir", root, "--repo-root", root, plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Blocked by policy") {
		t.Error("rules in .github/tofu/review.hcl should be loaded by default")
	}
}
