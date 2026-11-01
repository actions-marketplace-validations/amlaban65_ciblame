package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/amlaban65/ciblame/internal/classify"
	"github.com/amlaban65/ciblame/internal/collect"
	"github.com/amlaban65/ciblame/internal/pricing"
)

var (
	since = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	until = since.Add(30 * 24 * time.Hour)
)

func rec(who classify.Result, minutes int, pr *collect.PRRef, concl string) collect.Record {
	return collect.Record{
		Repo: "acme/web", Attribution: who, Minutes: minutes, Cost: float64(minutes) * pricing.LinuxRate,
		PR: pr, Conclusion: concl, Attempts: 1, Jobs: 1, Workflow: "CI", WorkflowPath: ".github/workflows/ci.yml",
	}
}

var (
	copilot = classify.Result{ID: "copilot", Name: "GitHub Copilot", Kind: classify.KindAgent}
	review  = classify.Result{ID: "copilot-code-review", Name: "Copilot code review", Kind: classify.KindAgent, Role: classify.RoleRuntime}
	alice   = classify.Result{ID: "human:alice", Name: "alice", Kind: classify.KindHuman}
	bob     = classify.Result{ID: "human:bob", Name: "bob", Kind: classify.KindHuman}
	dbot    = classify.Result{ID: "dependabot", Name: "Dependabot", Kind: classify.KindBot}
)

func fixture() *collect.Result {
	merged := func(n int) *collect.PRRef { return &collect.PRRef{Number: n, State: collect.PRMerged} }
	closed := func(n int) *collect.PRRef { return &collect.PRRef{Number: n, State: collect.PRClosed} }
	return &collect.Result{
		Repos: []collect.RepoInfo{{FullName: "acme/web", Private: true}},
		Records: []collect.Record{
			// Copilot: PR 1 merged after 3 runs (90 min), PR 2 abandoned (60 min).
			rec(copilot, 30, merged(1), "failure"),
			rec(copilot, 30, merged(1), "failure"),
			rec(copilot, 30, merged(1), "success"),
			rec(copilot, 60, closed(2), "failure"),
			rec(review, 10, merged(1), "success"),
			// Humans: two people, one merged PR each, 20 min each.
			rec(alice, 20, merged(3), "success"),
			rec(bob, 20, merged(4), "success"),
			rec(dbot, 10, nil, "success"),
		},
	}
}

func TestBuild(t *testing.T) {
	rep := Build(fixture(), since, until, Options{Pricing: pricing.Default()})
	if rep.Total.Minutes != 210 || rep.Total.Runs != 8 {
		t.Fatalf("total %+v", rep.Total)
	}
	if len(rep.Rows) != 4 {
		t.Fatalf("want 4 rows (copilot, humans, review, dependabot), got %d: %+v", len(rep.Rows), rep.Rows)
	}
	top := rep.Rows[0]
	if top.Key != "copilot" || top.Minutes != 150 || top.Failed != 3 || top.MergedPRs != 1 || top.ClosedPRs != 1 || top.WastedMinutes != 60 {
		t.Fatalf("copilot row %+v", top)
	}
	if got := top.MinutesPerMerge(); got != 150 {
		t.Fatalf("copilot minutes per merge = %v, want 150", got)
	}
	humans := rep.Rows[1]
	if humans.Key != "humans" || humans.Members != 2 || humans.Label != "humans (2 people)" || humans.MinutesPerMerge() != 20 {
		t.Fatalf("humans row %+v", humans)
	}
	agents := rep.Kinds["agent"]
	if agents.Minutes != 160 || agents.RuntimeMinutes != 10 {
		t.Fatalf("agent kind %+v", agents)
	}
	want := []string{"AI agents used 76% of CI minutes", "Biggest consumer: GitHub Copilot",
		"60 agent minutes", "Agent PRs needed 8.0× the CI per merged PR"}
	joined := strings.Join(rep.Insights, "\n")
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Errorf("insights missing %q:\n%s", w, joined)
		}
	}
	if strings.Contains(joined, "failed") {
		t.Errorf("fail-rate comparison needs at least 10 agent runs:\n%s", joined)
	}
}

