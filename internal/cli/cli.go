// Package cli implements the ciblame command line.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
)

// Version is set at build time with -ldflags "-X github.com/amlaban65/ciblame/internal/cli.Version=v1.2.3".
var Version = ""

const rootUsage = `ciblame — git blame for your CI bill.

Attributes GitHub Actions minutes and cost to the AI agents, bots and humans
that triggered them.

Usage:
  ciblame [flags] <owner/repo>...    report on one or more repositories
  ciblame --org <owner> [flags]      report on every active repository of an org or user
  ciblame --demo                     try it on a month of sample data, no token needed
  ciblame why <run-url>              explain how a single run was attributed
  ciblame agents                     list the agents and bots ciblame recognises
  ciblame version                    print the version

Examples:
  ciblame acme/web
  ciblame acme/web acme/api --since 7d --format markdown >> "$GITHUB_STEP_SUMMARY"
  ciblame --org acme --since 2026-10-01 --until 2026-11-01 --format json
  ciblame why https://github.com/acme/web/actions/runs/123456789

Authentication: GITHUB_TOKEN, GH_TOKEN, or the GitHub CLI's login (gh auth token).
The token needs read access to Actions and pull requests.

Run 'ciblame --help' for all report flags.
`

// Main runs the CLI and returns the process exit code.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	err := run(ctx, args, stdout, stderr)
	var ue *usageError
	if err != nil && !errors.Is(err, flag.ErrHelp) && !(errors.As(err, &ue) && ue.silent) {
		fmt.Fprintf(stderr, "ciblame: %v\n", err)
	}
	return exitCode(err)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "why", "explain":
			return runWhy(ctx, args[1:], stdout, stderr)
		case "agents", "signatures":
			return runAgents(args[1:], stdout, stderr)
		case "version", "--version", "-v":
			fmt.Fprintf(stdout, "ciblame %s\n", version())
			return nil
		case "help":
			fmt.Fprint(stdout, rootUsage)
			return nil
		case "report":
			args = args[1:]
		}
	}
	return runReport(ctx, args, stdout, stderr)
}

func version() string {
	if Version != "" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

func newFlagSet(name string, stderr io.Writer, usage string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fmt.Fprintln(stderr, "\nFlags:")
		fs.PrintDefaults()
	}
	return fs
}

// stdoutFile returns the *os.File behind w, if any, for terminal detection.
func stdoutFile(w io.Writer) *os.File {
	if f, ok := w.(*os.File); ok {
		return f
	}
	return nil
}

func oneOf(v string, allowed []string) bool {
	for _, a := range allowed {
		if strings.EqualFold(v, a) {
			return true
		}
	}
	return false
}
