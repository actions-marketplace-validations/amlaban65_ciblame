// Package pricing turns GitHub Actions jobs into billable minutes and
// estimated list-price cost.
package pricing

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Default per-minute list prices (USD) for standard 2-core GitHub-hosted
// runners, effective 2026-01-01.
const (
	LinuxRate   = 0.006
	WindowsRate = 0.010
	MacOSRate   = 0.062
)

// Runner describes the machine a job ran on, as inferred from its labels.
type Runner struct {
	OS         string // "linux", "windows", "macos" or "" if unknown
	Cores      int    // 0 if not stated in a label
	ARM        bool
	SelfHosted bool
	Large      bool // macOS large/xlarge or other non-standard size
}

// Table holds per-minute rates.
type Table struct {
	Linux, Windows, MacOS float64
	// SelfHosted is the rate applied to self-hosted runners. GitHub charges
	// nothing for them, so the default is 0; set it to your own infra cost.
	SelfHosted float64
	// Overrides maps an exact runner label (case-insensitive) to a rate and
	// takes precedence over everything else.
	Overrides map[string]float64
}

// Default returns GitHub's 2026 list prices.
func Default() Table {
	return Table{Linux: LinuxRate, Windows: WindowsRate, MacOS: MacOSRate, Overrides: map[string]float64{}}
}

// ParseOverride parses "LABEL=USD" into a label and rate.
func ParseOverride(s string) (string, float64, error) {
	label, rate, ok := strings.Cut(s, "=")
	label = strings.TrimSpace(label)
	if !ok || label == "" {
		return "", 0, fmt.Errorf("invalid rate %q: want LABEL=USD_PER_MINUTE", s)
	}
	r, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(rate, "$")), 64)
	if err != nil || r < 0 || math.IsNaN(r) || math.IsInf(r, 0) {
		return "", 0, fmt.Errorf("invalid rate %q: want LABEL=USD_PER_MINUTE", s)
	}
	return strings.ToLower(label), r, nil
}

var coresRe = regexp.MustCompile(`(\d+)[-_ ]?(?:cores?|cpus?|vcpus?)\b`)

// Identify infers the runner from a job's labels.
func Identify(labels []string) Runner {
	var r Runner
	for _, raw := range labels {
		l := strings.ToLower(strings.TrimSpace(raw))
		switch {
		case l == "self-hosted":
			r.SelfHosted = true
		case strings.HasPrefix(l, "ubuntu") || strings.HasPrefix(l, "linux"):
			r.OS = "linux"
		case strings.HasPrefix(l, "windows"):
			r.OS = "windows"
		case strings.HasPrefix(l, "macos") || strings.HasPrefix(l, "mac-"):
			r.OS = "macos"
		}
		if strings.Contains(l, "arm") {
			r.ARM = true
		}
		if m := coresRe.FindStringSubmatch(l); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
				r.Cores = n
			}
		}
		if strings.HasPrefix(l, "macos") && (strings.Contains(l, "large") || strings.Contains(l, "xlarge")) {
			r.Large = true
		}
	}
	return r
}

// Rate returns the per-minute rate for a job's labels and whether a rate is known.
func (t Table) Rate(labels []string) (rate float64, known bool) {
	for _, l := range labels {
		if v, ok := t.Overrides[strings.ToLower(strings.TrimSpace(l))]; ok {
			return v, true
		}
	}
	r := Identify(labels)
	if r.SelfHosted {
		return t.SelfHosted, true
	}
	switch r.OS {
	case "linux":
		return t.Linux * coreScale(r.Cores), true
	case "windows":
		return t.Windows * coreScale(r.Cores), true
	case "macos":
		if r.Large {
			return 0, false
		}
		return t.MacOS, true
	}
	return 0, false
}

// coreScale estimates larger-runner prices as linear in core count relative
// to the standard 2-core runner.
func coreScale(cores int) float64 {
	if cores <= 2 {
		return 1
	}
	return float64(cores) / 2
}

// BillableMinutes is the job duration rounded up to a whole minute, the way
// GitHub bills each job. Jobs that never started bill nothing.
func BillableMinutes(started, completed *time.Time) int {
	if started == nil || completed == nil || started.IsZero() || completed.IsZero() {
		return 0
	}
	d := completed.Sub(*started)
	if d <= 0 {
		return 0
	}
	return int(math.Ceil(d.Minutes()))
}

// PrimaryLabel picks a stable display label for a job's runner.
func PrimaryLabel(labels []string) string {
	if len(labels) == 0 {
		return "(no labels)"
	}
	ls := append([]string(nil), labels...)
	sort.Strings(ls)
	return strings.Join(ls, ",")
}
