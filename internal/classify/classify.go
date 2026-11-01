// Package classify attributes a workflow run to the AI agent, bot or human
// responsible for it, and records the evidence for that decision.
package classify

import (
	"fmt"
	"regexp"
	"strings"
)

// Kind is the broad category a run is attributed to.
type Kind string

const (
	KindAgent Kind = "agent"
	KindBot   Kind = "bot"
	KindHuman Kind = "human"
)

// RoleRuntime marks runs that are the agent itself executing on Actions
// (for example a Copilot coding-agent session or a Copilot code review),
// as opposed to CI triggered by the agent's commits.
const RoleRuntime = "runtime"

// Identity is a GitHub account.
type Identity struct {
	Login string
	Type  string // "User", "Bot", ...
}

func (i Identity) isBot() bool {
	return strings.EqualFold(i.Type, "Bot") || strings.HasSuffix(strings.ToLower(i.Login), "[bot]")
}

// Input is everything the classifier looks at for one workflow run.
type Input struct {
	Event        string
	WorkflowPath string
	Actor        Identity
	// PRAuthor is the author of the pull request the run belongs to, if any.
	PRAuthor *Identity
	PRBody   string
	// Branch is the pull request head branch, or the run's branch.
	Branch            string
	CommitMessage     string
	CommitAuthorEmail string
}

// Result is the attribution for one run.
type Result struct {
	// ID is stable: a signature ID, "human:<login>", "bot:<login>" or "schedule".
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
	// Role is "runtime" when the run is the agent itself executing.
	Role string `json:"role,omitempty"`
	// Evidence explains which signal produced the attribution.
	Evidence string `json:"evidence"`
	// Operator is the human account an agent worked through, when the agent
	// was detected from commit or PR content rather than its own identity.
	Operator string `json:"operator,omitempty"`
}

// Classifier attributes runs using an ordered list of signatures.
// Earlier signatures win when several match at the same signal level.
type Classifier struct {
	sigs []Signature
}

// New returns a classifier over sigs.
func New(sigs []Signature) *Classifier {
	return &Classifier{sigs: sigs}
}

// Default returns a classifier over the built-in signatures.
func Default() *Classifier {
	sigs, err := Builtin()
	if err != nil {
		panic("ciblame: invalid built-in signatures: " + err.Error())
	}
	return New(sigs)
}

// Signatures returns the classifier's signatures in priority order.
func (c *Classifier) Signatures() []Signature { return c.sigs }

var coAuthorEmail = regexp.MustCompile(`(?im)^co-authored-by:[^<\n]*<([^>\n]+)>`)

