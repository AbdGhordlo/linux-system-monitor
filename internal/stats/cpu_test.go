package stats_test

import (
	"os"
	"testing"

	"github.com/AbdGhordlo/linux-system-monitor/internal/stats"
)

// TestParseCPUTimesAggregateCount verifies that ParseCPUTimes returns the
// expected number of CPU entries and that the first entry is the aggregate
// "cpu" line.
func TestParseCPUTimesAggregateCount(t *testing.T) {
	f := openFixture(t, "testdata/proc_stat_a")
	defer f.Close()

	times, err := stats.ParseCPUTimes(f)
	if err != nil {
		t.Fatalf("ParseCPUTimes returned unexpected error: %v", err)
	}

	// fixture has 1 aggregate + 4 per-core lines
	if len(times) != 5 {
		t.Fatalf("expected 5 CPU entries, got %d", len(times))
	}
	if times[0].Name != "cpu" {
		t.Errorf("expected first entry to be aggregate 'cpu', got %q", times[0].Name)
	}
}

// TestParseCPUTimesFieldValues verifies that the parser correctly maps
// jiffy values from /proc/stat into the corresponding CPUTimes fields.
func TestParseCPUTimesFieldValues(t *testing.T) {
	f := openFixture(t, "testdata/proc_stat_a")
	defer f.Close()

	times, err := stats.ParseCPUTimes(f)
	if err != nil {
		t.Fatalf("ParseCPUTimes: %v", err)
	}

	agg := times[0]
	if agg.User != 1000 {
		t.Errorf("aggregate User: want 1000, got %d", agg.User)
	}
	if agg.Idle != 8000 {
		t.Errorf("aggregate Idle: want 8000, got %d", agg.Idle)
	}
	if agg.IOWait != 100 {
		t.Errorf("aggregate IOWait: want 100, got %d", agg.IOWait)
	}
}

// TestCPUPercentReasonableRange verifies that CPUPercent computes a sensible
// utilization value between two snapshots and matches the expected percentage
// for the provided test fixtures.
func TestCPUPercentReasonableRange(t *testing.T) {
	fa := openFixture(t, "testdata/proc_stat_a")
	defer fa.Close()
	fb := openFixture(t, "testdata/proc_stat_b")
	defer fb.Close()

	prev, err := stats.ParseCPUTimes(fa)
	if err != nil {
		t.Fatalf("ParseCPUTimes(a): %v", err)
	}
	cur, err := stats.ParseCPUTimes(fb)
	if err != nil {
		t.Fatalf("ParseCPUTimes(b): %v", err)
	}

	pct := stats.CPUPercent(prev, cur)

	aggPct, ok := pct["cpu"]
	if !ok {
		t.Fatal("CPUPercent missing aggregate 'cpu' key")
	}
	if aggPct < 0 || aggPct > 100 {
		t.Errorf("aggregate CPU%%: want 0-100, got %.2f", aggPct)
	}

	// Between the two fixtures the idle delta is 200 and total delta is
	// (1200+50+400+8200+100+30+15) - (1000+50+300+8000+100+20+10) = 515
	// busy = 515 - 200 = 315  →  315/515 ≈ 61.2%
	const wantApprox = 61.2
	const tolerance = 0.5
	if aggPct < wantApprox-tolerance || aggPct > wantApprox+tolerance {
		t.Errorf("aggregate CPU%%: want ~%.1f±%.1f, got %.2f", wantApprox, tolerance, aggPct)
	}
}

// TestCPUPercentIdenticalSamples verifies that identical CPU samples produce
// 0% utilization rather than causing a divide-by-zero or invalid result.
func TestCPUPercentIdenticalSamples(t *testing.T) {
	f := openFixture(t, "testdata/proc_stat_a")
	defer f.Close()

	times, _ := stats.ParseCPUTimes(f)
	pct := stats.CPUPercent(times, times) // zero delta → should return 0, not divide-by-zero

	for name, p := range pct {
		if p != 0 {
			t.Errorf("%s: expected 0%% for identical samples, got %.2f", name, p)
		}
	}
}

// openFixture is a test helper that opens a testdata file. Calling t.Helper()
// marks this function as a helper so that any test failures are reported at
// the calling test rather than inside this helper itself.
func openFixture(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("could not open fixture %q: %v", path, err)
	}
	return f
}
