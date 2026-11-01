package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/amlaban65/ciblame/internal/classify"
)

// Formats lists the supported output formats.
var Formats = []string{"table", "markdown", "json", "csv"}

// RenderOptions control rendering.
type RenderOptions struct {
	Color   bool
	Records bool // include per-run records (json, csv)
}

// Render writes rep in the given format.
func Render(w io.Writer, rep *Report, format string, opts RenderOptions) error {
	switch format {
	case "", "table":
		return renderTable(w, rep, opts)
	case "markdown", "md":
		return renderMarkdown(w, rep)
	case "json":
		return renderJSON(w, rep, opts)
	case "csv":
		return renderCSV(w, rep, opts)
	}
	return fmt.Errorf("unknown format %q (want one of %s)", format, strings.Join(Formats, ", "))
}

// ---- terminal table ----

type palette struct{ bold, dim, agent, bot, human, warn, reset string }

func newPalette(color bool) palette {
	if !color {
		return palette{}
	}
	return palette{
		bold: "\x1b[1m", dim: "\x1b[2m", agent: "\x1b[35m", bot: "\x1b[36m",
		human: "\x1b[32m", warn: "\x1b[33m", reset: "\x1b[0m",
	}
}

func (p palette) kind(k classify.Kind) string {
	switch k {
	case classify.KindAgent:
		return p.agent
	case classify.KindBot:
		return p.bot
	case classify.KindHuman:
		return p.human
	}
	return ""
}

type cell struct {
	text  string
	style string
}

type column struct {
	head  string
	right bool
}

