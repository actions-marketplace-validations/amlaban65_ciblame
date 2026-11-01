package collect

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amlaban65/ciblame/internal/cache"
	"github.com/amlaban65/ciblame/internal/gh"
	"github.com/amlaban65/ciblame/internal/pricing"
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

type fakeSource struct {
	repo      gh.Repo
	pages     [][]gh.Run
	jobs      map[int64][]gh.Job
	pulls     []gh.PullRequest
	extra     map[int]gh.PullRequest // only reachable through GetPull
	jobCalls  atomic.Int32
	pullCalls atomic.Int32
	jobsErr   error
}

func (f *fakeSource) GetRepo(context.Context, string, string) (*gh.Repo, error) {
	r := f.repo
	return &r, nil
}
func (f *fakeSource) ListRuns(_ context.Context, _, _ string, _, _ time.Time, fn func([]gh.Run) error) error {
	for _, p := range f.pages {
		if err := fn(p); err != nil {
			return err
		}
	}
	return nil
}
func (f *fakeSource) ListJobs(_ context.Context, _, _ string, id int64) ([]gh.Job, error) {
	f.jobCalls.Add(1)
	if f.jobsErr != nil {
		return nil, f.jobsErr
	}
	return f.jobs[id], nil
}
func (f *fakeSource) ListPulls(_ context.Context, _, _ string, _ time.Time, fn func([]gh.PullRequest) error) error {
	return fn(f.pulls)
}
func (f *fakeSource) GetPull(_ context.Context, _, _ string, n int) (*gh.PullRequest, error) {
	f.pullCalls.Add(1)
	if pr, ok := f.extra[n]; ok {
		return &pr, nil
	}
	return nil, gh.ErrNotFound
}

func job(runID int64, attempt int, mins float64, labels ...string) gh.Job {
	s := t0
	e := t0.Add(time.Duration(mins * float64(time.Minute)))
	return gh.Job{RunID: runID, RunAttempt: attempt, StartedAt: &s, CompletedAt: &e, Labels: labels}
}

func pr(n int, login, typ, ref, headRepo string, merged bool) gh.PullRequest {
	p := gh.PullRequest{Number: n, User: &gh.User{Login: login, Type: typ}, CreatedAt: t0.Add(-time.Hour), UpdatedAt: t0, State: "closed"}
	if merged {
		m := t0
		p.MergedAt = &m
	}
	p.Head.Ref = ref
	p.Head.Repo = &gh.RepoRef{FullName: headRepo}
	return p
}

func fixture() *fakeSource {
	user := func(l string) *gh.User { return &gh.User{Login: l, Type: "User"} }
	bot := func(l string) *gh.User { return &gh.User{Login: l, Type: "Bot"} }
	runs := []gh.Run{
		// 1: Copilot PR, same repo, PR listed on the run. Re-run once.
		{ID: 1, Name: "CI", Path: ".github/workflows/ci.yml", Event: "pull_request", Status: "completed", Conclusion: "failure",
			HeadBranch: "copilot/fix", RunAttempt: 2, CreatedAt: t0, Actor: bot("Copilot"),
			PullRequests: []gh.RunPullRequest{{Number: 10}}},
		// 2: fork PR — GitHub leaves pull_requests empty.
		{ID: 2, Name: "CI", Path: ".github/workflows/ci.yml", Event: "pull_request", Status: "completed", Conclusion: "success",
			HeadBranch: "patch-1", RunAttempt: 1, CreatedAt: t0, Actor: user("outsider"),
			HeadRepository: &gh.RepoRef{FullName: "outsider/web"}},
		// 3: merge queue run for PR 10.
		{ID: 3, Name: "CI", Path: ".github/workflows/ci.yml", Event: "merge_group", Status: "completed", Conclusion: "success",
			HeadBranch: "gh-readonly-queue/main/pr-10-abc123", RunAttempt: 1, CreatedAt: t0, Actor: user("maintainer")},
		// 4: Code Quality dynamic run for PR 12, which isn't in the PR listing.
		{ID: 4, Name: "Code Quality: PR #12", Path: "dynamic/github-code-quality/codeql", Event: "dynamic", Status: "completed",
			Conclusion: "success", HeadBranch: "refs/pull/12/head", RunAttempt: 1, CreatedAt: t0, Actor: bot("github-actions[bot]")},
		// 5: still running — skipped.
		{ID: 5, Event: "push", Status: "in_progress", CreatedAt: t0, Actor: user("maintainer")},
		// 6: self-hosted plus an unknown label.
		{ID: 6, Name: "Deploy", Path: ".github/workflows/deploy.yml", Event: "push", Status: "completed", Conclusion: "success",
			HeadBranch: "main", RunAttempt: 1, CreatedAt: t0, Actor: user("maintainer"),
			HeadCommit: &gh.HeadCommit{Message: "deploy"}},
	}
	return &fakeSource{
		repo: gh.Repo{FullName: "acme/web", Private: true},
		// Run 1 appears on two pages (bisection boundary); it must be counted once.
		pages: [][]gh.Run{runs[:3], append([]gh.Run{runs[0]}, runs[3:]...)},
		jobs: map[int64][]gh.Job{
			1: {job(1, 1, 4.2, "ubuntu-latest"), job(1, 2, 3, "ubuntu-latest"), job(1, 2, 0, "ubuntu-latest")},
			2: {job(2, 1, 2, "ubuntu-latest")},
			3: {job(3, 1, 10, "ubuntu-latest-8-cores")},
			4: {job(4, 1, 1, "ubuntu-latest")},
			6: {job(6, 1, 5, "self-hosted", "linux"), job(6, 1, 2, "gpu-a100")},
		},
		pulls: []gh.PullRequest{
			pr(10, "Copilot", "Bot", "copilot/fix", "acme/web", true),
			pr(11, "outsider", "User", "patch-1", "outsider/web", false),
		},
		extra: map[int]gh.PullRequest{12: pr(12, "Copilot", "Bot", "copilot/other", "acme/web", false)},
	}
}

