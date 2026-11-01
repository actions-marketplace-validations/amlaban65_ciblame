package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/amlaban65/ciblame/internal/cache"
	"github.com/amlaban65/ciblame/internal/classify"
	"github.com/amlaban65/ciblame/internal/collect"
	"github.com/amlaban65/ciblame/internal/demo"
	"github.com/amlaban65/ciblame/internal/gh"
	"github.com/amlaban65/ciblame/internal/pricing"
	"github.com/amlaban65/ciblame/internal/report"
)

type reportFlags struct {
	org             string
	since, until    string
	by              string
	format          string
	output          string
	color           string
	records         bool
	perHuman        bool
	fast            bool
	workers         int
	top             int
	rates           stringList
	selfHostedRate  float64
	signatures      string
	noCache         bool
	includeInactive bool
	demo            bool
	quiet           bool
	token           string
	apiURL          string
}

func runReport(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	var f reportFlags
	fs := newFlagSet("ciblame", stderr, rootUsage)
	fs.StringVar(&f.org, "org", "", "report on every active repository of this org or user")
	fs.StringVar(&f.since, "since", "30d", "start of the window: 7d, 12h, 2w (counted back from --until), 2026-10-01 or RFC 3339")
	fs.StringVar(&f.until, "until", "", "end of the window: 2026-11-01, RFC 3339, or 3d ago (default now)")
	fs.StringVar(&f.by, "by", report.ByActor, "group rows by: "+strings.Join(report.Groupings, ", "))
	fs.StringVar(&f.format, "format", "table", "output format: "+strings.Join(report.Formats, ", "))
	fs.StringVar(&f.output, "output", "", "write the report to this file instead of stdout")
	fs.StringVar(&f.color, "color", "auto", "colorize table output: auto, always, never")
	fs.BoolVar(&f.records, "records", false, "include per-run records (json and csv formats)")
	fs.BoolVar(&f.perHuman, "per-human", false, "list each person separately instead of one 'humans' row")
	fs.BoolVar(&f.fast, "fast", false, "skip per-run job lookups and estimate minutes from run duration (fewer API calls, less accurate)")
	fs.IntVar(&f.workers, "workers", 8, "concurrent API requests")
	fs.IntVar(&f.top, "top", 15, "show at most this many rows, folding the rest into one (0 = all)")
	fs.Var(&f.rates, "rate", "per-minute USD rate for a runner label, e.g. --rate macos-15-xlarge=0.10 (repeatable)")
	fs.Float64Var(&f.selfHostedRate, "self-hosted-rate", 0, "per-minute USD cost to assign to self-hosted runners")
	fs.StringVar(&f.signatures, "signatures", "", "JSON file with extra agent/bot signatures (checked before built-ins)")
	fs.BoolVar(&f.noCache, "no-cache", false, "don't read or write the on-disk cache")
	fs.BoolVar(&f.includeInactive, "include-inactive", false, "with --org, include repositories not pushed to during the window")
	fs.BoolVar(&f.demo, "demo", false, "use a month of built-in sample data instead of the GitHub API")
	fs.BoolVar(&f.quiet, "quiet", false, "don't print progress")
	fs.StringVar(&f.token, "token", "", "GitHub token (prefer the GITHUB_TOKEN environment variable)")
	fs.StringVar(&f.apiURL, "api-url", gh.DefaultBaseURL, "GitHub API base URL (for GitHub Enterprise Server: https://HOST/api/v3)")

	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}

	// Validate flags.
	now := time.Now().UTC()
	until := now
	if f.until != "" {
		if until, err = parseWhen(f.until, now); err != nil {
			return usagef("%v", err)
		}
	}
	since, err := parseWhen(f.since, until)
	if err != nil {
		return usagef("%v", err)
	}
	if !since.Before(until) {
		return usagef("--since must be before --until")
	}
	if !oneOf(f.by, report.Groupings) {
		return usagef("--by must be one of %s", strings.Join(report.Groupings, ", "))
	}
	if !oneOf(f.format, append([]string{"md"}, report.Formats...)) {
		return usagef("--format must be one of %s", strings.Join(report.Formats, ", "))
	}
	if !oneOf(f.color, []string{"auto", "always", "never"}) {
		return usagef("--color must be auto, always or never")
	}
	if f.workers < 1 || f.workers > 32 {
		return usagef("--workers must be between 1 and 32")
	}
	if f.selfHostedRate < 0 {
		return usagef("--self-hosted-rate must not be negative")
	}
	table := pricing.Default()
	table.SelfHosted = f.selfHostedRate
	for _, r := range f.rates {
		label, rate, err := pricing.ParseOverride(r)
		if err != nil {
			return usagef("%v", err)
		}
		table.Overrides[label] = rate
	}
	classifier, err := buildClassifier(f.signatures)
	if err != nil {
		return err
	}

	var repos []string
	for _, p := range pos {
		r, err := normalizeRepo(p)
		if err != nil {
			return usagef("%v", err)
		}
		repos = append(repos, r)
	}

	progress := &progressPrinter{w: stderr, enabled: !f.quiet && isTerminal(os.Stderr) && stderr == os.Stderr}
	defer progress.clear()

	var src collect.Source
	var client *gh.Client
	var store *cache.Cache
	if f.demo {
		if len(repos) > 0 || f.org != "" {
			return usagef("--demo uses built-in data; don't pass repositories or --org")
		}
		src = demo.New(since, until)
		repos = []string{demo.Repo}
	} else {
		if len(repos) == 0 && f.org == "" {
			fs.Usage()
			return usagef("no repositories given (try 'ciblame --demo' to see sample output)")
		}
		token, source := resolveToken(f.token)
		if token == "" {
			progress.warnf("no GitHub token found; anonymous requests are limited to 60/hour. Set GITHUB_TOKEN or run 'gh auth login'.")
		} else if source == "--token" {
			progress.warnf("passing tokens on the command line can leak them into shell history; prefer GITHUB_TOKEN")
		}
		client = gh.NewClient(token)
		client.BaseURL = strings.TrimRight(f.apiURL, "/")
		client.UserAgent = "ciblame/" + version()
		client.Logf = progress.warnf
		src = client

		if f.org != "" {
			orgRepos, err := listOrgRepos(ctx, client, f.org, since, f.includeInactive)
			if err != nil {
				return err
			}
			if len(orgRepos) == 0 {
				progress.warnf("%s has no repositories pushed to since %s (use --include-inactive to include all)", f.org, since.Format("2006-01-02"))
			}
			repos = append(repos, orgRepos...)
		}
		if !f.noCache {
			if dir, err := cache.DefaultDir(); err == nil {
				if store, err = cache.Open(dir); err != nil {
					progress.warnf("cache disabled: %v", err)
					store = nil
				}
			}
		}
	}
	repos = dedupe(repos)

	res, err := collect.Run(ctx, src, repos, collect.Options{
		Since:      since,
		Until:      until,
		Workers:    f.workers,
		Fast:       f.fast,
		Pricing:    table,
		Classifier: classifier,
		Cache:      store,
		Progress:   progress.update,
		Logf:       progress.warnf,
	})
	progress.clear()
	if err != nil {
		return err
	}

	rep := report.Build(res, since, until, report.Options{
		By: strings.ToLower(f.by), PerHuman: f.perHuman, Top: f.top, Pricing: table,
	})
	if f.records {
		rep.Records = res.Records
	}

	out := stdout
	if f.output != "" {
		file, err := os.Create(f.output)
		if err != nil {
			return err
		}
		defer file.Close()
		out = file
	}
	color := false
	switch f.color {
	case "always":
		color = true
	case "auto":
		sf := stdoutFile(out)
		color = sf != nil && isTerminal(sf) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	}
	if err := report.Render(out, rep, strings.ToLower(f.format), report.RenderOptions{Color: color, Records: f.records}); err != nil {
		return err
	}
	if client != nil && !f.quiet && isTerminal(os.Stderr) && stderr == os.Stderr {
		fmt.Fprintf(stderr, "\n(%d GitHub API requests)\n", client.Requests())
	}
	return nil
}

func buildClassifier(extraPath string) (*classify.Classifier, error) {
	sigs, err := classify.Builtin()
	if err != nil {
		return nil, err
	}
	if extraPath != "" {
		extra, err := classify.LoadFile(extraPath)
		if err != nil {
			return nil, err
		}
		// Extra signatures take priority; built-ins with the same ID are replaced.
		ids := map[string]bool{}
		for _, s := range extra {
			ids[s.ID] = true
		}
		merged := append([]classify.Signature(nil), extra...)
		for _, s := range sigs {
			if !ids[s.ID] {
				merged = append(merged, s)
			}
		}
		sigs = merged
	}
	return classify.New(sigs), nil
}

func listOrgRepos(ctx context.Context, client *gh.Client, owner string, since time.Time, includeInactive bool) ([]string, error) {
	all, err := client.ListOwnerRepos(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("listing repositories of %s: %w", owner, err)
	}
	var out []string
	for _, r := range all {
		if r.Archived {
			continue
		}
		if !includeInactive && r.PushedAt.Before(since) {
			continue
		}
		out = append(out, r.FullName)
	}
	sort.Strings(out)
	return out, nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		k := strings.ToLower(s)
		if !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
}
