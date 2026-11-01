package pricing

import (
	"math"
	"testing"
	"time"
)

func TestRate(t *testing.T) {
	tb := Default()
	tb.SelfHosted = 0.001
	tb.Overrides["gpu-t4-4-core"] = 0.07
	tests := []struct {
		labels []string
		want   float64
		known  bool
	}{
		{[]string{"ubuntu-latest"}, LinuxRate, true},
		{[]string{"ubuntu-24.04"}, LinuxRate, true},
		{[]string{"ubuntu-24.04-arm"}, LinuxRate, true},
		{[]string{"windows-latest"}, WindowsRate, true},
		{[]string{"macos-14"}, MacOSRate, true},
		{[]string{"macos-15-xlarge"}, 0, false},
		{[]string{"ubuntu-latest-8-cores"}, LinuxRate * 4, true},
		{[]string{"linux-16core"}, LinuxRate * 8, true},
		{[]string{"windows-latest-4-cores"}, WindowsRate * 2, true},
		{[]string{"self-hosted", "linux", "x64"}, 0.001, true},
		{[]string{"GPU-T4-4-Core"}, 0.07, true},
		{[]string{"my-custom-runner"}, 0, false},
		{nil, 0, false},
	}
	for _, tt := range tests {
		got, known := tb.Rate(tt.labels)
		if known != tt.known || math.Abs(got-tt.want) > 1e-12 {
			t.Errorf("Rate(%v) = %v, %v; want %v, %v", tt.labels, got, known, tt.want, tt.known)
		}
	}
}

func TestBillableMinutes(t *testing.T) {
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { t := base.Add(d); return &t }
	tests := []struct {
		start, end *time.Time
		want       int
	}{
		{at(0), at(time.Second), 1},
		{at(0), at(60 * time.Second), 1},
		{at(0), at(61 * time.Second), 2},
		{at(0), at(10*time.Minute + 30*time.Second), 11},
		{at(0), at(0), 0},
		{at(time.Minute), at(0), 0},
		{nil, at(0), 0},
		{at(0), nil, 0},
	}
	for _, tt := range tests {
		if got := BillableMinutes(tt.start, tt.end); got != tt.want {
			t.Errorf("BillableMinutes(%v, %v) = %d, want %d", tt.start, tt.end, got, tt.want)
		}
	}
}

func TestParseOverride(t *testing.T) {
	label, rate, err := ParseOverride("macos-15-xlarge=$0.10")
	if err != nil || label != "macos-15-xlarge" || rate != 0.10 {
		t.Fatalf("got %q %v %v", label, rate, err)
	}
	for _, bad := range []string{"nolabel", "=0.1", "x=abc", "x=-1", "x=NaN"} {
		if _, _, err := ParseOverride(bad); err == nil {
			t.Errorf("ParseOverride(%q) should fail", bad)
		}
	}
}

func TestIdentify(t *testing.T) {
	r := Identify([]string{"ubuntu-24.04-arm-4-cores"})
	if r.OS != "linux" || !r.ARM || r.Cores != 4 || r.SelfHosted {
		t.Fatalf("got %+v", r)
	}
	if PrimaryLabel([]string{"x64", "self-hosted", "linux"}) != "linux,self-hosted,x64" {
		t.Fatal("PrimaryLabel should sort labels")
	}
}