func byID(recs []Record) map[int64]Record {
	m := map[int64]Record{}
	for _, r := range recs {
		m[r.RunID] = r
	}
	return m
}

func TestCollect(t *testing.T) {
	src := fixture()
	res, err := Run(context.Background(), src, []string{"acme/web"}, Options{
		Since: t0.Add(-24 * time.Hour), Until: t0.Add(time.Hour), Pricing: pricing.Default(), Workers: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != 5 {
		t.Fatalf("want 5 records (dedupe + skip in-progress), got %d", len(res.Records))
	}
	if res.InProgress != 1 {
		t.Fatalf("InProgress = %d", res.InProgress)
	}
	if !res.Repos[0].Private {
		t.Fatal("repo privacy lost")
	}
	recs := byID(res.Records)

	r1 := recs[1]
	if r1.Minutes != 5+3 || r1.Attempts != 2 || r1.Jobs != 3 || !r1.Failed() {
		t.Errorf("run 1: %+v", r1)
	}
	if r1.Attribution.ID != "copilot" || r1.PR == nil || r1.PR.State != PRMerged {
		t.Errorf("run 1 attribution/pr: %+v %+v", r1.Attribution, r1.PR)
	}
	if want := 8 * pricing.LinuxRate; abs(r1.Cost-want) > 1e-9 {
		t.Errorf("run 1 cost %v, want %v", r1.Cost, want)
	}

	if r2 := recs[2]; r2.PR == nil || r2.PR.Number != 11 || r2.PR.State != PRClosed || r2.Attribution.ID != "human:outsider" {
		t.Errorf("fork PR not resolved: %+v / %+v", r2.PR, r2.Attribution)
	}
	if r3 := recs[3]; r3.PR == nil || r3.PR.Number != 10 || r3.Attribution.ID != "copilot" {
		t.Errorf("merge queue run not linked to PR 10: %+v / %+v", r3.PR, r3.Attribution)
	}
	if r3 := recs[3]; abs(r3.Cost-10*pricing.LinuxRate*4) > 1e-9 {
		t.Errorf("8-core runner cost %v", r3.Cost)
	}
	if r4 := recs[4]; r4.PR == nil || r4.PR.Number != 12 || r4.Attribution.ID != "copilot" {
		t.Errorf("dynamic run not linked to PR 12: %+v / %+v", r4.PR, r4.Attribution)
	}
	r6 := recs[6]
	if r6.SelfHostedMinutes != 5 || r6.Unpriced["gpu-a100"] != 2 || r6.Cost != 0 || r6.Minutes != 7 {
		t.Errorf("run 6: %+v", r6)
	}
	if src.pullCalls.Load() != 1 {
		t.Errorf("GetPull calls = %d, want 1 (only PR 12)", src.pullCalls.Load())
	}
}

func TestCollectUsesCache(t *testing.T) {
	c, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Since: t0.Add(-time.Hour), Until: t0.Add(time.Hour), Pricing: pricing.Default(), Cache: c}
	src := fixture()
	if _, err := Run(context.Background(), src, []string{"acme/web"}, opts); err != nil {
		t.Fatal(err)
	}
	first := src.jobCalls.Load()
	if first != 5 {
		t.Fatalf("first run fetched jobs %d times, want 5", first)
	}
	src2 := fixture()
	if _, err := Run(context.Background(), src2, []string{"acme/web"}, opts); err != nil {
		t.Fatal(err)
	}
	if n := src2.jobCalls.Load(); n != 0 {
		t.Fatalf("second run fetched jobs %d times, want 0 (cached)", n)
	}
	if n := src2.pullCalls.Load(); n != 0 {
		t.Fatalf("closed PR should be cached, got %d GetPull calls", n)
	}
}

func TestFastMode(t *testing.T) {
	src := fixture()
	for _, p := range src.pages {
		for i := range p {
			started := p[i].CreatedAt
			p[i].RunStartedAt = &started
			p[i].UpdatedAt = started.Add(90 * time.Second)
		}
	}
	res, err := Run(context.Background(), src, []string{"acme/web"}, Options{Pricing: pricing.Default(), Fast: true})
	if err != nil {
		t.Fatal(err)
	}
	if src.jobCalls.Load() != 0 {
		t.Fatal("fast mode should not fetch jobs")
	}
	for _, r := range res.Records {
		if !r.Approx || r.Minutes != 2 {
			t.Fatalf("fast record %+v", r)
		}
	}
}

func TestJobErrorsPropagate(t *testing.T) {
	src := fixture()
	src.jobsErr = errors.New("boom")
	_, err := Run(context.Background(), src, []string{"acme/web"}, Options{Pricing: pricing.Default()})
	if err == nil || !errors.Is(err, src.jobsErr) {
		t.Fatalf("want boom, got %v", err)
	}
}

func TestInvalidRepo(t *testing.T) {
	if _, err := Run(context.Background(), fixture(), []string{"nope"}, Options{}); err == nil {
		t.Fatal("expected error")
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
