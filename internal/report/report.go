// Package report aggregates attributed runs into a report and renders it as
// a terminal table, Markdown, JSON or CSV.
package report

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/amlaban65/ciblame/internal/classify"
	"github.com/amlaban65/ciblame/internal/collect"
	"github.com/amlaban65/ciblame/internal/pricing"
)

// Grouping dimensions.
const (
	ByActor    = "actor"
	ByKind     = "kind"
	ByWorkflow = "workflow"
	ByRepo     = "repo"
)

// Groupings lists the valid values for Options.By.
var Groupings = []string{ByActor, ByKind, ByWorkflow, ByRepo}

// Options control aggregation.
type Options struct {
	By string
	// PerHuman lists each person separately. By default humans are collapsed
	// into one "humans" row so the report is about tools, not people.
	PerHuman bool
	// Top limits the number of rows; the remainder is folded into one row. 0 = all.
	Top     int
	Pricing pricing.Table
}

// Row is one line of the report.
type Row struct {
	Key   string        `json:"key"`
	Label string        `json:"label"`
	Kind  classify.Kind `json:"kind,omitempty"`

	Runs    int     `json:"runs"`
	Failed  int     `json:"failed_runs"`
	Reruns  int     `json:"reruns"`
	Jobs    int     `json:"jobs"`
	Minutes int     `json:"billable_minutes"`
	Cost    float64 `json:"estimated_cost_usd"`
	Share   float64 `json:"share_of_minutes"`

	// RuntimeMinutes are minutes of the agent itself executing on Actions.
	RuntimeMinutes int `json:"agent_runtime_minutes,omitempty"`

	PRs       int `json:"pull_requests"`
	MergedPRs int `json:"merged_pull_requests"`
	// ClosedPRs were closed without merging.
	ClosedPRs int `json:"closed_unmerged_pull_requests"`
	// ResolvedPRMinutes are minutes on runs of merged or closed pull requests.
	ResolvedPRMinutes int `json:"minutes_on_resolved_pull_requests"`
	// WastedMinutes are minutes on runs of pull requests closed without merging.
	WastedMinutes int `json:"minutes_on_unmerged_pull_requests"`

	// Members counts distinct people folded into a collapsed "humans" row.
	Members int `json:"members,omitempty"`

	prs    map[string]string // repo#number -> state
	people map[string]bool
}

// FailRate is the share of runs that failed.
func (r *Row) FailRate() float64 {
	if r.Runs == 0 {
		return 0
	}
	return float64(r.Failed) / float64(r.Runs)
}

// MinutesPerMerge is CI minutes spent per merged pull request, counting
// minutes on both merged and abandoned pull requests (the full cost of
// getting one change in). It is 0 when nothing merged.
func (r *Row) MinutesPerMerge() float64 {
	if r.MergedPRs == 0 {
		return 0
	}
	return float64(r.ResolvedPRMinutes) / float64(r.MergedPRs)
}

// Report is an aggregated view over collected runs.
type Report struct {
	Repos       []collect.RepoInfo `json:"repos"`
	Since       time.Time          `json:"since"`
	Until       time.Time          `json:"until"`
	By          string             `json:"group_by"`
	GeneratedAt time.Time          `json:"generated_at"`

	Total Row            `json:"total"`
	Kinds map[string]Row `json:"by_kind"`
	Rows  []Row          `json:"rows"`

	Insights []string `json:"insights"`

	SelfHostedMinutes int            `json:"self_hosted_minutes"`
	Unpriced          map[string]int `json:"unpriced_runner_minutes,omitempty"`
	InProgress        int            `json:"in_progress_runs_skipped"`
	Approximate       bool           `json:"approximate"`
	AnyPublic         bool           `json:"includes_public_repos"`
	Rates             RateCard       `json:"rates_usd_per_minute"`

	Records []collect.Record `json:"records,omitempty"`
}

// RateCard documents the base rates used for cost estimates.
type RateCard struct {
	Linux      float64            `json:"linux"`
	Windows    float64            `json:"windows"`
	MacOS      float64            `json:"macos"`
	SelfHosted float64            `json:"self_hosted"`
	Overrides  map[string]float64 `json:"overrides,omitempty"`
}

