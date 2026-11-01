package demo

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/amlaban65/ciblame/internal/collect"
	"github.com/amlaban65/ciblame/internal/pricing"
)

func TestDeterministic(t *testing.T) {
	until := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	since := until.AddDate(0, 0, -30)
	a, b := New(since, until), New(since, until)
	if !reflect.DeepEqual(a.runs, b.runs) || !reflect.DeepEqual(a.jobs, b.jobs) {
		t.Fatal("demo data should be deterministic")
	}
}

func TestDemoCoversEveryKind(t *testing.T) {
	until := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	since := until.AddDate(0, 0, -30)
	res, err := collect.Run(context.Background(), New(since, until), []string{Repo},
		collect.Options{Since: since, Until: until, Pricing: pricing.Default()})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, r := range res.Records {
		ids[r.Attribution.ID] = true
		if r.CreatedAt.Before(since) || r.CreatedAt.After(until) {
			t.Fatalf("run outside window: %v", r.CreatedAt)
		}
	}
	for _, want := range []string{"copilot", "copilot-code-review", "claude-code", "openai-codex", "devin", "dependabot", "schedule"} {
		if !ids[want] {
			t.Errorf("demo data never attributed to %q", want)
		}
	}
	if res.InProgress == 0 {
		t.Error("demo should include in-progress runs")
	}
}

func TestShortWindows(t *testing.T) {
	until := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for _, d := range []time.Duration{time.Hour, 24 * time.Hour, 7 * 24 * time.Hour, 90 * 24 * time.Hour} {
		src := New(until.Add(-d), until)
		if _, err := collect.Run(context.Background(), src, []string{Repo},
			collect.Options{Since: until.Add(-d), Until: until, Pricing: pricing.Default()}); err != nil {
			t.Fatalf("window %v: %v", d, err)
		}
	}
}
