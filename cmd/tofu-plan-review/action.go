package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/urfave/cli/v3"
)

// NewAction creates the entrypoint of the Docker action. The image has no
// shell, so this command does what action.yml cannot: it splits the
// newline-separated plan input and dispatches on the mode. All other
// inputs arrive as TOFU_PLAN_REVIEW_* variables set in action.yml.
func NewAction() *cli.Command {
	return &cli.Command{
		Name:   "action",
		Usage:  "Run as the GitHub Action (reads its inputs from the environment)",
		Hidden: true,
		Flags: slices.Concat(inputFlags(), renderFlags(), commentFlags(), analyzeFlags(), []cli.Flag{
			&cli.StringFlag{
				Name:    "mode",
				Usage:   "comment or analyze",
				Sources: env("MODE"),
				Value:   "comment",
			},
			&cli.StringFlag{
				Name:    "plan",
				Usage:   "plan files or reports, one per line",
				Sources: env("PLAN"),
			},
		}),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			paths := planLines(cmd.String("plan"))
			if len(paths) == 0 {
				return fmt.Errorf("the plan input is empty")
			}
			if err := checkWorkspacePaths(paths); err != nil {
				return err
			}
			switch mode := cmd.String("mode"); mode {
			case "comment":
				return runComment(cmd, paths)
			case "analyze":
				return runAnalyze(cmd, paths)
			default:
				return fmt.Errorf("mode must be comment or analyze, got %q", mode)
			}
		},
	}
}

func planLines(s string) []string {
	var out []string
	for line := range strings.Lines(s) {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// checkWorkspacePaths explains the most likely mistake with a Docker
// action: only the workspace is mounted into the container, so files under
// runner.temp or other host paths do not exist there.
func checkWorkspacePaths(paths []string) error {
	ws := os.Getenv("GITHUB_WORKSPACE")
	if ws == "" {
		return nil
	}
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			continue
		}
		if rel, err := filepath.Rel(ws, p); err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("%s is outside the workspace (%s); the action runs in a container that can only read files in the workspace", p, ws)
		}
	}
	return nil
}
