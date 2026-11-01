package classify

import (
	"strings"
	"testing"
)

func user(login string) Identity { return Identity{Login: login, Type: "User"} }
func bot(login string) Identity  { return Identity{Login: login, Type: "Bot"} }
func ptr(i Identity) *Identity   { return &i }

func TestClassify(t *testing.T) {
	c := Default()
	tests := []struct {
		name         string
		in           Input
		wantID       string
		wantKind     Kind
		wantRole     string
		wantOperator string
	}{
		{
			name:   "Copilot code review runtime",
			in:     Input{Event: "dynamic", WorkflowPath: "dynamic/agents/copilot-pull-request-reviewer", Actor: user("alice")},
			wantID: "copilot-code-review", wantKind: KindAgent, wantRole: RoleRuntime,
		},
		{
			name:   "code scanning AI runtime (observed on github/gh-aw)",
			in:     Input{Event: "dynamic", WorkflowPath: "dynamic/agents/github-advanced-security", Actor: bot("github-advanced-security[bot]")},
			wantID: "github-advanced-security", wantKind: KindAgent, wantRole: RoleRuntime,
		},
		{
			name:   "unknown GitHub-hosted agent",
			in:     Input{Event: "dynamic", WorkflowPath: "dynamic/agents/some-new-agent", Actor: user("alice")},
			wantID: "github-agent:some-new-agent", wantKind: KindAgent, wantRole: RoleRuntime,
		},
		{
			name:   "Copilot coding agent session",
			in:     Input{Event: "dynamic", WorkflowPath: "dynamic/copilot-swe-agent/copilot", Actor: bot("Copilot")},
			wantID: "copilot", wantKind: KindAgent, wantRole: RoleRuntime,
		},
		{
			name:   "agentic workflow lock file",
			in:     Input{Event: "pull_request", WorkflowPath: ".github/workflows/ai-moderator.lock.yml", Actor: bot("github-actions[bot]")},
			wantID: "gh-aw", wantKind: KindAgent, wantRole: RoleRuntime,
		},
		{
			name:   "CI on a PR opened by Copilot",
			in:     Input{Event: "pull_request", WorkflowPath: ".github/workflows/ci.yml", Actor: bot("Copilot"), PRAuthor: ptr(bot("Copilot")), Branch: "copilot/fix-thing"},
			wantID: "copilot", wantKind: KindAgent,
		},
		{
			name:   "Code Quality on a Copilot PR is CI, not runtime",
			in:     Input{Event: "dynamic", WorkflowPath: "dynamic/github-code-quality/codeql", Actor: bot("Copilot"), Branch: "refs/pull/72932/head"},
			wantID: "copilot", wantKind: KindAgent,
		},
		{
			name: "agentic workflow PR via github-actions[bot] (marker beats bot login and Copilot trailer)",
			in: Input{
				Event: "pull_request", WorkflowPath: ".github/workflows/ci.yml", Actor: bot("github-actions[bot]"),
				PRAuthor:      ptr(bot("github-actions[bot]")),
				PRBody:        "Removes dead code.\n\n<!-- gh-aw-agentic-workflow: Dead Code Removal Agent, engine: copilot -->",
				CommitMessage: "chore: remove dead function\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>",
			},
			wantID: "gh-aw", wantKind: KindAgent,
		},
		{
			name: "Copilot co-author trailer (2026 format)",
			in: Input{Event: "push", WorkflowPath: ".github/workflows/ci.yml", Actor: user("maintainer"),
				CommitMessage: "fix: thing (#12)\n\nCo-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"},
			wantID: "copilot", wantKind: KindAgent, wantOperator: "maintainer",
		},
		{
			name: "Claude Code through a person's account",
			in: Input{Event: "pull_request", Actor: user("maya"), PRAuthor: ptr(user("maya")), Branch: "feat/x",
				CommitMessage: "Add retry\n\n🤖 Generated with Claude Code\n\nCo-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"},
			wantID: "claude-code", wantKind: KindAgent, wantOperator: "maya",
		},
		{
			name:   "Claude Code PR body only",
			in:     Input{Event: "pull_request", Actor: user("maya"), PRAuthor: ptr(user("maya")), PRBody: "## Summary\n\n🤖 Generated with [Claude Code](https://claude.com/claude-code)"},
			wantID: "claude-code", wantKind: KindAgent, wantOperator: "maya",
		},
		{
			name:   "a human co-author called Claude is not Claude Code",
			in:     Input{Event: "pull_request", Actor: user("maya"), PRAuthor: ptr(user("maya")), CommitMessage: "Fix\n\nCo-authored-by: Claude Monet <claude@example.com>"},
			wantID: "human:maya", wantKind: KindHuman,
		},
		{
			name:   "claude[bot] pushing to a human PR",
			in:     Input{Event: "pull_request", Actor: bot("claude[bot]"), PRAuthor: ptr(user("maya")), Branch: "feat/y"},
			wantID: "claude-code", wantKind: KindAgent,
		},
		{
			name:   "Codex branch",
			in:     Input{Event: "pull_request", Actor: user("dan"), PRAuthor: ptr(user("dan")), Branch: "codex/add-tests"},
			wantID: "openai-codex", wantKind: KindAgent, wantOperator: "dan",
		},
		{
			name:   "Cursor agent commit author",
			in:     Input{Event: "push", Actor: user("dan"), CommitAuthorEmail: "cursoragent@cursor.com"},
			wantID: "cursor-agent", wantKind: KindAgent, wantOperator: "dan",
		},
		{
			name:   "Dependabot",
			in:     Input{Event: "pull_request", Actor: bot("dependabot[bot]"), PRAuthor: ptr(bot("dependabot[bot]")), Branch: "dependabot/npm_and_yarn/x-1.2.3"},
			wantID: "dependabot", wantKind: KindBot,
		},
		{
			name:   "Dependabot update job",
			in:     Input{Event: "dynamic", WorkflowPath: "dynamic/dependabot/dependabot-updates", Actor: bot("dependabot[bot]")},
			wantID: "dependabot", wantKind: KindBot, wantRole: RoleRuntime,
		},
		{
			name:   "github-actions push with no agent signals",
			in:     Input{Event: "push", Actor: bot("github-actions[bot]"), CommitMessage: "chore: release v1.2.3"},
			wantID: "github-actions", wantKind: KindBot,
		},
		{
			name:   "unknown bot",
			in:     Input{Event: "pull_request", Actor: bot("snyk-bot[bot]"), PRAuthor: ptr(bot("snyk-bot[bot]"))},
			wantID: "bot:snyk-bot[bot]", wantKind: KindBot,
		},
		{
			name:   "schedule beats the last cron editor",
			in:     Input{Event: "schedule", Actor: user("alice")},
			wantID: "schedule", wantKind: KindBot,
		},
		{
			name:   "unknown dynamic workflow",
			in:     Input{Event: "dynamic", WorkflowPath: "dynamic/pages/pages-build-deployment"},
			wantID: "dynamic", wantKind: KindBot, wantRole: RoleRuntime,
		},
		{
			name:   "plain human PR uses the PR author, not whoever pushed",
			in:     Input{Event: "pull_request", Actor: user("Maintainer"), PRAuthor: ptr(user("Alice"))},
			wantID: "human:alice", wantKind: KindHuman,
		},
		{
			name:   "no actor at all",
			in:     Input{Event: "push"},
			wantID: "unknown", wantKind: KindHuman,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.Classify(tt.in)
			if got.ID != tt.wantID || got.Kind != tt.wantKind || got.Role != tt.wantRole || got.Operator != tt.wantOperator {
				t.Errorf("Classify() = {ID:%q Kind:%q Role:%q Operator:%q} (evidence %q), want {ID:%q Kind:%q Role:%q Operator:%q}",
					got.ID, got.Kind, got.Role, got.Operator, got.Evidence, tt.wantID, tt.wantKind, tt.wantRole, tt.wantOperator)
			}
			if got.Evidence == "" {
				t.Error("missing evidence")
			}
		})
	}
}

