package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/tofu-contrib/tofu-plan-review/internal/plan"
	"github.com/tofu-contrib/tofu-plan-review/internal/policy"
	"github.com/tofu-contrib/tofu-plan-review/internal/source"
)

func load(t *testing.T, name, dir, rules string) *Report {
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
	return Analyze(p, Options{Name: name, Policy: cfg, Sources: idx})
}

func byAddress(r *Report) map[string]Change {
	m := map[string]Change{}
	for _, c := range r.Changes {
		m[c.Address] = c
	}
	return m
}

// TestNoSecretsInReport is the most important test in this repository:
// values the plan marks sensitive must not reach the report in any form,
// including derived attributes tofu itself prints in clear text.
func TestNoSecretsInReport(t *testing.T) {
	for _, tc := range []struct{ plan, dir string }{
		{"basic", "../../testdata/scenarios/basic/v2"},
		{"database", "../../testdata/handwritten/database"},
	} {
		r := load(t, tc.plan, tc.dir, "")
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"not-a-real-secret", "fake-db-password"} {
			if strings.Contains(string(data), secret) {
				t.Errorf("%s: report contains sensitive value %q", tc.plan, secret)
			}
		}
	}
}

func TestBasicActions(t *testing.T) {
	r := load(t, "basic", "../../testdata/scenarios/basic/v2", "")
	changes := byAddress(r)
	want := map[string]Action{
		"terraform_data.old":                   Delete,
		"terraform_data.server":                Replace,
		"terraform_data.forgotten":             Forget,
		"terraform_data.policy":                Update,
		"terraform_data.list":                  Update,
		"terraform_data.secret":                Update,
		"module.app.terraform_data.deployment": Update,
		"terraform_data.new":                   Create,
		"terraform_data.renamed_to":            Move,
	}
	if len(changes) != len(want) {
		t.Errorf("got %d changes, want %d", len(changes), len(want))
	}
	for addr, a := range want {
		if got := changes[addr].Action; got != a {
			t.Errorf("%s: action %q, want %q", addr, got, a)
		}
	}
	if _, ok := changes["terraform_data.kept"]; ok {
		t.Error("no-op change should be dropped")
	}

	server := changes["terraform_data.server"]
	if len(server.ReplacedBy) != 1 || server.ReplacedBy[0] != "triggers_replace" {
		t.Errorf("ReplacedBy = %v", server.ReplacedBy)
	}
	forced := false
	for _, a := range server.Attrs {
		if a.Path == "triggers_replace[0]" && a.ForcesReplacement {
			forced = true
		}
	}
	if !forced {
		t.Error("triggers_replace[0] should be marked as forcing replacement")
	}
	if mv := changes["terraform_data.renamed_to"]; mv.PreviousAddress != "terraform_data.renamed_from" {
		t.Errorf("PreviousAddress = %q", mv.PreviousAddress)
	}
	if r.Changes[0].Action != Delete || r.Changes[1].Action != Replace {
		t.Errorf("destructive changes should sort first, got %s, %s", r.Changes[0].Action, r.Changes[1].Action)
	}
}

func TestPolicy(t *testing.T) {
	r := load(t, "database", "../../testdata/handwritten/database", "../../testdata/handwritten/database/rules.hcl")
	changes := byAddress(r)

	if r.OverrideLabel != "destroy-approved" {
		t.Errorf("OverrideLabel = %q", r.OverrideLabel)
	}
	if b := r.Blocked(); len(b) != 1 || b[0].Address != "aws_db_instance.main" {
		t.Errorf("Blocked = %v", b)
	}
	if f := changes["aws_iam_role.ci"].Findings; len(f) != 1 || f[0].Severity != policy.Warn {
		t.Errorf("iam findings = %v", f)
	}
	if f := changes["data.aws_iam_policy_document.assume"].Findings; len(f) != 0 {
		t.Errorf("reads should not match rules without the read action: %v", f)
	}
	// tags_all-only update is hidden; the bucket keeps its tags change.
	if _, ok := changes["aws_instance.web"]; ok {
		t.Error("aws_instance.web should be hidden by the ignore rule")
	}
	if r.Hidden != 1 {
		t.Errorf("Hidden = %d", r.Hidden)
	}
	for _, a := range changes["aws_s3_bucket.logs"].Attrs {
		if strings.HasPrefix(a.Path, "tags_all") {
			t.Errorf("tags_all should be ignored: %+v", a)
		}
	}
	if !r.Incomplete {
		t.Error("complete=false should mark the report incomplete")
	}
	if len(r.Drift) != 1 {
		t.Errorf("Drift = %v", r.Drift)
	}
	if src := changes["aws_iam_role.ci"].Source; src == nil || src.Line != 21 {
		t.Errorf("iam role source = %v", src)
	}
}

func TestIgnoreNeverHidesFindings(t *testing.T) {
	cfg := &policy.Config{Rules: []policy.Rule{
		{Name: "hide-all", Severity: policy.Ignore},
		{Name: "watch", Severity: policy.Warn, ResourceTypes: []string{"aws_iam_role"}},
	}}
	p, err := plan.Load("../../testdata/plans/database.json", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r := Analyze(p, Options{Policy: cfg})
	var kept []string
	for _, c := range r.Changes {
		if c.Action != Read { // rules only match reads that ask for them
			kept = append(kept, c.Address)
		}
	}
	if len(kept) != 1 || kept[0] != "aws_iam_role.ci" {
		t.Errorf("only the change with a finding should remain, got %v", kept)
	}
}

func TestDigestStable(t *testing.T) {
	a := load(t, "basic", "../../testdata/scenarios/basic/v2", "")
	b := load(t, "basic", "../../testdata/scenarios/basic/v2", "")
	for i := range a.Changes {
		if a.Changes[i].Digest != b.Changes[i].Digest {
			t.Errorf("%s: digest not stable", a.Changes[i].Address)
		}
	}
}
