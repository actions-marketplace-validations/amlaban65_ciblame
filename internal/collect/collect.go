// Package collect gathers workflow runs, their jobs and pull requests for a
// set of repositories and turns each run into an attributed, priced Record.
package collect

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amlaban65/ciblame/internal/cache"
	"github.com/amlaban65/ciblame/internal/classify"
	"github.com/amlaban65/ciblame/internal/gh"
	"github.com/amlaban65/ciblame/internal/pricing"
)

// Source is where runs, jobs and pull requests come from. *gh.Client
// implements it; tests and --demo use in-memory sources.
type Source interface {
	GetRepo(ctx context.Context, owner, name string) (*gh.Repo, error)
	ListRuns(ctx context.Context, owner, name string, from, to time.Time, fn func([]gh.Run) error) error
	ListJobs(ctx context.Context, owner, name string, runID int64) ([]gh.Job, error)
	ListPulls(ctx context.Context, owner, name string, since time.Time, fn func([]gh.PullRequest) error) error
	GetPull(ctx context.Context, owner, name string, number int) (*gh.PullRequest, error)
}

// Options control collection.
type Options struct {
	Since, Until time.Time
	// Workers is the number of concurrent job fetches.
	Workers int
	// Fast skips per-run job fetches and estimates minutes from run wall-clock time.
	Fast       bool
	Pricing    pricing.Table
	Classifier *classify.Classifier
	Cache      *cache.Cache
	// Progress, if set, is called as work advances.
	Progress func(Progress)
	// Logf receives warnings.
	Logf func(format string, args ...any)
}

// Progress reports collection progress for one repository.
type Progress struct {
	Repo  string
	Stage string // "runs", "pulls", "jobs"
	Done  int
	Total int
}

// PRRef summarises the pull request a run belongs to.
type PRRef struct {
	Number int    `json:"number"`
	State  string `json:"state"` // "open", "merged" or "closed" (closed without merging)
	Author string `json:"author"`
	URL    string `json:"url,omitempty"`
}

// PR states.
const (
	PROpen   = "open"
	PRMerged = "merged"
	PRClosed = "closed"
)

// Record is one attributed, priced workflow run (all attempts).
type Record struct {
	Repo         string          `json:"repo"`
	RunID        int64           `json:"run_id"`
	URL          string          `json:"url"`
	Workflow     string          `json:"workflow"`
	WorkflowPath string          `json:"workflow_path"`
	Event        string          `json:"event"`
	Branch       string          `json:"branch"`
	Attempts     int             `json:"attempts"`
	Conclusion   string          `json:"conclusion"`
	CreatedAt    time.Time       `json:"created_at"`
	Attribution  classify.Result `json:"attributed_to"`
	PR           *PRRef          `json:"pull_request,omitempty"`
	Jobs         int             `json:"jobs"`
	Minutes      int             `json:"billable_minutes"`
	// SelfHostedMinutes is the part of Minutes that ran on self-hosted runners.
	SelfHostedMinutes int     `json:"self_hosted_minutes,omitempty"`
	Cost              float64 `json:"estimated_cost_usd"`
	// Approx is set when minutes were estimated from wall-clock time (--fast).
	Approx bool `json:"approximate,omitempty"`
	// Unpriced maps runner labels with no known rate to their minutes.
	Unpriced map[string]int `json:"unpriced_runner_minutes,omitempty"`
}

// Failed reports whether the run's final conclusion was a failure.
func (r *Record) Failed() bool {
	switch r.Conclusion {
	case "failure", "timed_out", "startup_failure":
		return true
	}
	return false
}

// RepoInfo describes a collected repository.
type RepoInfo struct {
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
}

// Result is the output of a collection.
type Result struct {
	Records    []Record   `json:"records"`
	Repos      []RepoInfo `json:"repos"`
	InProgress int        `json:"in_progress_runs_skipped"`
}