// Build aggregates a collection result.
func Build(res *collect.Result, since, until time.Time, opts Options) *Report {
	if opts.By == "" {
		opts.By = ByActor
	}
	rep := &Report{
		Repos:       res.Repos,
		Since:       since,
		Until:       until,
		By:          opts.By,
		GeneratedAt: time.Now().UTC(),
		Kinds:       map[string]Row{},
		Unpriced:    map[string]int{},
		InProgress:  res.InProgress,
		Rates: RateCard{
			Linux: opts.Pricing.Linux, Windows: opts.Pricing.Windows, MacOS: opts.Pricing.MacOS,
			SelfHosted: opts.Pricing.SelfHosted, Overrides: opts.Pricing.Overrides,
		},
	}
	for _, r := range res.Repos {
		if !r.Private {
			rep.AnyPublic = true
		}
	}

	rows := map[string]*Row{}
	kinds := map[classify.Kind]*Row{}
	total := &Row{Key: "total", Label: "total"}

	for i := range res.Records {
		rec := &res.Records[i]
		key, label, kind := groupKey(rec, opts)
		row := rows[key]
		if row == nil {
			row = &Row{Key: key, Label: label, Kind: kind}
			rows[key] = row
		}
		k := kinds[rec.Attribution.Kind]
		if k == nil {
			k = &Row{Key: string(rec.Attribution.Kind), Label: string(rec.Attribution.Kind), Kind: rec.Attribution.Kind}
			kinds[rec.Attribution.Kind] = k
		}
		for _, r := range []*Row{row, k, total} {
			r.add(rec)
		}
		if rec.Attribution.Kind == classify.KindHuman && !opts.PerHuman && opts.By == ByActor {
			if row.people == nil {
				row.people = map[string]bool{}
			}
			row.people[rec.Attribution.ID] = true
		}
		rep.SelfHostedMinutes += rec.SelfHostedMinutes
		for l, m := range rec.Unpriced {
			rep.Unpriced[l] += m
		}
		if rec.Approx {
			rep.Approximate = true
		}
	}

	for _, r := range rows {
		r.finish(total.Minutes)
		if r.people != nil {
			r.Members = len(r.people)
			r.Label = fmt.Sprintf("humans (%d %s)", r.Members, plural(r.Members, "person", "people"))
		}
		rep.Rows = append(rep.Rows, *r)
	}
	for k, r := range kinds {
		r.finish(total.Minutes)
		rep.Kinds[string(k)] = *r
	}
	total.finish(total.Minutes)
	rep.Total = *total

	sort.SliceStable(rep.Rows, func(i, j int) bool {
		a, b := rep.Rows[i], rep.Rows[j]
		if a.Minutes != b.Minutes {
			return a.Minutes > b.Minutes
		}
		if a.Runs != b.Runs {
			return a.Runs > b.Runs
		}
		return a.Label < b.Label
	})
	if opts.Top > 0 && len(rep.Rows) > opts.Top {
		rep.Rows = foldTail(rep.Rows, opts.Top, total.Minutes)
	}
	if len(rep.Unpriced) == 0 {
		rep.Unpriced = nil
	}
	rep.Insights = insights(rep)
	return rep
}

func groupKey(rec *collect.Record, opts Options) (key, label string, kind classify.Kind) {
	a := rec.Attribution
	switch opts.By {
	case ByKind:
		return string(a.Kind), string(a.Kind), a.Kind
	case ByWorkflow:
		return rec.Repo + ":" + rec.WorkflowPath, rec.Workflow, ""
	case ByRepo:
		return rec.Repo, rec.Repo, ""
	default:
		if a.Kind == classify.KindHuman && !opts.PerHuman {
			return "humans", "humans", classify.KindHuman
		}
		return a.ID, a.Name, a.Kind
	}
}

func (r *Row) add(rec *collect.Record) {
	r.Runs++
	r.Jobs += rec.Jobs
	r.Minutes += rec.Minutes
	r.Cost += rec.Cost
	r.Reruns += rec.Attempts - 1
	if rec.Failed() {
		r.Failed++
	}
	if rec.Attribution.Role == classify.RoleRuntime {
		r.RuntimeMinutes += rec.Minutes
	}
	if rec.PR != nil {
		if r.prs == nil {
			r.prs = map[string]string{}
		}
		r.prs[fmt.Sprintf("%s#%d", rec.Repo, rec.PR.Number)] = rec.PR.State
		switch rec.PR.State {
		case collect.PRMerged:
			r.ResolvedPRMinutes += rec.Minutes
		case collect.PRClosed:
			r.ResolvedPRMinutes += rec.Minutes
			r.WastedMinutes += rec.Minutes
		}
	}
}

func (r *Row) finish(totalMinutes int) {
	r.Cost = math.Round(r.Cost*100) / 100
	r.PRs = len(r.prs)
	r.MergedPRs, r.ClosedPRs = 0, 0
	for _, st := range r.prs {
		switch st {
		case collect.PRMerged:
			r.MergedPRs++
		case collect.PRClosed:
			r.ClosedPRs++
		}
	}
	if totalMinutes > 0 {
		r.Share = float64(r.Minutes) / float64(totalMinutes)
	}
}

