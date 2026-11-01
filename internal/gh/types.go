// Package gh is a small, dependency-free client for the parts of the GitHub
// REST API that ciblame needs: workflow runs, jobs, pull requests and repos.
package gh

import "time"

// User is a GitHub account (a person, a bot, or an app).
type User struct {
	Login string `json:"login"`
	Type  string `json:"type"` // "User", "Bot", "Organization"
}

// Repo is the subset of repository fields ciblame uses.
type Repo struct {
	Name     string    `json:"name"`
	FullName string    `json:"full_name"`
	Owner    User      `json:"owner"`
	Private  bool      `json:"private"`
	Archived bool      `json:"archived"`
	Fork     bool      `json:"fork"`
	PushedAt time.Time `json:"pushed_at"`
}

// CommitIdentity is the author or committer of a commit.
type CommitIdentity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// HeadCommit is the commit a workflow run was triggered for.
type HeadCommit struct {
	ID        string         `json:"id"`
	Message   string         `json:"message"`
	Author    CommitIdentity `json:"author"`
	Committer CommitIdentity `json:"committer"`
}

// RunPullRequest is the minimal pull request reference embedded in a run.
// GitHub only populates it for pull requests opened from the same repository.
type RunPullRequest struct {
	Number int `json:"number"`
	Head   struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	} `json:"head"`
}

// RepoRef is a minimal repository reference.
type RepoRef struct {
	FullName string `json:"full_name"`
}

// Run is a GitHub Actions workflow run.
type Run struct {
	ID              int64            `json:"id"`
	Name            string           `json:"name"`
	Path            string           `json:"path"`
	Event           string           `json:"event"`
	Status          string           `json:"status"`
	Conclusion      string           `json:"conclusion"`
	HeadBranch      string           `json:"head_branch"`
	HeadSHA         string           `json:"head_sha"`
	RunNumber       int              `json:"run_number"`
	RunAttempt      int              `json:"run_attempt"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	RunStartedAt    *time.Time       `json:"run_started_at"`
	HTMLURL         string           `json:"html_url"`
	Actor           *User            `json:"actor"`
	TriggeringActor *User            `json:"triggering_actor"`
	PullRequests    []RunPullRequest `json:"pull_requests"`
	HeadCommit      *HeadCommit      `json:"head_commit"`
	HeadRepository  *RepoRef         `json:"head_repository"`
}

// Job is a single job within a workflow run attempt.
type Job struct {
	ID              int64      `json:"id"`
	RunID           int64      `json:"run_id"`
	RunAttempt      int        `json:"run_attempt"`
	Name            string     `json:"name"`
	Status          string     `json:"status"`
	Conclusion      string     `json:"conclusion"`
	StartedAt       *time.Time `json:"started_at"`
	CompletedAt     *time.Time `json:"completed_at"`
	Labels          []string   `json:"labels"`
	RunnerName      string     `json:"runner_name"`
	RunnerGroupName string     `json:"runner_group_name"`
}

// PullRequest is the subset of pull request fields ciblame uses.
type PullRequest struct {
	Number    int        `json:"number"`
	State     string     `json:"state"` // "open" or "closed"
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Draft     bool       `json:"draft"`
	HTMLURL   string     `json:"html_url"`
	User      *User      `json:"user"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ClosedAt  *time.Time `json:"closed_at"`
	MergedAt  *time.Time `json:"merged_at"`
	Head      struct {
		Ref  string   `json:"ref"`
		SHA  string   `json:"sha"`
		Repo *RepoRef `json:"repo"`
	} `json:"head"`
}

type runsPage struct {
	TotalCount   int   `json:"total_count"`
	WorkflowRuns []Run `json:"workflow_runs"`
}

type jobsPage struct {
	TotalCount int   `json:"total_count"`
	Jobs       []Job `json:"jobs"`
}
