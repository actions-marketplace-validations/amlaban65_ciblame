// Package demo is an in-memory GitHub source with a realistic, deterministic
// month of activity on a fictional repository. It powers `ciblame --demo` and
// exercises the same classification and pricing pipeline as real data.
package demo

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"github.com/amlaban65/ciblame/internal/gh"
)

// Repo is the fictional repository the demo describes.
const Repo = "acme/storefront"

// Source implements collect.Source with generated data.
type Source struct {
	repo  gh.Repo
	runs  []gh.Run
	jobs  map[int64][]gh.Job
	pulls map[int]*gh.PullRequest
}

type persona struct {
	author      gh.User // PR author
	actor       gh.User // who pushes
	branch      func(slug string) string
	trailer     string // appended to commit messages
	email       string
	body        string
	count       int // PRs per 30 days
	iterMin     int
	iterMax     int
	gapMin      time.Duration // between pushes
	gapMax      time.Duration
	mergeProb   float64
	failProb    float64
	copilotSess bool // spawn Copilot coding-agent session runs
}

var (
	userCopilot    = gh.User{Login: "Copilot", Type: "Bot"}
	userDevin      = gh.User{Login: "devin-ai-integration[bot]", Type: "Bot"}
	userDependabot = gh.User{Login: "dependabot[bot]", Type: "Bot"}
	humans         = []gh.User{
		{Login: "maya-chen", Type: "User"}, {Login: "dan-okafor", Type: "User"}, {Login: "priya-s", Type: "User"},
		{Login: "leo-martins", Type: "User"}, {Login: "sam-wu", Type: "User"},
	}
)

// New generates activity between since and until.
func New(since, until time.Time) *Source {
	g := &gen{
		rng:     rand.New(rand.NewPCG(42, 2026)),
		since:   since,
		until:   until,
		nextRun: 9_100_000_000,
		nextJob: 51_000_000_000,
		nextPR:  4100,
		src: &Source{
			repo:  gh.Repo{Name: "storefront", FullName: Repo, Owner: gh.User{Login: "acme", Type: "Organization"}, Private: true},
			jobs:  map[int64][]gh.Job{},
			pulls: map[int]*gh.PullRequest{},
		},
	}
	scale := until.Sub(since).Hours() / 24 / 30
	if scale <= 0 {
		return g.src
	}

	personas := []persona{
		{ // Copilot coding agent: opens its own PRs and iterates against CI.
			author: userCopilot, actor: userCopilot,
			branch:  func(s string) string { return "copilot/" + s },
			trailer: "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>",
			count:   34, iterMin: 5, iterMax: 14, gapMin: 6 * time.Minute, gapMax: 35 * time.Minute,
			mergeProb: 0.55, failProb: 0.36, copilotSess: true,
		},
		{ // Claude Code, driven from a human's machine: only trailers and PR body give it away.
			author: humans[0], actor: humans[0],
			branch:  func(s string) string { return "feat/" + s },
			trailer: "Co-Authored-By: Claude <noreply@anthropic.com>",
			body:    "## Summary\n- Implements the change\n\n🤖 Generated with [Claude Code](https://claude.com/claude-code)",
			count:   22, iterMin: 3, iterMax: 8, gapMin: 8 * time.Minute, gapMax: 50 * time.Minute,
			mergeProb: 0.82, failProb: 0.22,
		},
		{ // OpenAI Codex cloud tasks, pushed as a human with codex/ branches.
			author: humans[1], actor: humans[1],
			branch: func(s string) string { return "codex/" + s },
			count:  14, iterMin: 2, iterMax: 6, gapMin: 10 * time.Minute, gapMax: 60 * time.Minute,
			mergeProb: 0.64, failProb: 0.27,
		},
		{ // Devin.
			author: userDevin, actor: userDevin,
			branch: func(s string) string { return "devin/" + s },
			count:  7, iterMin: 3, iterMax: 9, gapMin: 5 * time.Minute, gapMax: 25 * time.Minute,
			mergeProb: 0.43, failProb: 0.4,
		},
		{ // Dependabot.
			author: userDependabot, actor: userDependabot,
			branch: func(s string) string { return "dependabot/npm_and_yarn/" + s },
			count:  26, iterMin: 1, iterMax: 2, gapMin: 30 * time.Minute, gapMax: 4 * time.Hour,
			mergeProb: 0.77, failProb: 0.08,
		},
	}
	for _, p := range personas {
		n := max(1, int(float64(p.count)*scale+0.5))
		for i := 0; i < n; i++ {
			g.pullRequest(p)
		}
	}
	// Plain human PRs.
	n := max(1, int(40*scale+0.5))
	for i := 0; i < n; i++ {
		h := humans[g.rng.IntN(len(humans))]
		g.pullRequest(persona{
			author: h, actor: h, branch: func(s string) string { return h.Login + "/" + s },
			count: 1, iterMin: 1, iterMax: 4, gapMin: 20 * time.Minute, gapMax: 6 * time.Hour,
			mergeProb: 0.9, failProb: 0.1,
		})
	}
	// Nightly integration suite and Dependabot update jobs.
	for d := since.Truncate(24 * time.Hour).Add(26 * time.Hour); d.Before(until); d = d.Add(24 * time.Hour) {
		g.run(runSpec{
			name: "Nightly", path: ".github/workflows/nightly.yml", event: "schedule", branch: "main",
			actor: humans[2], at: d, failProb: 0.12,
			jobs: []jobSpec{{"integration", "ubuntu-latest", 22, 34}, {"macos-smoke", "macos-14", 7, 12}},
		})
		g.run(runSpec{
			name: "Dependabot Updates", path: "dynamic/dependabot/dependabot-updates", event: "dynamic",
			branch: "main", actor: userDependabot, at: d.Add(3 * time.Hour), failProb: 0.02,
			jobs: []jobSpec{{"Dependabot", "ubuntu-latest", 1, 3}},
		})
	}
	// A few runs still going when the report is taken.
	for i := 0; i < 3; i++ {
		g.src.runs = append(g.src.runs, gh.Run{
			ID: g.id(), Name: "CI", Path: ".github/workflows/ci.yml", Event: "pull_request",
			Status: "in_progress", CreatedAt: until.Add(-time.Duration(i+1) * 4 * time.Minute),
			Actor: &humans[i],
		})
	}
	sort.Slice(g.src.runs, func(i, j int) bool { return g.src.runs[i].CreatedAt.After(g.src.runs[j].CreatedAt) })
	return g.src
}

