// Package plan decodes the JSON plan format produced by `tofu show -json`
// (and `terraform show -json`, which shares the format).
//
// Values in a JSON plan are NOT redacted: sensitive values appear in plain
// text and are only flagged by the *_sensitive masks. Nothing in this package
// may be rendered directly; use the report package, which applies the masks.
package plan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Plan struct {
	FormatVersion   string            `json:"format_version"`
	TofuVersion     string            `json:"terraform_version"`
	ResourceChanges []ResourceChange  `json:"resource_changes"`
	ResourceDrift   []ResourceChange  `json:"resource_drift"`
	OutputChanges   map[string]Change `json:"output_changes"`
	Configuration   Configuration     `json:"configuration"`
	Errored         bool              `json:"errored"`
	Complete        *bool             `json:"complete"`
	Applyable       *bool             `json:"applyable"`
	Timestamp       string            `json:"timestamp"`
}

type ResourceChange struct {
	Address         string `json:"address"`
	PreviousAddress string `json:"previous_address"`
	ModuleAddress   string `json:"module_address"`
	Mode            string `json:"mode"`
	Type            string `json:"type"`
	Name            string `json:"name"`
	Index           any    `json:"index"`
	ProviderName    string `json:"provider_name"`
	Deposed         string `json:"deposed"`
	Change          Change `json:"change"`
	ActionReason    string `json:"action_reason"`
}

type Change struct {
	Actions         []string   `json:"actions"`
	Before          any        `json:"before"`
	After           any        `json:"after"`
	AfterUnknown    any        `json:"after_unknown"`
	BeforeSensitive any        `json:"before_sensitive"`
	AfterSensitive  any        `json:"after_sensitive"`
	ReplacePaths    [][]any    `json:"replace_paths"`
	Importing       *Importing `json:"importing"`
}

type Importing struct {
	ID string `json:"id"`
}

type Configuration struct {
	RootModule Module `json:"root_module"`
}

type Module struct {
	ModuleCalls map[string]ModuleCall `json:"module_calls"`
}

type ModuleCall struct {
	Source string `json:"source"`
	Module Module `json:"module"`
}

// Decode parses a JSON plan.
func Decode(data []byte) (*Plan, error) {
	var p Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("decoding JSON plan: %w", err)
	}
	if p.FormatVersion == "" {
		return nil, errors.New("not a JSON plan: missing format_version (did you pass the output of `tofu show -json`?)")
	}
	return &p, nil
}

// IsJSON reports whether data looks like a JSON document rather than a
// binary plan file (which is a zip archive).
func IsJSON(data []byte) bool {
	data = bytes.TrimSpace(data)
	return len(data) > 0 && data[0] == '{'
}

// Load reads a plan from path. If the file is a binary plan, it is converted
// with `<bin> show -json` executed in dir, so encryption settings
// (TF_ENCRYPTION) and provider schemas from that working directory apply.
func Load(path, bin, dir string) (*Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !IsJSON(data) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		if data, err = showJSON(abs, bin, dir); err != nil {
			return nil, err
		}
	}
	return Decode(data)
}

func showJSON(path, bin, dir string) ([]byte, error) {
	if bin == "" {
		bin = "tofu"
	}
	cmd := exec.Command(bin, "show", "-json", path)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s show -json %s: %w\n%s", bin, path, err, stderr.String())
	}
	return out, nil
}
