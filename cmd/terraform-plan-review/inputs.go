package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/tofu-contrib/terraform-plan-review/internal/plan"
	"github.com/tofu-contrib/terraform-plan-review/internal/policy"
	"github.com/tofu-contrib/terraform-plan-review/internal/report"
	"github.com/tofu-contrib/terraform-plan-review/internal/source"
)

// inputFlags are shared by every command that reads plans.
func inputFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "name",
			Usage:   "display name of the root (default: --dir relative to the repository root)",
			Sources: env("NAME"),
		},
		&cli.StringFlag{
			Name:    "dir",
			Usage:   "root module directory, used to resolve source locations and to run tofu show",
			Sources: env("DIR"),
			Value:   ".",
		},
		&cli.StringFlag{
			Name:    "repo-root",
			Usage:   "repository root (default: git top level of --dir)",
			Sources: env("REPO_ROOT"),
		},
		&cli.StringFlag{
			Name:    "config",
			Usage:   "rules file (default: <repo-root>/" + policy.DefaultFile + ")",
			Sources: env("CONFIG"),
		},
		&cli.StringFlag{
			Name:    "tofu",
			Usage:   "tofu binary used to convert binary plan files",
			Sources: env("TOFU"),
			Value:   "tofu",
		},
	}
}

// analyzer turns plans into reports using the input flags.
type analyzer struct {
	name     string
	dir      string
	repoRoot string
	config   string
	tofu     string
}

func newAnalyzer(cmd *cli.Command) *analyzer {
	return &analyzer{
		name:     cmd.String("name"),
		dir:      cmd.String("dir"),
		repoRoot: cmd.String("repo-root"),
		config:   cmd.String("config"),
		tofu:     cmd.String("tofu"),
	}
}

func (a *analyzer) analyze(planPath string) (*report.Report, error) {
	p, err := plan.Load(planPath, a.tofu, a.dir)
	if err != nil {
		return nil, err
	}
	repoRoot := a.repoRoot
	if repoRoot == "" {
		repoRoot = gitTopLevel(a.dir)
	}
	cfg, err := a.policy(repoRoot)
	if err != nil {
		return nil, err
	}
	idx, err := source.Build(a.dir, repoRoot, p.Configuration.RootModule)
	if err != nil {
		return nil, fmt.Errorf("indexing sources in %s: %w", a.dir, err)
	}
	dir := "."
	if abs, err := filepath.Abs(a.dir); err == nil {
		if rel, err := filepath.Rel(repoRoot, abs); err == nil {
			dir = filepath.ToSlash(rel)
		}
	}
	name := a.name
	if name == "" && dir != "." {
		name = dir
	}
	return report.Analyze(p, report.Options{Name: name, Dir: dir, Policy: cfg, Sources: idx}), nil
}

// policy loads --config, or the default rules file in repoRoot. Only the
// default may be missing, which means no rules.
func (a *analyzer) policy(repoRoot string) (*policy.Config, error) {
	path := a.config
	if path == "" {
		found, legacy, err := policy.Find(repoRoot)
		if err != nil {
			return nil, err
		}
		if found == "" {
			return &policy.Config{}, nil
		}
		if legacy {
			fmt.Fprintf(os.Stderr, "terraform-plan-review: %s is deprecated, move it to %s\n",
				policy.LegacyFile, policy.DefaultFile)
		}
		path = found
	}
	cfg, err := policy.Load(path)
	if err != nil {
		return nil, fmt.Errorf("loading rules: %w", err)
	}
	return cfg, nil
}

func gitTopLevel(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	if ws := os.Getenv("GITHUB_WORKSPACE"); ws != "" {
		return ws
	}
	wd, _ := os.Getwd()
	return wd
}

// load reads plans and reports. Plans are analyzed with a.
func (a *analyzer) load(paths []string) ([]*report.Report, error) {
	if len(paths) == 0 {
		return nil, errors.New("no input files")
	}
	var reports []*report.Report
	seen := map[string]bool{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var r *report.Report
		if report.IsReport(data) {
			r, err = report.Decode(data)
		} else {
			r, err = a.analyze(path)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if seen[r.Name] {
			return nil, fmt.Errorf("%s: duplicate root name %q; give each root a distinct --name", path, r.Name)
		}
		seen[r.Name] = true
		reports = append(reports, r)
	}
	return reports, nil
}

// renderFlags are shared by the commands that produce Markdown.
func renderFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "id",
			Usage:   "comment identifier; use distinct ids for independent comments on one pull request",
			Sources: env("ID"),
			Value:   "default",
		},
		&cli.StringFlag{
			Name:    "title",
			Usage:   "comment title",
			Sources: env("TITLE"),
			Value:   "OpenTofu plan",
		},
	}
}