// Run collects every repository ("owner/name") in repos.
func Run(ctx context.Context, src Source, repos []string, opts Options) (*Result, error) {
	if opts.Workers <= 0 {
		opts.Workers = 8
	}
	if opts.Classifier == nil {
		opts.Classifier = classify.Default()
	}
	if opts.Pricing.Overrides == nil {
		opts.Pricing.Overrides = map[string]float64{}
	}
	res := &Result{}
	for _, full := range repos {
		owner, name, ok := strings.Cut(full, "/")
		if !ok {
			return nil, fmt.Errorf("invalid repository %q: want owner/name", full)
		}
		recs, info, inProgress, err := collectRepo(ctx, src, owner, name, opts)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", full, err)
		}
		res.Records = append(res.Records, recs...)
		res.Repos = append(res.Repos, info)
		res.InProgress += inProgress
	}
	sort.SliceStable(res.Records, func(i, j int) bool {
		return res.Records[i].CreatedAt.Before(res.Records[j].CreatedAt)
	})
	return res, nil
}

func (o *Options) progress(p Progress) {
	if o.Progress != nil {
		o.Progress(p)
	}
}

func (o *Options) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

func collectRepo(ctx context.Context, src Source, owner, name string, opts Options) ([]Record, RepoInfo, int, error) {
	full := owner + "/" + name
	info := RepoInfo{FullName: full}

	repo, err := src.GetRepo(ctx, owner, name)
	if err != nil {
		return nil, info, 0, err
	}
	info.Private = repo.Private
	if repo.FullName != "" {
		info.FullName = repo.FullName
	}

	// 1. Runs.
	seen := map[int64]bool{}
	var runs []gh.Run
	inProgress := 0
	err = src.ListRuns(ctx, owner, name, opts.Since, opts.Until, func(page []gh.Run) error {
		for _, r := range page {
			if seen[r.ID] {
				continue
			}
			seen[r.ID] = true
			if r.Status != "completed" {
				inProgress++
				continue
			}
			runs = append(runs, r)
		}
		opts.progress(Progress{Repo: full, Stage: "runs", Done: len(runs)})
		return nil
	})
	if err != nil {
		return nil, info, 0, fmt.Errorf("listing workflow runs: %w", err)
	}

	// 2. Pull requests.
	idx := newPRIndex()
	err = src.ListPulls(ctx, owner, name, opts.Since, func(page []gh.PullRequest) error {
		for i := range page {
			idx.add(&page[i])
		}
		opts.progress(Progress{Repo: full, Stage: "pulls", Done: len(idx.byNum)})
		return nil
	})
	if err != nil {
		return nil, info, 0, fmt.Errorf("listing pull requests: %w", err)
	}
	missing := map[int]bool{}
	for _, r := range runs {
		for _, n := range prNumbers(r) {
			if idx.byNum[n] == nil {
				missing[n] = true
			}
		}
	}
	var missingList []int
	for n := range missing {
		missingList = append(missingList, n)
	}
	sort.Ints(missingList)
	var mu sync.Mutex
	err = forEach(ctx, len(missingList), opts.Workers, func(i int) error {
		n := missingList[i]
		key := fmt.Sprintf("pulls/%s/%s/%d", owner, name, n)
		var pr gh.PullRequest
		if !opts.Cache.Get(key, &pr) {
			p, err := src.GetPull(ctx, owner, name, n)
			if errors.Is(err, gh.ErrNotFound) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("fetching pull request #%d: %w", n, err)
			}
			pr = *p
			if pr.State == "closed" {
				_ = opts.Cache.Put(key, pr)
			}
		}
		mu.Lock()
		idx.add(&pr)
		mu.Unlock()
		return nil
	})
	if err != nil {
		return nil, info, 0, err
	}

	// 3. Jobs, priced and attributed.
	records := make([]Record, len(runs))
	var done int
	err = forEach(ctx, len(runs), opts.Workers, func(i int) error {
		run := runs[i]
		rec := baseRecord(full, run)
		pr := idx.resolve(run, full)
		if pr != nil {
			rec.PR = prRef(pr)
		}
		rec.Attribution = opts.Classifier.Classify(classifyInput(run, pr))

		if opts.Fast {
			priceFromWallClock(&rec, run, opts.Pricing)
		} else {
			jobs, err := fetchJobs(ctx, src, opts.Cache, owner, name, run)
			if err != nil {
				return fmt.Errorf("listing jobs for run %d: %w", run.ID, err)
			}
			priceJobs(&rec, jobs, opts.Pricing)
		}
		records[i] = rec

		mu.Lock()
		done++
		d := done
		mu.Unlock()
		opts.progress(Progress{Repo: full, Stage: "jobs", Done: d, Total: len(runs)})
		return nil
	})
	if err != nil {
		return nil, info, 0, err
	}
	return records, info, inProgress, nil
}

