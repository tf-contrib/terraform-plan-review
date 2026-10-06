// Package policy loads review rules from .github/tofu-plan-review.hcl.
//
//	override_label = "destroy-approved"
//
//	rule "protect-databases" {
//	  severity       = "block"
//	  message        = "Databases must not be destroyed without approval."
//	  resource_types = ["aws_db_instance", "aws_rds_*"]
//	  actions        = ["delete", "replace"]
//	}
//
//	rule "hide-tags-all" {
//	  severity   = "ignore"
//	  attributes = ["tags_all"]
//	}
package policy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
)

// DefaultFile is where rules are read from, relative to the repository root.
const DefaultFile = ".github/tofu-plan-review.hcl"

// LegacyFile is the default location before 0.3.0. It is still read, with a
// deprecation notice, when DefaultFile is absent.
const LegacyFile = ".github/tofu/review.hcl"

// Find returns the rules file in repoRoot, or "" when there is none. legacy
// reports whether it was found at LegacyFile.
func Find(repoRoot string) (file string, legacy bool, err error) {
	for _, name := range []string{DefaultFile, LegacyFile} {
		path := filepath.Join(repoRoot, name)
		if _, err := os.Stat(path); err == nil {
			return path, name == LegacyFile, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", false, err
		}
	}
	return "", false, nil
}

type Severity string

const (
	Block  Severity = "block"
	Warn   Severity = "warn"
	Ignore Severity = "ignore"
)

// Actions a rule can match on.
var Actions = []string{"create", "update", "replace", "delete", "forget", "move", "import", "read"}

type Config struct {
	// OverrideLabel, when present on the pull request, downgrades blocking
	// findings to warnings.
	OverrideLabel string `hcl:"override_label,optional"`
	Rules         []Rule `hcl:"rule,block"`
}

type Rule struct {
	Name          string   `hcl:"name,label"`
	Severity      Severity `hcl:"severity"`
	Message       string   `hcl:"message,optional"`
	ResourceTypes []string `hcl:"resource_types,optional"`
	Addresses     []string `hcl:"addresses,optional"`
	Actions       []string `hcl:"actions,optional"`
	// Attributes restricts the rule to changes touching these attribute
	// paths. For ignore rules, only these attributes are hidden.
	Attributes []string `hcl:"attributes,optional"`
}

// Load reads a config file. A missing file is an error, so a mistyped path
// can't silently disable the rules.
func Load(file string) (*Config, error) {
	if _, err := os.Stat(file); err != nil {
		return nil, err
	}
	p := hclparse.NewParser()
	f, diags := p.ParseHCLFile(file)
	if diags.HasErrors() {
		return nil, diags
	}
	var cfg Config
	if diags := gohcl.DecodeBody(f.Body, nil, &cfg); diags.HasErrors() {
		return nil, diags
	}
	return &cfg, cfg.validate()
}

func (c *Config) validate() error {
	var errs []error
	for _, r := range c.Rules {
		switch r.Severity {
		case Block, Warn, Ignore:
		default:
			errs = append(errs, fmt.Errorf("rule %q: severity must be block, warn or ignore, got %q", r.Name, r.Severity))
		}
		for _, a := range r.Actions {
			if !slices.Contains(Actions, a) {
				errs = append(errs, fmt.Errorf("rule %q: unknown action %q (valid: %s)", r.Name, a, strings.Join(Actions, ", ")))
			}
		}
	}
	return errors.Join(errs...)
}

// MatchesResource reports whether the rule's resource and action filters
// match. Attribute filters are checked separately with MatchesAttribute.
//
// Data source reads only match rules that list the "read" action: they
// change nothing, so broad rules would only add noise.
func (r Rule) MatchesResource(resourceType, address, action string) bool {
	if action == "read" && !slices.Contains(r.Actions, "read") {
		return false
	}
	if len(r.Actions) > 0 && !slices.Contains(r.Actions, action) {
		return false
	}
	if len(r.ResourceTypes) > 0 && !matchAny(r.ResourceTypes, resourceType) {
		return false
	}
	if len(r.Addresses) > 0 && !matchAny(r.Addresses, address) {
		return false
	}
	return true
}

// MatchesAttribute reports whether attrPath is covered by the rule's
// attribute patterns. A pattern covers nested paths, so "tags" matches
// tags.env and tags["kubernetes.io/role"].
func (r Rule) MatchesAttribute(attrPath string) bool {
	for _, p := range r.Attributes {
		if glob(p, attrPath) || glob(p+".*", attrPath) || glob(p+"[*", attrPath) {
			return true
		}
	}
	return false
}

func matchAny(patterns []string, s string) bool {
	for _, p := range patterns {
		if glob(p, s) {
			return true
		}
	}
	return false
}

// glob matches s against a pattern where * matches any run of characters
// and ? any single character. Everything else is literal, including the
// brackets of instance keys and list indexes (ingress[*].cidr_blocks).
func glob(pattern, s string) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			for i := len(s); i >= 0; i-- {
				if glob(pattern[1:], s[i:]) {
					return true
				}
			}
			return false
		case '?':
			if s == "" {
				return false
			}
			_, size := utf8.DecodeRuneInString(s)
			pattern, s = pattern[1:], s[size:]
		default:
			if s == "" || s[0] != pattern[0] {
				return false
			}
			pattern, s = pattern[1:], s[1:]
		}
	}
	return s == ""
}
