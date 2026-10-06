// Command tofu-plan-review renders OpenTofu plans for pull request review.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/tofu-contrib/tofu-plan-review/internal/github"
	"github.com/tofu-contrib/tofu-plan-review/internal/plan"
	"github.com/tofu-contrib/tofu-plan-review/internal/policy"
	"github.com/tofu-contrib/tofu-plan-review/internal/render"
	"github.com/tofu-contrib/tofu-plan-review/internal/report"
	"github.com/tofu-contrib/tofu-plan-review/internal/source"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = ""

const usage = `tofu-plan-review renders OpenTofu plans for pull request review.

Usage:
  tofu-plan-review analyze [flags] PLAN          Write a redacted report for one root
  tofu-plan-review render  [flags] INPUT...      Print the review as Markdown
  tofu-plan-review comment [flags] INPUT...      Post or update the pull request comment
  tofu-plan-review version

PLAN is a binary plan file (converted with "tofu show -json") or the JSON
output of "tofu show -json". INPUT is a PLAN or a report written by analyze;
use one analyze per root and pass all reports to comment for a combined view.

Run "tofu-plan-review <command> -h" for the flags of a command.
`

// exitBlocked is returned when a blocking policy rule matched.
const exitBlocked = 2

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}
	var err error
	switch cmd, args := os.Args[1], os.Args[2:]; cmd {
	case "analyze":
		err = runAnalyze(args)
	case "render":
		err = runRender(args, os.Stdout)
	case "comment":
		err = runComment(args)
	case "version", "-v", "--version":
		fmt.Println(buildVersion())
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(1)
	}
	var blocked errBlocked
	switch {
	case errors.As(err, &blocked):
		fmt.Fprintln(os.Stderr, "tofu-plan-review:", err)
		os.Exit(exitBlocked)
	case err != nil:
		fmt.Fprintln(os.Stderr, "tofu-plan-review:", err)
		os.Exit(1)
	}
}

type errBlocked struct{ n int }

func (e errBlocked) Error() string {
	return fmt.Sprintf("%d change(s) blocked by policy", e.n)
}

func buildVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "dev"
}

// analyzeFlags are shared by every command that reads plans.
type analyzeFlags struct {
	name     string
	dir      string
	repoRoot string
	config   string
	tofu     string
}

func (f *analyzeFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.name, "name", "", "display name of the root (default: -dir relative to the repository root)")
	fs.StringVar(&f.dir, "dir", ".", "root module directory: used for source links and to run `tofu show`")
	fs.StringVar(&f.repoRoot, "repo-root", "", "repository root (default: git top level of -dir)")
	fs.StringVar(&f.config, "config", "", "rules file (default: <repo-root>/"+policy.DefaultFile+")")
	fs.StringVar(&f.tofu, "tofu", "tofu", "tofu binary used to convert binary plan files")
}

func (f *analyzeFlags) analyze(planPath string) (*report.Report, error) {
	p, err := plan.Load(planPath, f.tofu, f.dir)
	if err != nil {
		return nil, err
	}
	repoRoot := f.repoRoot
	if repoRoot == "" {
		repoRoot = gitTopLevel(f.dir)
	}
	cfgPath := f.config
	if cfgPath == "" {
		cfgPath = filepath.Join(repoRoot, policy.DefaultFile)
	}
	cfg, err := policy.Load(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", cfgPath, err)
	}
	idx, err := source.Build(f.dir, repoRoot, p.Configuration.RootModule)
	if err != nil {
		return nil, fmt.Errorf("indexing sources in %s: %w", f.dir, err)
	}
	dir := "."
	if abs, err := filepath.Abs(f.dir); err == nil {
		if rel, err := filepath.Rel(repoRoot, abs); err == nil {
			dir = filepath.ToSlash(rel)
		}
	}
	name := f.name
	if name == "" && dir != "." {
		name = dir
	}
	return report.Analyze(p, report.Options{Name: name, Dir: dir, Policy: cfg, Sources: idx}), nil
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

func runAnalyze(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ExitOnError)
	var af analyzeFlags
	af.register(fs)
	out := fs.String("out", "", "write the report to this file (default: stdout)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("analyze takes exactly one plan file")
	}
	r, err := af.analyze(fs.Arg(0))
	if err != nil {
		return err
	}
	if *out == "" {
		*out = "/dev/stdout"
	}
	return r.Write(*out)
}

// loadInputs reads plans and reports. Plans are analyzed with af.
func loadInputs(paths []string, af *analyzeFlags) ([]*report.Report, error) {
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
			r, err = af.analyze(path)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if seen[r.Name] {
			return nil, fmt.Errorf("%s: duplicate root name %q; give each root a distinct -name", path, r.Name)
		}
		seen[r.Name] = true
		reports = append(reports, r)
	}
	return reports, nil
}

type renderFlags struct {
	id    string
	title string
}