type gen struct {
	rng              *rand.Rand
	since, until     time.Time
	nextRun, nextJob int64
	nextPR           int
	src              *Source
}

type jobSpec struct {
	name, label      string
	minMins, maxMins int
}

type runSpec struct {
	name, path, event, branch string
	actor                     gh.User
	at                        time.Time
	pr                        int
	commitMsg, email          string
	failProb                  float64
	forceSuccess              bool
	jobs                      []jobSpec
}

func (g *gen) id() int64 { g.nextRun++; return g.nextRun }

func (g *gen) between(a, b time.Duration) time.Duration {
	if b <= a {
		return a
	}
	return a + time.Duration(g.rng.Int64N(int64(b-a)))
}

func (g *gen) intn(lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + g.rng.IntN(hi-lo+1)
}

var (
	verbs = []string{"fix", "add", "refactor", "update", "remove", "speed-up", "migrate", "handle", "cache", "validate"}
	nouns = []string{"cart-totals", "checkout-retry", "price-rounding", "search-index", "auth-session", "inventory-sync",
		"coupon-codes", "order-webhooks", "image-resizer", "tax-rules", "shipping-quotes", "rate-limiter", "feature-flags"}
)

func (g *gen) slug() string {
	return verbs[g.rng.IntN(len(verbs))] + "-" + nouns[g.rng.IntN(len(nouns))]
}

func ciJobs(withE2E bool) []jobSpec {
	jobs := []jobSpec{{"lint", "ubuntu-latest", 1, 3}, {"test", "ubuntu-latest", 7, 15}, {"build", "ubuntu-latest", 3, 6}}
	if withE2E {
		jobs = append(jobs, jobSpec{"e2e", "ubuntu-latest-8-cores", 5, 11})
	}
	return jobs
}