func renderTable(w io.Writer, rep *Report, opts RenderOptions) error {
	p := newPalette(opts.Color)
	var b strings.Builder

	fmt.Fprintf(&b, "%sciblame%s %s·%s %s %s·%s %s\n", p.bold, p.reset, p.dim, p.reset, repoLabel(rep), p.dim, p.reset, windowLabel(rep.Since, rep.Until))
	t := rep.Total
	fmt.Fprintf(&b, "%s runs · %s jobs · %s billable min · %s%s%s at list price\n\n",
		commas(t.Runs), commas(t.Jobs), commas(t.Minutes), p.bold, usd(t.Cost), p.reset)

	if t.Runs == 0 {
		b.WriteString("No completed workflow runs in this window.\n")
		_, err := io.WriteString(w, b.String())
		return err
	}

	b.WriteString(kindBar(rep, p, 48))
	b.WriteString("\n\n")

	cols := []column{
		{head: firstHeader(rep.By)}, {head: "KIND"}, {head: "RUNS", right: true}, {head: "MINUTES", right: true},
		{head: "SHARE"}, {head: "EST $", right: true}, {head: "FAIL", right: true},
		{head: "MERGED", right: true}, {head: "MIN/MERGE", right: true}, {head: "UNMERGED", right: true},
	}
	maxShare := 0.0
	for _, r := range rep.Rows {
		maxShare = math.Max(maxShare, r.Share)
	}
	var rows [][]cell
	for _, r := range rep.Rows {
		ks := p.kind(r.Kind)
		rows = append(rows, []cell{
			{text: r.Label, style: ks},
			{text: string(r.Kind), style: ks},
			{text: commas(r.Runs)},
			{text: commas(r.Minutes), style: p.bold},
			{text: fmt.Sprintf("%5.1f%% %s", r.Share*100, bar(r.Share/maxShare, 12)), style: ks},
			{text: money(r.Cost)},
			{text: failCell(r)},
			{text: mergedCell(r)},
			{text: perMergeCell(r)},
			{text: dashIfZero(r.WastedMinutes)},
		})
	}
	writeTable(&b, cols, rows, p)

	if len(rep.Insights) > 0 {
		b.WriteString("\n")
		for _, s := range rep.Insights {
			fmt.Fprintf(&b, "%s▸%s %s\n", p.warn, p.reset, s)
		}
	}

	b.WriteString("\n")
	for _, n := range notes(rep) {
		fmt.Fprintf(&b, "%s%s%s\n", p.dim, n, p.reset)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func firstHeader(by string) string {
	switch by {
	case ByKind:
		return "KIND OF ACTOR"
	case ByWorkflow:
		return "WORKFLOW"
	case ByRepo:
		return "REPOSITORY"
	}
	return "WHO"
}

func writeTable(b *strings.Builder, cols []column, rows [][]cell, p palette) {
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = utf8.RuneCountInString(c.head)
	}
	for _, r := range rows {
		for i, c := range r {
			if n := utf8.RuneCountInString(c.text); n > widths[i] {
				widths[i] = n
			}
		}
	}
	// Cap the first column so very long workflow names don't blow up the layout.
	if widths[0] > 40 {
		widths[0] = 40
	}
	line := func(cells []cell, header bool) {
		for i, c := range cells {
			text := c.text
			if i == 0 && utf8.RuneCountInString(text) > widths[0] {
				text = string([]rune(text)[:widths[0]-1]) + "…"
			}
			pad := strings.Repeat(" ", widths[i]-utf8.RuneCountInString(text))
			if i > 0 {
				b.WriteString("  ")
			}
			style := c.style
			if header {
				style = p.dim
			}
			if cols[i].right {
				b.WriteString(pad)
			}
			if style != "" {
				b.WriteString(style + text + p.reset)
			} else {
				b.WriteString(text)
			}
			if !cols[i].right && i < len(cells)-1 {
				b.WriteString(pad)
			}
		}
		b.WriteString("\n")
	}
	head := make([]cell, len(cols))
	for i, c := range cols {
		head[i] = cell{text: c.head}
	}
	line(head, true)
	for _, r := range rows {
		line(r, false)
	}
}

// kindBar draws a stacked bar of agent / bot / human share of minutes.
func kindBar(rep *Report, p palette, width int) string {
	type seg struct {
		kind  classify.Kind
		label string
		ch    string
	}
	segs := []seg{{classify.KindAgent, "agents", "█"}, {classify.KindBot, "bots", "▓"}, {classify.KindHuman, "humans", "░"}}
	var bar, legend strings.Builder
	used := 0
	for i, s := range segs {
		share := rep.Kinds[string(s.kind)].Share
		n := int(math.Round(share * float64(width)))
		if i == len(segs)-1 {
			n = width - used
		}
		if n < 0 {
			n = 0
		}
		if used+n > width {
			n = width - used
		}
		used += n
		if n > 0 {
			bar.WriteString(p.kind(s.kind) + strings.Repeat(s.ch, n) + p.reset)
		}
		if i > 0 {
			legend.WriteString("   ")
		}
		fmt.Fprintf(&legend, "%s%s %s%s %s", p.kind(s.kind), s.ch, s.label, p.reset, pct(share))
	}
	return bar.String() + "\n" + legend.String()
}

func bar(frac float64, width int) string {
	if frac <= 0 || math.IsNaN(frac) {
		return strings.Repeat(" ", width)
	}
	if frac > 1 {
		frac = 1
	}
	eighths := int(math.Round(frac * float64(width*8)))
	if eighths == 0 {
		eighths = 1
	}
	full, rem := eighths/8, eighths%8
	partial := []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}
	s := strings.Repeat("█", full) + partial[rem]
	return s + strings.Repeat(" ", width-utf8.RuneCountInString(s))
}

func money(f float64) string {
	if f >= 1000 {
		return "$" + commas(int(math.Round(f)))
	}
	return fmt.Sprintf("$%.2f", f)
}

func failCell(r Row) string {
	if r.Runs == 0 {
		return "–"
	}
	return pct(r.FailRate())
}

func mergedCell(r Row) string {
	if r.PRs == 0 {
		return "–"
	}
	return fmt.Sprintf("%d/%d", r.MergedPRs, r.PRs)
}

func perMergeCell(r Row) string {
	if r.MergedPRs == 0 {
		return "–"
	}
	return fmt.Sprintf("%.0f", r.MinutesPerMerge())
}

func dashIfZero(n int) string {
	if n == 0 {
		return "–"
	}
	return commas(n)
}

