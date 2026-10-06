package plan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadJSON(t *testing.T) {
	p, err := Load("../../testdata/plans/basic.json", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if p.FormatVersion == "" || len(p.ResourceChanges) == 0 {
		t.Errorf("got %+v", p)
	}
	if p.Complete != nil {
		t.Error("OpenTofu plans do not set complete; it must stay nil, not false")
	}
}

func TestDecodeRejectsNonPlans(t *testing.T) {
	if _, err := Decode([]byte(`{"foo": 1}`)); err == nil || !strings.Contains(err.Error(), "format_version") {
		t.Errorf("err = %v", err)
	}
	if _, err := Decode([]byte(`{`)); err == nil {
		t.Error("invalid JSON should fail")
	}
}

func TestIsJSON(t *testing.T) {
	if !IsJSON([]byte("  \n{\"a\":1}")) {
		t.Error("JSON with leading whitespace")
	}
	if IsJSON([]byte("PK\x03\x04")) {
		t.Error("zip archive is a binary plan")
	}
}

func TestLoadBinaryPlanRunsShow(t *testing.T) {
	dir := t.TempDir()
	// A fake tofu that records its arguments and prints a minimal plan.
	fake := filepath.Join(dir, "tofu")
	script := "#!/bin/sh\necho \"$@\" > " + filepath.Join(dir, "args") + "\necho '{\"format_version\":\"1.2\"}'\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	planFile := filepath.Join(dir, "tfplan")
	if err := os.WriteFile(planFile, []byte("PK\x03\x04binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(planFile, fake, dir); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if got := strings.TrimSpace(string(args)); got != "show -json "+planFile {
		t.Errorf("args = %q", got)
	}
}

func TestLoadBinaryPlanReportsShowErrors(t *testing.T) {
	dir := t.TempDir()
	planFile := filepath.Join(dir, "tfplan")
	_ = os.WriteFile(planFile, []byte("PK"), 0o644)
	_, err := Load(planFile, "false", dir)
	if err == nil || !strings.Contains(err.Error(), "show -json") {
		t.Errorf("err = %v", err)
	}
}