func (g *gen) pullRequest(p persona) {
	window := g.until.Sub(g.since)
	created := g.since.Add(time.Duration(float64(window) * (0.02 + 0.9*g.rng.Float64())))
	g.nextPR++
	slug := g.slug()
	pr := &gh.PullRequest{
		Number:    g.nextPR,
		State:     "open",
		Title:     strings.ReplaceAll(slug, "-", " "),
		Body:      p.body,
		HTMLURL:   fmt.Sprintf("https://github.com/%s/pull/%d", Repo, g.nextPR),
		User:      &gh.User{Login: p.author.Login, Type: p.author.Type},
		CreatedAt: created,
	}
	pr.Head.Ref = p.branch(slug)
	pr.Head.Repo = &gh.RepoRef{FullName: Repo}

	msg := func(i int) string {
		m := fmt.Sprintf("%s (step %d)", pr.Title, i+1)
		if p.trailer != "" {
			m += "\n\n" + p.trailer
		}
		return m
	}
	email := p.email
	if email == "" {
		email = p.actor.Login + "@users.noreply.github.com"
	}

	reviewer := humans[2]
	if p.author.Type == "User" {
		reviewer = p.author
	}
	if g.rng.Float64() < 0.6 {
		g.run(runSpec{
			name: "Copilot code review", path: "dynamic/agents/copilot-pull-request-reviewer", event: "dynamic",
			branch: pr.Head.Ref, actor: reviewer, at: created.Add(40 * time.Second), pr: pr.Number,
			jobs: []jobSpec{{"review", "ubuntu-latest", 2, 5}},
		})
	}

	iters := g.intn(p.iterMin, p.iterMax)
	at := created
	withE2E := g.rng.Float64() < 0.5
	for i := 0; i < iters; i++ {
		if p.copilotSess && (i == 0 || g.rng.Float64() < 0.2) {
			g.run(runSpec{
				name: "Copilot coding agent", path: "dynamic/copilot-swe-agent/copilot", event: "dynamic",
				branch: pr.Head.Ref, actor: userCopilot, at: at.Add(-2 * time.Minute), pr: pr.Number,
				jobs: []jobSpec{{"copilot", "ubuntu-latest", 9, 26}},
			})
		}
		g.run(runSpec{
			name: "CI", path: ".github/workflows/ci.yml", event: "pull_request", branch: pr.Head.Ref,
			actor: p.actor, at: at, pr: pr.Number, commitMsg: msg(i), email: email,
			failProb: p.failProb, forceSuccess: i == iters-1 && g.rng.Float64() < p.mergeProb,
			jobs: ciJobs(withE2E),
		})
		at = at.Add(g.between(p.gapMin, p.gapMax))
	}
	pr.UpdatedAt = at

	// Recent PRs may still be open.
	if at.After(g.until.Add(-window/8)) && g.rng.Float64() < 0.5 {
		g.src.pulls[pr.Number] = pr
		return
	}
	closeAt := at.Add(g.between(30*time.Minute, 30*time.Hour))
	if closeAt.After(g.until) {
		g.src.pulls[pr.Number] = pr
		return
	}
	pr.State = "closed"
	pr.ClosedAt = &closeAt
	pr.UpdatedAt = closeAt
	if g.rng.Float64() < p.mergeProb {
		pr.MergedAt = &closeAt
		// The squash-merge commit on main keeps co-author trailers.
		g.run(runSpec{
			name: "CI", path: ".github/workflows/ci.yml", event: "push", branch: "main",
			actor: humans[2], at: closeAt.Add(20 * time.Second), commitMsg: msg(iters), email: humans[2].Login + "@users.noreply.github.com",
			failProb: 0.04, jobs: ciJobs(false),
		})
	}
	g.src.pulls[pr.Number] = pr
}

