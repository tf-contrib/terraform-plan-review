package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/urfave/cli/v3"

	"github.com/tofu-contrib/tofu-plan-review/internal/github"
	"github.com/tofu-contrib/tofu-plan-review/internal/render"
	"github.com/tofu-contrib/tofu-plan-review/internal/report"
)

// NewAnalyze creates the command that writes a redacted report for one root.
func NewAnalyze() *cli.Command {
	return &cli.Command{
		Name:      "analyze",
		Usage:     "Write a redacted report for one root, to combine roots with comment",
		ArgsUsage: "PLAN",
		Flags:     append(inputFlags(), analyzeFlags()...),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runAnalyze(cmd, cmd.Args().Slice())
		},
	}
}

func analyzeFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "out",
			Usage:   "write the report to this file (default: stdout)",
			Sources: env("OUT"),
		},
	}
}

func runAnalyze(cmd *cli.Command, paths []string) error {
	if len(paths) != 1 {
		return errors.New("analyze takes exactly one plan file")
	}
	r, err := newAnalyzer(cmd).analyze(paths[0])
	if err != nil {
		return err
	}
	if out := cmd.String("out"); out != "" {
		return r.Write(out)
	}
	return r.Encode(cmd.Root().Writer)
}

// NewRender creates the command that prints the review as Markdown.
func NewRender() *cli.Command {
	return &cli.Command{
		Name:      "render",
		Usage:     "Print the review as Markdown",
		ArgsUsage: "INPUT...",
		Flags: slices.Concat(inputFlags(), renderFlags(), []cli.Flag{
			&cli.StringFlag{
				Name:    "blob-url",
				Usage:   "base URL for source links, e.g. https://github.com/org/repo/blob/main",
				Sources: env("BLOB_URL"),
			},
			&cli.IntFlag{
				Name:    "limit",
				Usage:   "maximum output size in bytes",
				Sources: env("LIMIT"),
				Value:   render.CommentLimit,
			},
		}),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			reports, err := newAnalyzer(cmd).load(cmd.Args().Slice())
			if err != nil {
				return err
			}
			_, err = io.WriteString(cmd.Root().Writer, render.Markdown(reports, render.Options{
				ID:      cmd.String("id"),
				Title:   cmd.String("title"),
				BlobURL: cmd.String("blob-url"),
				Limit:   int(cmd.Int("limit")),
			}))
			return err
		},
	}
}

// NewComment creates the command that posts the review on a pull request.
func NewComment() *cli.Command {
	return &cli.Command{
		Name:  "comment",
		Usage: "Post or update the pull request comment, job summary, annotations and outputs",
		Description: `Reads the pull request, repository and token from the GitHub Actions
environment (GITHUB_EVENT_PATH, GITHUB_REPOSITORY, GITHUB_TOKEN).`,
		ArgsUsage: "INPUT...",
		Flags:     slices.Concat(inputFlags(), renderFlags(), commentFlags()),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runComment(cmd, cmd.Args().Slice())
		},
	}
}

func commentFlags() []cli.Flag {
	return []cli.Flag{
		&cli.IntFlag{
			Name:    "pr",
			Usage:   "pull request number; 0 reads it from the event payload",
			Sources: env("PR"),
		},
		&cli.BoolFlag{
			Name:    "dry-run",
			Usage:   "print the comment instead of posting it",
			Sources: env("DRY_RUN"),
		},
		&cli.BoolFlag{
			Name:    "annotate",
			Usage:   "annotate changed resources in the pull request diff; on by default, disable with --annotate=false",
			Sources: env("ANNOTATE"),
			Value:   true,
		},
		&cli.BoolFlag{
			Name:    "summary",
			Usage:   "write the full review to the job summary; on by default",
			Sources: env("SUMMARY"),
			Value:   true,
		},
		&cli.BoolFlag{
			Name:    "fail-on-block",
			Usage:   "exit with status 2 when a blocking rule matches; on by default",
			Sources: env("FAIL_ON_BLOCK"),
			Value:   true,
		},
	}
}

func runComment(cmd *cli.Command, paths []string) error {
	reports, err := newAnalyzer(cmd).load(paths)
	if err != nil {
		return err
	}
	gh, err := github.FromEnv()
	if err != nil {
		return err
	}
	if pr := cmd.Int("pr"); pr != 0 {
		gh.PR = int(pr)
	}
	opts := render.Options{
		ID:      cmd.String("id"),
		Title:   cmd.String("title"),
		BlobURL: gh.BlobURL(),
		Commit:  gh.Commit(),
		Labels:  gh.Labels,
	}
	stdout := cmd.Root().Writer

	// Find our previous comment first, so both the comment and the job
	// summary can say what changed since the last plan.
	var client *github.Client
	var existing *github.Comment
	post := !cmd.Bool("dry-run") && gh.PR != 0
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

	if cmd.Bool("summary") {
		so := opts
		so.Limit = render.SummaryLimit
		if err := github.AppendSummary(render.Markdown(reports, so)); err != nil {
			return fmt.Errorf("writing job summary: %w", err)
		}
		opts.DetailsURL = gh.RunURL()
	}
	if cmd.Bool("annotate") {
		for _, a := range annotations(reports, gh.Labels) {
			fmt.Fprintln(stdout, a.Command())
		}
	}
	if err := setOutputs(reports, gh.Labels); err != nil {
		return err
	}

	body := render.Markdown(reports, opts)
	switch {
	case cmd.Bool("dry-run"):
		fmt.Fprintln(stdout, body)
	case !post:
		fmt.Fprintln(os.Stderr, "tofu-plan-review: not a pull request event and no --pr given; skipping the comment")
	case existing != nil:
		if err := client.UpdateComment(existing.ID, body); err != nil {
			return err
		}
	default:
		if err := client.CreateComment(gh.PR, body); err != nil {
			return err
		}
	}

	if n := blockedCount(reports, gh.Labels); n > 0 && cmd.Bool("fail-on-block") {
		return errBlocked{n}
	}
	return nil
}

func overridden(r *report.Report, labels []string) bool {
	return r.OverrideLabel != "" && slices.Contains(labels, r.OverrideLabel)
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
