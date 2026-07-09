package stats_test

import (
	"strings"
	"testing"

	"github.com/AbdGhordlo/linux-system-monitor/internal/stats"
)

// TestParseMemInfoModernKernel verifies that ParseMemInfo correctly parses
// memory statistics from a modern /proc/meminfo file containing the
// MemAvailable field.
func TestParseMemInfoModernKernel(t *testing.T) {
	f := openFixture(t, "testdata/proc_meminfo")
	defer f.Close()

	mi, err := stats.ParseMemInfo(f)
	if err != nil {
		t.Fatalf("ParseMemInfo: %v", err)
	}

	if mi.TotalKB != 16384000 {
		t.Errorf("TotalKB: want 16384000, got %d", mi.TotalKB)
	}
	if mi.AvailableKB != 8000000 {
		t.Errorf("AvailableKB: want 8000000, got %d", mi.AvailableKB)
	}
	if mi.SwapTotalKB != 4096000 {
		t.Errorf("SwapTotalKB: want 4096000, got %d", mi.SwapTotalKB)
	}
	if mi.SwapUsedKB() != 1096000 {
		t.Errorf("SwapUsedKB(): want 1096000, got %d", mi.SwapUsedKB())
	}
}

// TestParseMemInfoOldKernelFallback verifies that ParseMemInfo correctly
// estimates MemAvailable on older kernels that do not expose the
// MemAvailable field.
func TestParseMemInfoOldKernelFallback(t *testing.T) {
	// Pre-3.14 kernels don't expose MemAvailable; the parser should fall
	// back to Free + Buffers + Cached.
	f := openFixture(t, "testdata/proc_meminfo_old_kernel")
	defer f.Close()

	mi, err := stats.ParseMemInfo(f)
	if err != nil {
		t.Fatalf("ParseMemInfo (old kernel): %v", err)
	}

	// MemAvailable not in file → should be 1000000 + 200000 + 2000000 = 3200000
	wantAvail := uint64(3200000)
	if mi.AvailableKB != wantAvail {
		t.Errorf("AvailableKB fallback: want %d, got %d", wantAvail, mi.AvailableKB)
	}
}

// TestMemInfoUsedPercent verifies that UsedPercent returns a reasonable
// memory utilization percentage based on the parsed memory statistics.
func TestMemInfoUsedPercent(t *testing.T) {
	f := openFixture(t, "testdata/proc_meminfo")
	defer f.Close()

	mi, _ := stats.ParseMemInfo(f)
	pct := mi.UsedPercent()

	// Used = 16384000 - 8000000 = 8384000; pct = 8384000/16384000 ≈ 51.17%
	if pct < 50 || pct > 55 {
		t.Errorf("UsedPercent: expected ~51%%, got %.2f%%", pct)
	}
}

// TestMemInfoZeroTotal verifies that UsedPercent safely returns 0 when the
// total memory is zero, avoiding a divide-by-zero calculation.
func TestMemInfoZeroTotal(t *testing.T) {
	// Edge case: completely empty or zero file should not divide by zero.
	mi := stats.MemInfo{}
	if mi.UsedPercent() != 0 {
		t.Errorf("UsedPercent with zero TotalKB should return 0, got %.2f", mi.UsedPercent())
	}
}

// TestParseMemInfoMalformedLine verifies that ParseMemInfo returns an error
// when a numeric field contains invalid data instead of silently accepting it.
func TestParseMemInfoMalformedLine(t *testing.T) {
	// A file with a numeric parse error should return an error, not silently
	// produce wrong results.
	bad := "MemTotal: not_a_number kB\n"
	_, err := stats.ParseMemInfo(strings.NewReader(bad))
	if err == nil {
		t.Error("expected error for malformed numeric field, got nil")
	}
}