func (g *gen) run(s runSpec) {
	if s.at.Before(g.since) || !s.at.Before(g.until) {
		return
	}
	actor := s.actor
	run := gh.Run{
		ID: g.id(), Name: s.name, Path: s.path, Event: s.event, Status: "completed", Conclusion: "success",
		HeadBranch: s.branch, HeadSHA: fmt.Sprintf("%040x", g.rng.Uint64()), RunAttempt: 1,
		CreatedAt: s.at, Actor: &actor, TriggeringActor: &actor,
		HeadRepository: &gh.RepoRef{FullName: Repo},
	}
	run.HTMLURL = fmt.Sprintf("https://github.com/%s/actions/runs/%d", Repo, run.ID)
	run.RunStartedAt = &run.CreatedAt
	if s.pr > 0 {
		run.PullRequests = []gh.RunPullRequest{{Number: s.pr}}
	}
	if s.commitMsg != "" {
		run.HeadCommit = &gh.HeadCommit{Message: s.commitMsg, Author: gh.CommitIdentity{Name: actor.Login, Email: s.email}}
	}

	failed := !s.forceSuccess && g.rng.Float64() < s.failProb
	attempts := 1
	if failed && g.rng.Float64() < 0.35 {
		attempts = 2 // someone hit "re-run failed jobs"
	}
	end := s.at
	start := s.at
	for a := 1; a <= attempts; a++ {
		var latest time.Time
		for ji, js := range s.jobs {
			queued := start.Add(g.between(8*time.Second, 50*time.Second))
			dur := time.Duration(g.intn(js.minMins*60, js.maxMins*60)) * time.Second
			concl := "success"
			if failed && ji == 1 {
				concl = "failure"
			}
			if failed && ji > 1 && js.name == "build" {
				concl, dur = "skipped", 0
			}
			done := queued.Add(dur)
			g.nextJob++
			st, ct := queued, done
			g.src.jobs[run.ID] = append(g.src.jobs[run.ID], gh.Job{
				ID: g.nextJob, RunID: run.ID, RunAttempt: a, Name: js.name, Status: "completed", Conclusion: concl,
				StartedAt: &st, CompletedAt: &ct, Labels: []string{js.label},
				RunnerName: "GitHub Actions " + fmt.Sprint(g.rng.IntN(900)), RunnerGroupName: "GitHub Actions",
			})
			if done.After(latest) {
				latest = done
			}
		}
		end = latest
		start = latest.Add(g.between(5*time.Minute, 90*time.Minute))
	}
	if failed && attempts == 2 && g.rng.Float64() < 0.5 {
		failed = false // the re-run went green
		for i := range g.src.jobs[run.ID] {
			j := &g.src.jobs[run.ID][i]
			if j.RunAttempt == 2 && j.Conclusion == "failure" {
				j.Conclusion = "success"
			}
		}
	}
	if failed {
		run.Conclusion = "failure"
	}
	run.RunAttempt = attempts
	run.UpdatedAt = end
	g.src.runs = append(g.src.runs, run)
}

// GetRun returns a generated run by ID.
func (s *Source) GetRun(_ context.Context, _, _ string, id int64) (*gh.Run, error) {
	for i := range s.runs {
		if s.runs[i].ID == id {
			r := s.runs[i]
			return &r, nil
		}
	}
	return nil, fmt.Errorf("%w: run %d is not in the demo data", gh.ErrNotFound, id)
}

// Runs returns all generated runs, newest first.
func (s *Source) Runs() []gh.Run { return s.runs }

// GetRepo implements collect.Source.
func (s *Source) GetRepo(_ context.Context, owner, name string) (*gh.Repo, error) {
	if owner+"/"+name != Repo {
		return nil, fmt.Errorf("%w: demo data only covers %s", gh.ErrNotFound, Repo)
	}
	r := s.repo
	return &r, nil
}

// ListRuns implements collect.Source.
func (s *Source) ListRuns(_ context.Context, _, _ string, from, to time.Time, fn func([]gh.Run) error) error {
	var page []gh.Run
	for _, r := range s.runs {
		if r.CreatedAt.Before(from) || r.CreatedAt.After(to) {
			continue
		}
		page = append(page, r)
		if len(page) == 100 {
			if err := fn(page); err != nil {
				return err
			}
			page = nil
		}
	}
	if len(page) > 0 {
		return fn(page)
	}
	return nil
}

// ListJobs implements collect.Source.
func (s *Source) ListJobs(_ context.Context, _, _ string, runID int64) ([]gh.Job, error) {
	return s.jobs[runID], nil
}

// ListPulls implements collect.Source.
func (s *Source) ListPulls(_ context.Context, _, _ string, since time.Time, fn func([]gh.PullRequest) error) error {
	var out []gh.PullRequest
	for _, pr := range s.pulls {
		if !pr.UpdatedAt.Before(since) {
			out = append(out, *pr)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	if len(out) == 0 {
		return nil
	}
	return fn(out)
}

// GetPull implements collect.Source.
func (s *Source) GetPull(_ context.Context, _, _ string, number int) (*gh.PullRequest, error) {
	pr, ok := s.pulls[number]
	if !ok {
		return nil, gh.ErrNotFound
	}
	c := *pr
	return &c, nil
}