// Explain attributes and prices a single run and returns its jobs, for
// `ciblame why`. It looks up the run's pull request directly.
func Explain(ctx context.Context, src Source, owner, name string, run gh.Run, opts Options) (Record, []gh.Job, error) {
	if opts.Classifier == nil {
		opts.Classifier = classify.Default()
	}
	rec := baseRecord(owner+"/"+name, run)
	var pr *gh.PullRequest
	if nums := prNumbers(run); len(nums) > 0 {
		p, err := src.GetPull(ctx, owner, name, nums[0])
		switch {
		case err == nil:
			pr = p
		case !errors.Is(err, gh.ErrNotFound):
			return rec, nil, fmt.Errorf("fetching pull request #%d: %w", nums[0], err)
		}
	}
	if pr != nil {
		rec.PR = prRef(pr)
	}
	rec.Attribution = opts.Classifier.Classify(classifyInput(run, pr))
	jobs, err := src.ListJobs(ctx, owner, name, run.ID)
	if err != nil {
		return rec, nil, fmt.Errorf("listing jobs: %w", err)
	}
	priceJobs(&rec, jobs, opts.Pricing)
	return rec, jobs, nil
}

func baseRecord(repo string, run gh.Run) Record {
	attempts := run.RunAttempt
	if attempts < 1 {
		attempts = 1
	}
	name := run.Name
	if name == "" {
		name = run.Path
	}
	return Record{
		Repo:         repo,
		RunID:        run.ID,
		URL:          run.HTMLURL,
		Workflow:     name,
		WorkflowPath: run.Path,
		Event:        run.Event,
		Branch:       run.HeadBranch,
		Attempts:     attempts,
		Conclusion:   run.Conclusion,
		CreatedAt:    run.CreatedAt,
	}
}

func prRef(pr *gh.PullRequest) *PRRef {
	ref := &PRRef{Number: pr.Number, URL: pr.HTMLURL, State: PROpen}
	if pr.User != nil {
		ref.Author = pr.User.Login
	}
	switch {
	case pr.MergedAt != nil:
		ref.State = PRMerged
	case pr.State == "closed":
		ref.State = PRClosed
	}
	return ref
}

func classifyInput(run gh.Run, pr *gh.PullRequest) classify.Input {
	in := classify.Input{
		Event:        run.Event,
		WorkflowPath: run.Path,
		Branch:       run.HeadBranch,
	}
	if run.Actor != nil {
		in.Actor = classify.Identity{Login: run.Actor.Login, Type: run.Actor.Type}
	}
	if run.HeadCommit != nil {
		in.CommitMessage = run.HeadCommit.Message
		in.CommitAuthorEmail = run.HeadCommit.Author.Email
	}
	if pr != nil {
		if pr.User != nil {
			in.PRAuthor = &classify.Identity{Login: pr.User.Login, Type: pr.User.Type}
		}
		in.PRBody = pr.Body
		if pr.Head.Ref != "" {
			in.Branch = pr.Head.Ref
		}
	}
	return in
}

func fetchJobs(ctx context.Context, src Source, c *cache.Cache, owner, name string, run gh.Run) ([]gh.Job, error) {
	key := fmt.Sprintf("jobs/%s/%s/%d-%d", owner, name, run.ID, run.RunAttempt)
	var jobs []gh.Job
	if c.Get(key, &jobs) {
		return jobs, nil
	}
	jobs, err := src.ListJobs(ctx, owner, name, run.ID)
	if err != nil {
		return nil, err
	}
	_ = c.Put(key, jobs)
	return jobs, nil
}

func priceJobs(rec *Record, jobs []gh.Job, t pricing.Table) {
	for _, j := range jobs {
		rec.Jobs++
		m := pricing.BillableMinutes(j.StartedAt, j.CompletedAt)
		if m == 0 {
			continue
		}
		rec.Minutes += m
		if pricing.Identify(j.Labels).SelfHosted {
			rec.SelfHostedMinutes += m
		}
		rate, ok := t.Rate(j.Labels)
		if !ok {
			if rec.Unpriced == nil {
				rec.Unpriced = map[string]int{}
			}
			rec.Unpriced[pricing.PrimaryLabel(j.Labels)] += m
			continue
		}
		rec.Cost += rate * float64(m)
	}
	rec.Cost = roundCents(rec.Cost)
}

