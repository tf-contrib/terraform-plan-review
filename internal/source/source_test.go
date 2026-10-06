package source

import (
	"testing"

	"github.com/tofu-contrib/tofu-plan-review/internal/plan"
)

func TestStripKeys(t *testing.T) {
	tests := map[string]string{
		`aws_instance.web`:    `aws_instance.web`,
		`aws_instance.web[0]`: `aws_instance.web`,
		`module.a["x.y"].module.b[2].aws_s3_bucket.l["k]"]`: `module.a.module.b.aws_s3_bucket.l`,
		`module.a["say \"hi\""].t.n`:                        `module.a.t.n`,
	}
	for in, want := range tests {
		if got := StripKeys(in); got != want {
			t.Errorf("StripKeys(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestModulePath(t *testing.T) {
	if got := modulePath(`module.a[0].module.b["x"]`); got != "a.b" {
		t.Errorf("got %q", got)
	}
	if got := modulePath(""); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestLookup(t *testing.T) {
	root := plan.Module{ModuleCalls: map[string]plan.ModuleCall{
		"app":    {Source: "./modules/app"},
		"remote": {Source: "registry.opentofu.org/org/mod/aws"},
	}}
	idx, err := Build("../../testdata/scenarios/basic/v2", "../..", root)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		rc   plan.ResourceChange
		want Location
	}{
		{plan.ResourceChange{Mode: "managed", Type: "terraform_data", Name: "server", Address: "terraform_data.server"},
			Location{"testdata/scenarios/basic/v2/main.tf", 7}},
		{plan.ResourceChange{Mode: "managed", Type: "terraform_data", Name: "deployment", ModuleAddress: "module.app", Address: "module.app.terraform_data.deployment"},
			Location{"testdata/scenarios/basic/v2/modules/app/main.tf", 2}},
		// No resource block left: falls back to the removed block.
		{plan.ResourceChange{Mode: "managed", Type: "terraform_data", Name: "forgotten", Address: "terraform_data.forgotten"},
			Location{"testdata/scenarios/basic/v2/main.tf", 18}},
	}
	for _, tt := range tests {
		got, ok := idx.Lookup(tt.rc)
		if !ok || got != tt.want {
			t.Errorf("Lookup(%s) = %v, %v; want %v", tt.rc.Address, got, ok, tt.want)
		}
	}
	if _, ok := idx.Lookup(plan.ResourceChange{Type: "terraform_data", Name: "old", Address: "terraform_data.old"}); ok {
		t.Error("deleted resource should have no location")
	}
}