func repoLabel(rep *Report) string {
	switch len(rep.Repos) {
	case 0:
		return "(no repos)"
	case 1:
		return rep.Repos[0].FullName
	case 2:
		return rep.Repos[0].FullName + ", " + rep.Repos[1].FullName
	}
	return fmt.Sprintf("%d repos", len(rep.Repos))
}

func windowLabel(since, until time.Time) string {
	days := int(math.Round(until.Sub(since).Hours() / 24))
	from := since.Format("Jan 2")
	if since.Year() != until.Year() {
		from = since.Format("Jan 2, 2006")
	}
	return fmt.Sprintf("%s – %s (%d %s)", from, until.Format("Jan 2, 2006"), days, plural(days, "day", "days"))
}

// notes are the footnotes explaining columns and estimate caveats.
func notes(rep *Report) []string {
	n := []string{
		"MERGED = merged/total PRs · MIN/MERGE = CI minutes per merged PR, counting abandoned PRs · UNMERGED = minutes on PRs closed without merging",
	}
	rates := fmt.Sprintf("Costs are estimates at GitHub list prices (Linux $%.3f/min, Windows $%.3f, macOS $%.3f; larger runners scaled by core count).",
		rep.Rates.Linux, rep.Rates.Windows, rep.Rates.MacOS)
	n = append(n, rates)
	if rep.AnyPublic {
		n = append(n, "Public repos don't pay for standard GitHub-hosted runners, so their costs are list-price equivalents.")
	}
	if rep.Approximate {
		n = append(n, "Minutes were estimated from run wall-clock time (--fast); drop --fast for exact per-job minutes.")
	}
	if rep.SelfHostedMinutes > 0 {
		n = append(n, fmt.Sprintf("%s min ran on self-hosted runners, priced at $%.3f/min (--self-hosted-rate).",
			commas(rep.SelfHostedMinutes), rep.Rates.SelfHosted))
	}
	if len(rep.Unpriced) > 0 {
		type kv struct {
			k string
			v int
		}
		var list []kv
		total := 0
		for k, v := range rep.Unpriced {
			list = append(list, kv{k, v})
			total += v
		}
		sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
		var parts []string
		for i, e := range list {
			if i == 3 {
				parts = append(parts, fmt.Sprintf("+%d more", len(list)-3))
				break
			}
			parts = append(parts, fmt.Sprintf("%s (%s min)", e.k, commas(e.v)))
		}
		n = append(n, fmt.Sprintf("%s min on runners with no known rate are not in the cost: %s. Price them with --rate LABEL=USD.",
			commas(total), strings.Join(parts, ", ")))
	}
	if rep.InProgress > 0 {
		n = append(n, fmt.Sprintf("%s in-progress %s skipped.", commas(rep.InProgress), plural(rep.InProgress, "run was", "runs were")))
	}
	return n
}

// ---- markdown ----

