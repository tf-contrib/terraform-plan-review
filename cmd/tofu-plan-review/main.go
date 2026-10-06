// Command tofu-plan-review renders OpenTofu plans for pull request review.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/urfave/cli/v3"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = ""

// exitBlocked is the exit status when a blocking policy rule matched.
const exitBlocked = 2

func main() {
	err := NewApp().Run(context.Background(), os.Args)
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

// NewApp creates the root command.
func NewApp() *cli.Command {
	return &cli.Command{
		Name:      "tofu-plan-review",
		Usage:     "Readable OpenTofu plan reviews on pull requests",
		UsageText: "tofu-plan-review [global options] command [options] INPUT...",
		Description: `INPUT is a binary plan file (converted with "tofu show -json"), the JSON
output of "tofu show -json", or a report written by "analyze". To review
several roots together, run "analyze" once per root and pass all reports
to "comment".

Every option can also be set with the TOFU_PLAN_REVIEW_<OPTION> environment
variable, e.g. TOFU_PLAN_REVIEW_DIR for --dir.`,
		Version:         buildVersion(),
		HideHelpCommand: true,
		ErrWriter:       os.Stderr,
		Commands: []*cli.Command{
			NewAnalyze(),
			NewRender(),
			NewComment(),
		},
	}
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

type errBlocked struct{ n int }

func (e errBlocked) Error() string {
	return fmt.Sprintf("%d change(s) blocked by policy", e.n)
}

// env returns the environment variable source for an option.
func env(name string) cli.ValueSourceChain {
	return cli.EnvVars("TOFU_PLAN_REVIEW_" + name)
}