func foldTail(rows []Row, top, totalMinutes int) []Row {
	head := append([]Row(nil), rows[:top]...)
	other := Row{Key: "other", Label: fmt.Sprintf("(%d more)", len(rows)-top)}
	for _, r := range rows[top:] {
		other.Runs += r.Runs
		other.Failed += r.Failed
		other.Reruns += r.Reruns
		other.Jobs += r.Jobs
		other.Minutes += r.Minutes
		other.Cost += r.Cost
		other.RuntimeMinutes += r.RuntimeMinutes
		other.PRs += r.PRs
		other.MergedPRs += r.MergedPRs
		other.ClosedPRs += r.ClosedPRs
		other.ResolvedPRMinutes += r.ResolvedPRMinutes
		other.WastedMinutes += r.WastedMinutes
	}
	if totalMinutes > 0 {
		other.Share = float64(other.Minutes) / float64(totalMinutes)
	}
	return append(head, other)
}

// insights produces the plain-language findings shown under the table.
func insights(rep *Report) []string {
	var out []string
	if rep.Total.Runs == 0 {
		return []string{"No completed workflow runs in this window."}
	}
	agents, hasAgents := rep.Kinds[string(classify.KindAgent)]
	humans := rep.Kinds[string(classify.KindHuman)]
	if !hasAgents || agents.Runs == 0 {
		return []string{"No AI-agent activity detected. If you use an agent ciblame doesn't recognise, add a signature (see `ciblame agents`)."}
	}

	out = append(out, fmt.Sprintf("AI agents used %s of CI minutes (%s min, %s) across %s runs.",
		pct(agents.Share), commas(agents.Minutes), usd(agents.Cost), commas(agents.Runs)))

	// Top agent by minutes, regardless of the chosen grouping.
	if top := topAgent(rep); top != "" {
		out = append(out, top)
	}
	if agents.WastedMinutes > 0 {
		out = append(out, fmt.Sprintf("%s agent minutes (%s of agent CI) went to %s closed without merging.",
			commas(agents.WastedMinutes), pct(ratio(agents.WastedMinutes, agents.Minutes)),
			plural(agents.ClosedPRs, "a pull request", fmt.Sprintf("%d pull requests", agents.ClosedPRs))))
	}
	am, hm := agents.MinutesPerMerge(), humans.MinutesPerMerge()
	if am > 0 && hm > 0 {
		x := am / hm
		switch {
		case x >= 1.15:
			out = append(out, fmt.Sprintf("Agent PRs needed %.1f× the CI per merged PR of human PRs (%.0f vs %.0f min).", x, am, hm))
		case x <= 0.87:
			out = append(out, fmt.Sprintf("Agent PRs needed less CI per merged PR than human PRs (%.0f vs %.0f min).", am, hm))
		default:
			out = append(out, fmt.Sprintf("Agent and human PRs cost about the same CI per merged PR (%.0f vs %.0f min).", am, hm))
		}
	}
	if humans.Runs > 0 && agents.Runs >= 10 {
		af, hf := agents.FailRate(), humans.FailRate()
		if af-hf >= 0.05 {
			out = append(out, fmt.Sprintf("Agent runs failed %s of the time vs %s for humans.", pct(af), pct(hf)))
		}
	}
	if agents.RuntimeMinutes > 0 {
		out = append(out, fmt.Sprintf("%s of agent minutes were the agents themselves running on Actions (sessions, reviews); the rest is CI on their changes.",
			pct(ratio(agents.RuntimeMinutes, agents.Minutes))))
	}
	return out
}

// topAgent names the agent with the most minutes, computed from records-level
// rows when grouping by actor, otherwise omitted.
func topAgent(rep *Report) string {
	if rep.By != ByActor {
		return ""
	}
	var best *Row
	n := 0
	for i := range rep.Rows {
		r := &rep.Rows[i]
		if r.Kind != classify.KindAgent {
			continue
		}
		n++
		if best == nil || r.Minutes > best.Minutes {
			best = r
		}
	}
	if best == nil || n < 2 {
		return ""
	}
	return fmt.Sprintf("Biggest consumer: %s with %s min (%s of all CI).", best.Label, commas(best.Minutes), pct(best.Share))
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

func pct(f float64) string {
	p := f * 100
	switch {
	case p == 0:
		return "0%"
	case p < 1:
		return "<1%"
	case p < 10:
		return fmt.Sprintf("%.1f%%", p)
	default:
		return fmt.Sprintf("%.0f%%", p)
	}
}

func usd(f float64) string {
	if f >= 100 {
		return "≈ $" + commas(int(f+0.5))
	}
	return fmt.Sprintf("≈ $%.2f", f)
}

func commas(n int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