func TestBuiltinSignaturesValid(t *testing.T) {
	sigs, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	if len(sigs) < 10 {
		t.Fatalf("only %d built-in signatures", len(sigs))
	}
	for _, s := range sigs {
		if len(s.Signals()) == 0 {
			t.Errorf("signature %q has no signals", s.ID)
		}
		for _, l := range s.Logins {
			if l != strings.TrimSpace(l) || l == "" {
				t.Errorf("signature %q: bad login %q", s.ID, l)
			}
		}
	}
}

func TestParseValidation(t *testing.T) {
	cases := map[string]string{
		"duplicate id": `{"signatures":[{"id":"a","name":"A","kind":"agent","logins":["x"]},{"id":"a","name":"B","kind":"bot"}]}`,
		"bad kind":     `{"signatures":[{"id":"a","name":"A","kind":"robot"}]}`,
		"missing name": `{"signatures":[{"id":"a","kind":"agent"}]}`,
		"bad regex":    `{"signatures":[{"id":"a","name":"A","kind":"agent","trailers":["("]}]}`,
		"not json":     `{`,
	}
	for name, data := range cases {
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestCustomSignaturesTakePriority(t *testing.T) {
	custom, err := Parse([]byte(`{"signatures":[{"id":"inhouse","name":"In-house agent","kind":"agent","branch_prefixes":["bot/"],"logins":["acme-agent[bot]"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	builtin, _ := Builtin()
	c := New(append(custom, builtin...))
	got := c.Classify(Input{Event: "pull_request", Actor: bot("acme-agent[bot]"), PRAuthor: ptr(bot("acme-agent[bot]"))})
	if got.ID != "inhouse" || got.Kind != KindAgent {
		t.Fatalf("got %+v", got)
	}
	got = c.Classify(Input{Event: "pull_request", Actor: user("bob"), PRAuthor: ptr(user("bob")), Branch: "bot/refactor"})
	if got.ID != "inhouse" || got.Operator != "bob" {
		t.Fatalf("got %+v", got)
	}
}