// Classify attributes a run. Signals are checked from strongest to weakest:
//
//  1. workflow path of a known agent runtime (e.g. Copilot code review),
//     including any GitHub-hosted agent under dynamic/agents/
//  2. scheduled runs (attributed to "scheduled workflows", not to whoever last edited the cron)
//  3. an agent's own account as pull request author or run actor
//  4. pull request body markers
//  5. commit trailers and author e-mail
//  6. branch prefix
//  7. a known bot account (Dependabot, Renovate, github-actions, ...)
//  8. any other bot account
//  9. the human who owns the change (pull request author, else run actor)
//
// Steps 4–6 run before known bots so that agents working through a generic
// automation account (e.g. agentic workflows opening PRs as github-actions[bot])
// or through a person's account are still credited to the agent.
func (c *Classifier) Classify(in Input) Result {
	owner := in.Actor
	if in.PRAuthor != nil && in.PRAuthor.Login != "" {
		owner = *in.PRAuthor
	}
	operator := ""
	if owner.Login != "" && !owner.isBot() {
		operator = owner.Login
	}

	// 1. Agent runtimes identified by workflow path.
	if in.WorkflowPath != "" {
		p := strings.ToLower(in.WorkflowPath)
		for i := range c.sigs {
			s := &c.sigs[i]
			for _, wp := range s.WorkflowPaths {
				if wp != "" && strings.Contains(p, strings.ToLower(wp)) {
					return s.result(RoleRuntime, "workflow path "+in.WorkflowPath, "")
				}
			}
			for _, suf := range s.WorkflowPathSuffixes {
				if suf != "" && strings.HasSuffix(p, strings.ToLower(suf)) {
					return s.result(RoleRuntime, "workflow path "+in.WorkflowPath, "")
				}
			}
		}
		if rest, ok := strings.CutPrefix(p, "dynamic/agents/"); ok {
			name, _, _ := strings.Cut(rest, "/")
			if name != "" {
				return Result{ID: "github-agent:" + name, Name: "GitHub agent: " + name, Kind: KindAgent,
					Role: RoleRuntime, Evidence: "workflow path " + in.WorkflowPath}
			}
		}
	}

	// 2. Scheduled runs.
	if in.Event == "schedule" {
		return Result{ID: "schedule", Name: "scheduled workflows", Kind: KindBot, Evidence: "event=schedule"}
	}

	// 3. An agent's own account.
	type who struct {
		label string
		id    Identity
	}
	accounts := []who{{"run actor", in.Actor}}
	if in.PRAuthor != nil {
		accounts = append([]who{{"PR author", *in.PRAuthor}}, accounts...)
	}
	matchLogin := func(kind Kind) (Result, bool) {
		for _, a := range accounts {
			if a.id.Login == "" {
				continue
			}
			for i := range c.sigs {
				if c.sigs[i].Kind == kind && c.sigs[i].hasLogin(a.id.Login) {
					return c.sigs[i].result("", fmt.Sprintf("%s is %s", a.label, a.id.Login), ""), true
				}
			}
		}
		return Result{}, false
	}
	if r, ok := matchLogin(KindAgent); ok {
		return r
	}

	// 4. Pull request body markers.
	if in.PRBody != "" {
		for i := range c.sigs {
			s := &c.sigs[i]
			for _, re := range s.bodies {
				if m := re.FindString(in.PRBody); m != "" {
					return s.result("", fmt.Sprintf("PR body contains %q", m), operator)
				}
			}
		}
	}

	// 5. Commit trailers and e-mails.
	if in.CommitMessage != "" || in.CommitAuthorEmail != "" {
		coEmails := coAuthorEmail.FindAllStringSubmatch(in.CommitMessage, -1)
		for i := range c.sigs {
			s := &c.sigs[i]
			for _, re := range s.trailers {
				if m := re.FindString(in.CommitMessage); m != "" {
					return s.result("", fmt.Sprintf("commit trailer %q", strings.TrimSpace(m)), operator)
				}
			}
			for _, e := range s.Emails {
				if strings.EqualFold(e, in.CommitAuthorEmail) {
					return s.result("", "commit author "+in.CommitAuthorEmail, operator)
				}
				for _, m := range coEmails {
					if strings.EqualFold(e, strings.TrimSpace(m[1])) {
						return s.result("", "co-author "+m[1], operator)
					}
				}
			}
		}
	}

	// 6. Branch prefixes.
	if in.Branch != "" {
		b := strings.ToLower(in.Branch)
		for i := range c.sigs {
			s := &c.sigs[i]
			for _, pre := range s.BranchPrefixes {
				if pre != "" && strings.HasPrefix(b, strings.ToLower(pre)) {
					return s.result("", "branch "+in.Branch, operator)
				}
			}
		}
	}

	// 7. Known bots.
	if r, ok := matchLogin(KindBot); ok {
		return r
	}

	// 8. Other bots.
	for _, a := range accounts {
		if a.id.Login != "" && a.id.isBot() {
			login := strings.ToLower(a.id.Login)
			return Result{
				ID:       "bot:" + login,
				Name:     strings.TrimSuffix(a.id.Login, "[bot]"),
				Kind:     KindBot,
				Evidence: fmt.Sprintf("%s %s is a bot account", a.label, a.id.Login),
			}
		}
	}
	if strings.HasPrefix(strings.ToLower(in.WorkflowPath), "dynamic/") {
		return Result{ID: "dynamic", Name: "GitHub dynamic workflows", Kind: KindBot, Role: RoleRuntime,
			Evidence: "workflow path " + in.WorkflowPath}
	}

	// 9. Humans.
	if owner.Login == "" {
		return Result{ID: "unknown", Name: "unknown", Kind: KindHuman, Evidence: "no actor on run"}
	}
	label := "run actor"
	if in.PRAuthor != nil && in.PRAuthor.Login != "" {
		label = "PR author"
	}
	return Result{
		ID:       "human:" + strings.ToLower(owner.Login),
		Name:     owner.Login,
		Kind:     KindHuman,
		Evidence: fmt.Sprintf("%s %s (no agent signals)", label, owner.Login),
	}
}

func (s *Signature) result(role, evidence, operator string) Result {
	return Result{ID: s.ID, Name: s.Name, Kind: s.Kind, Role: role, Evidence: evidence, Operator: operator}
}