// roundCents rounds to 1/100 of a cent, enough precision for per-run costs
// without float noise in JSON output.
func roundCents(f float64) float64 {
	return math.Round(f*10000) / 10000
}

// priceFromWallClock estimates a run's minutes from its start and last
// update, assuming one standard Linux job. Used by --fast.
func priceFromWallClock(rec *Record, run gh.Run, t pricing.Table) {
	rec.Approx = true
	start := run.CreatedAt
	if run.RunStartedAt != nil && !run.RunStartedAt.IsZero() {
		start = *run.RunStartedAt
	}
	end := run.UpdatedAt
	rec.Minutes = pricing.BillableMinutes(&start, &end)
	rec.Cost = roundCents(float64(rec.Minutes) * t.Linux)
}

var (
	// mergeQueueBranch matches GitHub merge-queue branches, which name the
	// PR: gh-readonly-queue/main/pr-123-<sha>.
	mergeQueueBranch = regexp.MustCompile(`^gh-readonly-queue/.+/pr-(\d+)-`)
	// pullRef matches refs/pull/123/head and refs/pull/123/merge, used as the
	// branch of GitHub-run dynamic workflows such as Code Quality.
	pullRef = regexp.MustCompile(`^refs/pull/(\d+)/(?:head|merge)$`)
	// dynamicRunPR matches names like "Code scanning AI findings on PR #123".
	dynamicRunPR = regexp.MustCompile(`\bPR #(\d+)\b`)
)

// prNumbers returns the pull requests a run points at, most reliable first.
func prNumbers(run gh.Run) []int {
	var out []int
	for _, p := range run.PullRequests {
		if p.Number > 0 {
			out = append(out, p.Number)
		}
	}
	add := func(re *regexp.Regexp, s string) {
		if m := re.FindStringSubmatch(s); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
				out = append(out, n)
			}
		}
	}
	add(mergeQueueBranch, run.HeadBranch)
	add(pullRef, run.HeadBranch)
	if run.Event == "dynamic" {
		add(dynamicRunPR, run.Name)
	}
	return out
}

type prIndex struct {
	byNum  map[int]*gh.PullRequest
	byHead map[string][]*gh.PullRequest // "headRepo:branch" -> PRs
}

func newPRIndex() *prIndex {
	return &prIndex{byNum: map[int]*gh.PullRequest{}, byHead: map[string][]*gh.PullRequest{}}
}

func (ix *prIndex) add(pr *gh.PullRequest) {
	if pr == nil || pr.Number == 0 || ix.byNum[pr.Number] != nil {
		return
	}
	ix.byNum[pr.Number] = pr
	if pr.Head.Ref != "" && pr.Head.Repo != nil {
		k := strings.ToLower(pr.Head.Repo.FullName + ":" + pr.Head.Ref)
		ix.byHead[k] = append(ix.byHead[k], pr)
	}
}

// resolve finds the pull request a run belongs to: from the run's own PR
// list, from a merge-queue branch name, or (for fork PRs, which GitHub omits
// from the run) by head repository and branch at the time of the run.
func (ix *prIndex) resolve(run gh.Run, repo string) *gh.PullRequest {
	for _, n := range prNumbers(run) {
		if pr := ix.byNum[n]; pr != nil {
			return pr
		}
	}
	switch run.Event {
	case "pull_request", "pull_request_target", "dynamic":
	default:
		return nil
	}
	headRepo := repo
	if run.HeadRepository != nil && run.HeadRepository.FullName != "" {
		headRepo = run.HeadRepository.FullName
	}
	var best *gh.PullRequest
	for _, pr := range ix.byHead[strings.ToLower(headRepo+":"+run.HeadBranch)] {
		if pr.CreatedAt.After(run.CreatedAt.Add(time.Minute)) {
			continue
		}
		if best == nil || pr.CreatedAt.After(best.CreatedAt) {
			best = pr
		}
	}
	return best
}

// forEach runs fn(0..n-1) on up to workers goroutines and returns the first error.
func forEach(ctx context.Context, n, workers int, fn func(i int) error) error {
	if n == 0 {
		return nil
	}
	if workers > n {
		workers = n
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	idx := make(chan int)
	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range idx {
				if err := fn(i); err != nil {
					once.Do(func() { firstErr = err; cancel() })
				}
			}
		}()
	}
feed:
	for i := 0; i < n; i++ {
		select {
		case idx <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(idx)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}
