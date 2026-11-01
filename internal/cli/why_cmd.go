package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/amlaban65/ciblame/internal/collect"
	"github.com/amlaban65/ciblame/internal/demo"
	"github.com/amlaban65/ciblame/internal/gh"
	"github.com/amlaban65/ciblame/internal/pricing"
)

const whyUsage = `Explain how ciblame attributes a single workflow run.

Usage:
  ciblame why <run-url>
  ciblame why --demo [run-id]

Example:
  ciblame why https://github.com/acme/web/actions/runs/123456789
`

type runGetter interface {
	collect.Source
	GetRun(ctx context.Context, owner, name string, id int64) (*gh.Run, error)
}

func runWhy(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := newFlagSet("ciblame why", stderr, whyUsage)
	useDemo := fs.Bool("demo", false, "explain a run from the built-in sample data")
	sigPath := fs.String("signatures", "", "JSON file with extra agent/bot signatures")
	token := fs.String("token", "", "GitHub token (prefer GITHUB_TOKEN)")
	apiURL := fs.String("api-url", gh.DefaultBaseURL, "GitHub API base URL")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	classifier, err := buildClassifier(*sigPath)
	if err != nil {
		return err
	}

	var (
		src         runGetter
		owner, name string
		id          int64
	)
	if *useDemo {
		now := time.Now().UTC()
		d := demo.New(now.Add(-30*24*time.Hour), now)
		src = d
		owner, name, _ = strings.Cut(demo.Repo, "/")
		if len(pos) > 0 {
			if id, err = strconv.ParseInt(pos[0], 10, 64); err != nil {
				if _, _, id, err = gh.ParseRunURL(pos[0]); err != nil {
					return usagef("%v", err)
				}
			}
		} else {
			id = pickDemoRun(d)
		}
	} else {
		if len(pos) != 1 {
			fs.Usage()
			return usagef("want exactly one run URL")
		}
		if owner, name, id, err = gh.ParseRunURL(pos[0]); err != nil {
			return usagef("%v", err)
		}
		tok, _ := resolveToken(*token)
		c := gh.NewClient(tok)
		c.BaseURL = strings.TrimRight(*apiURL, "/")
		c.UserAgent = "ciblame/" + version()
		src = c
	}

	run, err := src.GetRun(ctx, owner, name, id)
	if err != nil {
		return err
	}
	rec, jobs, err := collect.Explain(ctx, src, owner, name, *run, collect.Options{Pricing: pricing.Default(), Classifier: classifier})
	if err != nil {
		return err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Run %d · %s (%s) · %s · %s", rec.RunID, rec.Workflow, rec.WorkflowPath, rec.Event, orDash(rec.Conclusion))
	if rec.Attempts > 1 {
		fmt.Fprintf(&b, " · %d attempts", rec.Attempts)
	}
	fmt.Fprintf(&b, "\n  %s\n\n", rec.URL)
	a := rec.Attribution
	fmt.Fprintf(&b, "Attributed to  %s (%s", a.Name, a.Kind)
	if a.Role != "" {
		fmt.Fprintf(&b, ", %s", a.Role)
	}
	fmt.Fprintf(&b, ")\nEvidence       %s\n", a.Evidence)
	if a.Operator != "" {
		fmt.Fprintf(&b, "Operator       %s (the account the agent worked through)\n", a.Operator)
	}
	if rec.PR != nil {
		fmt.Fprintf(&b, "Pull request   #%d by %s (%s)\n", rec.PR.Number, rec.PR.Author, rec.PR.State)
	} else {
		b.WriteString("Pull request   none found\n")
	}
	if run.Actor != nil {
		fmt.Fprintf(&b, "Run actor      %s (%s)\n", run.Actor.Login, run.Actor.Type)
	}
	fmt.Fprintf(&b, "Branch         %s\n", orDash(rec.Branch))

	b.WriteString("\nJobs\n")
	sort.SliceStable(jobs, func(i, j int) bool { return jobs[i].RunAttempt < jobs[j].RunAttempt })
	t := pricing.Default()
	for _, j := range jobs {
		m := pricing.BillableMinutes(j.StartedAt, j.CompletedAt)
		cost := "unpriced"
		if rate, ok := t.Rate(j.Labels); ok {
			cost = fmt.Sprintf("$%.3f", rate*float64(m))
		}
		fmt.Fprintf(&b, "  #%d %-28s %-24s %4d min  %s\n", j.RunAttempt, truncate(j.Name, 28), truncate(strings.Join(j.Labels, ","), 24), m, cost)
	}
	fmt.Fprintf(&b, "  %-55s %4d min  $%.3f\n", fmt.Sprintf("total (%d jobs)", rec.Jobs), rec.Minutes, rec.Cost)
	b.WriteString("\nWrong attribution? Run 'ciblame agents' and see CONTRIBUTING.md to add or fix a signature.\n")
	_, err = io.WriteString(stdout, b.String())
	return err
}

// pickDemoRun chooses an agent-assisted run so the demo shows something interesting.
func pickDemoRun(d *demo.Source) int64 {
	runs := d.Runs()
	for _, r := range runs {
		if r.HeadCommit != nil && strings.Contains(r.HeadCommit.Message, "Claude") && r.Event == "pull_request" && r.RunAttempt > 1 {
			return r.ID
		}
	}
	for _, r := range runs {
		if r.Status == "completed" {
			return r.ID
		}
	}
	return 0
}

func orDash(s string) string {
	if s == "" {
		return "–"
	}
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