func (f *renderFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.id, "id", "default", "comment identifier; use distinct ids for independent comments on one PR")
	fs.StringVar(&f.title, "title", "OpenTofu plan", "comment title")
}

func runRender(args []string, w io.Writer) error {
	fs := flag.NewFlagSet("render", flag.ExitOnError)
	var af analyzeFlags
	var rf renderFlags
	af.register(fs)
	rf.register(fs)
	blobURL := fs.String("blob-url", "", "base URL for source links, e.g. https://github.com/org/repo/blob/main")
	limit := fs.Int("limit", render.CommentLimit, "maximum output size in bytes")
	_ = fs.Parse(args)
	reports, err := loadInputs(fs.Args(), &af)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, render.Markdown(reports, render.Options{
		ID: rf.id, Title: rf.title, BlobURL: *blobURL, Limit: *limit,
	}))
	return err
}

func runComment(args []string) error {
	fs := flag.NewFlagSet("comment", flag.ExitOnError)
	var af analyzeFlags
	var rf renderFlags
	af.register(fs)
	rf.register(fs)
	pr := fs.Int("pr", 0, "pull request number (default: from the event payload)")
	dryRun := fs.Bool("dry-run", false, "print the comment instead of posting it")
	annotate := fs.Bool("annotate", true, "annotate changed resources in the pull request diff")
	summary := fs.Bool("summary", true, "write the full review to the job summary")
	failOnBlock := fs.Bool("fail-on-block", true, "exit with status 2 when a blocking rule matches")
	_ = fs.Parse(args)

	reports, err := loadInputs(fs.Args(), &af)
	if err != nil {
		return err
	}
	gh, err := github.FromEnv()
	if err != nil {
		return err
	}
	if *pr != 0 {
		gh.PR = *pr
	}
	opts := render.Options{
		ID: rf.id, Title: rf.title, BlobURL: gh.BlobURL(), Commit: gh.Commit(), Labels: gh.Labels,
	}

	// Find our previous comment first, so both the comment and the job
	// summary can say what changed since the last plan.
	var client *github.Client
	var existing *github.Comment
	post := !*dryRun && gh.PR != 0
	if post {
		if client, err = github.NewClient(gh); err != nil {
			return err
		}
		if existing, err = client.FindComment(gh.PR, render.Marker(opts.ID)); err != nil {
			return err
		}
		if existing != nil {
			opts.Previous = render.ParseState(existing.Body)
		}
	}

	if *summary {
		so := opts
		so.Limit = render.SummaryLimit
		if err := github.AppendSummary(render.Markdown(reports, so)); err != nil {
			return fmt.Errorf("writing job summary: %w", err)
		}
		opts.DetailsURL = gh.RunURL()
	}
	if *annotate {
		for _, a := range annotations(reports, gh.Labels) {
			fmt.Println(a.Command())
		}
	}
	if err := setOutputs(reports, gh.Labels); err != nil {
		return err
	}

	body := render.Markdown(reports, opts)
	switch {
	case *dryRun:
		fmt.Println(body)
	case !post:
		fmt.Fprintln(os.Stderr, "tofu-plan-review: not a pull request event and no -pr given; skipping the comment")
	case existing != nil:
		if err := client.UpdateComment(existing.ID, body); err != nil {
			return err
		}
	default:
		if err := client.CreateComment(gh.PR, body); err != nil {
			return err
		}
	}

	if n := blockedCount(reports, gh.Labels); n > 0 && *failOnBlock {
		return errBlocked{n}
	}
	return nil
}

func overridden(r *report.Report, labels []string) bool {
	if r.OverrideLabel == "" {
		return false
	}
	for _, l := range labels {
		if l == r.OverrideLabel {
			return true
		}
	}
	return false
}

func blockedCount(reports []*report.Report, labels []string) int {
	n := 0
	for _, r := range reports {
		if !overridden(r, labels) {
			n += len(r.Blocked())
		}
	}
	return n
}

func setOutputs(reports []*report.Report, labels []string) error {
	total := report.Counts{}
	for _, r := range reports {
		for a, n := range r.Counts() {
			total[a] += n
		}
	}
	outputs := map[string]string{
		"has-changes": fmt.Sprint(total.Total() > 0),
		"destructive": fmt.Sprint(total[report.Delete]+total[report.Replace] > 0),
		"blocked":     fmt.Sprint(blockedCount(reports, labels) > 0),
		"to-add":      fmt.Sprint(total[report.Create]),
		"to-change":   fmt.Sprint(total[report.Update]),
		"to-replace":  fmt.Sprint(total[report.Replace]),
		"to-destroy":  fmt.Sprint(total[report.Delete]),
	}
	for k, v := range outputs {
		if err := github.SetOutput(k, v); err != nil {
			return fmt.Errorf("setting output %s: %w", k, err)
		}
	}
	return nil
}