func TestPerHumanAndTop(t *testing.T) {
	rep := Build(fixture(), since, until, Options{PerHuman: true, Top: 2, Pricing: pricing.Default()})
	if len(rep.Rows) != 3 || rep.Rows[2].Key != "other" || rep.Rows[2].Label != "(3 more)" {
		t.Fatalf("rows %+v", rep.Rows)
	}
	sum := 0
	for _, r := range rep.Rows {
		sum += r.Minutes
	}
	if sum != rep.Total.Minutes {
		t.Fatalf("folded rows lose minutes: %d vs %d", sum, rep.Total.Minutes)
	}
}

func TestGroupings(t *testing.T) {
	for _, by := range Groupings {
		rep := Build(fixture(), since, until, Options{By: by, Pricing: pricing.Default()})
		sum := 0
		for _, r := range rep.Rows {
			sum += r.Minutes
		}
		if sum != 210 {
			t.Errorf("by=%s: rows sum to %d", by, sum)
		}
	}
}

func TestNoAgents(t *testing.T) {
	res := &collect.Result{Records: []collect.Record{rec(alice, 5, nil, "success")}}
	rep := Build(res, since, until, Options{Pricing: pricing.Default()})
	if len(rep.Insights) != 1 || !strings.Contains(rep.Insights[0], "No AI-agent activity") {
		t.Fatalf("insights %v", rep.Insights)
	}
	empty := Build(&collect.Result{}, since, until, Options{Pricing: pricing.Default()})
	var buf bytes.Buffer
	if err := Render(&buf, empty, "table", RenderOptions{}); err != nil || !strings.Contains(buf.String(), "No completed workflow runs") {
		t.Fatalf("empty table: %v %q", err, buf.String())
	}
}

func TestRenderFormats(t *testing.T) {
	res := fixture()
	rep := Build(res, since, until, Options{Pricing: pricing.Default()})
	rep.Records = res.Records

	var buf bytes.Buffer
	if err := Render(&buf, rep, "table", RenderOptions{}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, w := range []string{"acme/web", "Oct 1 – Oct 31, 2026 (30 days)", "GitHub Copilot", "humans (2 people)", "1/2", "▸ AI agents"} {
		if !strings.Contains(out, w) {
			t.Errorf("table missing %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("uncolored table contains ANSI escapes")
	}
	buf.Reset()
	if err := Render(&buf, rep, "table", RenderOptions{Color: true}); err != nil || !strings.Contains(buf.String(), "\x1b[35m") {
		t.Error("colored table should use the agent color")
	}

	buf.Reset()
	if err := Render(&buf, rep, "markdown", RenderOptions{}); err != nil {
		t.Fatal(err)
	}
	if md := buf.String(); !strings.Contains(md, "| GitHub Copilot | 🤖 agent | 4 | 150 |") || !strings.Contains(md, "### ciblame") {
		t.Errorf("markdown:\n%s", md)
	}

	buf.Reset()
	if err := Render(&buf, rep, "json", RenderOptions{Records: true}); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Total   Row              `json:"total"`
		Records []collect.Record `json:"records"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Total.Minutes != 210 || len(decoded.Records) != 8 {
		t.Errorf("json total %+v records %d", decoded.Total, len(decoded.Records))
	}

	for _, records := range []bool{false, true} {
		buf.Reset()
		if err := Render(&buf, rep, "csv", RenderOptions{Records: records}); err != nil {
			t.Fatal(err)
		}
		rows, err := csv.NewReader(&buf).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		want := len(rep.Rows) + 1
		if records {
			want = len(rep.Records) + 1
		}
		if len(rows) != want {
			t.Errorf("csv records=%v: %d rows, want %d", records, len(rows), want)
		}
	}

	if err := Render(&buf, rep, "yaml", RenderOptions{}); err == nil {
		t.Error("unknown format should fail")
	}
}

func TestHelpers(t *testing.T) {
	if commas(1234567) != "1,234,567" || commas(12) != "12" || commas(-1000) != "-1,000" {
		t.Error("commas")
	}
	if pct(0.004) != "<1%" || pct(0.0512) != "5.1%" || pct(0.733) != "73%" || pct(0) != "0%" {
		t.Error("pct")
	}
	if b := bar(0.5, 4); b != "██  " {
		t.Errorf("bar = %q", b)
	}
}