func renderMarkdown(w io.Writer, rep *Report) error {
	var b strings.Builder
	t := rep.Total
	fmt.Fprintf(&b, "### ciblame · %s · %s\n\n", mdEscape(repoLabel(rep)), windowLabel(rep.Since, rep.Until))
	fmt.Fprintf(&b, "**%s runs · %s billable minutes · %s at list price** — ",
		commas(t.Runs), commas(t.Minutes), usd(t.Cost))
	var ks []string
	for _, k := range []classify.Kind{classify.KindAgent, classify.KindBot, classify.KindHuman} {
		ks = append(ks, fmt.Sprintf("%s %s %s", kindEmoji(k), kindPlural(k), pct(rep.Kinds[string(k)].Share)))
	}
	b.WriteString(strings.Join(ks, " · "))
	b.WriteString("\n\n")

	if t.Runs == 0 {
		b.WriteString("No completed workflow runs in this window.\n")
		_, err := io.WriteString(w, b.String())
		return err
	}

	head := strings.ToLower(firstHeader(rep.By))
	head = strings.ToUpper(head[:1]) + head[1:]
	fmt.Fprintf(&b, "| %s | Kind | Runs | Minutes | Share | Est. $ | Fail | Merged PRs | Min/merge | Unmerged min |\n", head)
	b.WriteString("|---|---|--:|--:|--:|--:|--:|--:|--:|--:|\n")
	for _, r := range rep.Rows {
		kind := ""
		if r.Kind != "" {
			kind = kindEmoji(r.Kind) + " " + string(r.Kind)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			mdEscape(r.Label), kind, commas(r.Runs), commas(r.Minutes), pct(r.Share), money(r.Cost),
			failCell(r), mergedCell(r), perMergeCell(r), dashIfZero(r.WastedMinutes))
	}
	if len(rep.Insights) > 0 {
		b.WriteString("\n")
		for _, s := range rep.Insights {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	b.WriteString("\n<sub>")
	b.WriteString(strings.Join(notes(rep), "<br>"))
	b.WriteString(" Generated by [ciblame](https://github.com/amlaban65/ciblame).</sub>\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func kindEmoji(k classify.Kind) string {
	switch k {
	case classify.KindAgent:
		return "🤖"
	case classify.KindBot:
		return "⚙️"
	case classify.KindHuman:
		return "👤"
	}
	return ""
}

func kindPlural(k classify.Kind) string {
	return string(k) + "s"
}

func mdEscape(s string) string {
	return strings.NewReplacer("|", `\|`, "<", "&lt;", ">", "&gt;").Replace(s)
}

// ---- json ----

func renderJSON(w io.Writer, rep *Report, opts RenderOptions) error {
	out := *rep
	if !opts.Records {
		out.Records = nil
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// ---- csv ----

func renderCSV(w io.Writer, rep *Report, opts RenderOptions) error {
	cw := csv.NewWriter(w)
	if opts.Records {
		_ = cw.Write([]string{"repo", "run_id", "url", "workflow", "event", "branch", "created_at", "attempts", "conclusion",
			"attributed_to", "kind", "role", "evidence", "operator", "pr_number", "pr_state", "pr_author",
			"jobs", "billable_minutes", "self_hosted_minutes", "estimated_cost_usd", "approximate"})
		for _, r := range rep.Records {
			prNum, prState, prAuthor := "", "", ""
			if r.PR != nil {
				prNum, prState, prAuthor = strconv.Itoa(r.PR.Number), r.PR.State, r.PR.Author
			}
			_ = cw.Write([]string{r.Repo, strconv.FormatInt(r.RunID, 10), r.URL, r.Workflow, r.Event, r.Branch,
				r.CreatedAt.UTC().Format(time.RFC3339), strconv.Itoa(r.Attempts), r.Conclusion,
				r.Attribution.Name, string(r.Attribution.Kind), r.Attribution.Role, r.Attribution.Evidence, r.Attribution.Operator,
				prNum, prState, prAuthor,
				strconv.Itoa(r.Jobs), strconv.Itoa(r.Minutes), strconv.Itoa(r.SelfHostedMinutes),
				strconv.FormatFloat(r.Cost, 'f', 4, 64), strconv.FormatBool(r.Approx)})
		}
	} else {
		_ = cw.Write([]string{"key", "label", "kind", "runs", "failed_runs", "reruns", "jobs", "billable_minutes", "share",
			"estimated_cost_usd", "agent_runtime_minutes", "pull_requests", "merged_pull_requests",
			"closed_unmerged_pull_requests", "minutes_per_merged_pr", "minutes_on_unmerged_prs"})
		for _, r := range rep.Rows {
			_ = cw.Write([]string{r.Key, r.Label, string(r.Kind), strconv.Itoa(r.Runs), strconv.Itoa(r.Failed),
				strconv.Itoa(r.Reruns), strconv.Itoa(r.Jobs), strconv.Itoa(r.Minutes),
				strconv.FormatFloat(r.Share, 'f', 4, 64), strconv.FormatFloat(r.Cost, 'f', 4, 64),
				strconv.Itoa(r.RuntimeMinutes), strconv.Itoa(r.PRs), strconv.Itoa(r.MergedPRs), strconv.Itoa(r.ClosedPRs),
				strconv.FormatFloat(r.MinutesPerMerge(), 'f', 1, 64), strconv.Itoa(r.WastedMinutes)})
		}
	}
	cw.Flush()
	return cw.Error()
}
