package stats_test

import (
	"testing"

	"github.com/AbdGhordlo/linux-system-monitor/internal/stats"
)

// TestParseDiskIOFiltersPartitions verifies that ParseDiskIO keeps only
// top-level block devices and correctly filters out partitions and virtual
// devices such as loop and device-mapper entries.
func TestParseDiskIOFiltersPartitions(t *testing.T) {
	f := openFixture(t, "testdata/proc_diskstats")
	defer f.Close()

	disks, err := stats.ParseDiskIO(f)
	if err != nil {
		t.Fatalf("ParseDiskIO: %v", err)
	}

	// Fixture contains: sda (whole), sda1 (part), sdb (whole),
	// dm-0 (virtual), loop0 (virtual), nvme0n1 (whole), nvme0n1p1 (part).
	// Only sda, sdb, and nvme0n1 should survive filtering.
	want := map[string]bool{"sda": true, "sdb": true, "nvme0n1": true}
	if len(disks) != len(want) {
		t.Errorf("expected %d whole-disk entries, got %d", len(want), len(disks))
	}
	for _, d := range disks {
		if !want[d.Device] {
			t.Errorf("unexpected device in results: %q", d.Device)
		}
	}
}

// TestParseDiskIOSectorValues verifies that ParseDiskIO correctly parses
// sector counts and that the ReadBytes helper converts sectors to bytes.
func TestParseDiskIOSectorValues(t *testing.T) {
	f := openFixture(t, "testdata/proc_diskstats")
	defer f.Close()

	disks, _ := stats.ParseDiskIO(f)

	var sda *stats.DiskIO
	for i := range disks {
		if disks[i].Device == "sda" {
			sda = &disks[i]
		}
	}
	if sda == nil {
		t.Fatal("sda not found in results")
	}

	if sda.ReadSectors != 50000 {
		t.Errorf("sda ReadSectors: want 50000, got %d", sda.ReadSectors)
	}
	if sda.WriteSectors != 20000 {
		t.Errorf("sda WriteSectors: want 20000, got %d", sda.WriteSectors)
	}
	// ReadBytes should be sectors * 512
	if sda.ReadBytes() != 50000*512 {
		t.Errorf("sda ReadBytes(): want %d, got %d", 50000*512, sda.ReadBytes())
	}
}

// TestDiskUsageCalculations verifies that the UsedBytes and UsedPercent
// helper methods return the expected values for a known disk capacity.
func TestDiskUsageCalculations(t *testing.T) {
	d := stats.DiskUsage{
		Path:       "/",
		TotalBytes: 100 * 1024 * 1024 * 1024, // 100 GiB
		FreeBytes:  40 * 1024 * 1024 * 1024,  // 40 GiB free
	}

	// UsedBytes = 60 GiB
	want := uint64(60 * 1024 * 1024 * 1024)
	if d.UsedBytes() != want {
		t.Errorf("UsedBytes: want %d, got %d", want, d.UsedBytes())
	}

	// UsedPercent = 60%
	if d.UsedPercent() != 60.0 {
		t.Errorf("UsedPercent: want 60.0, got %.2f", d.UsedPercent())
	}
}

// TestDiskUsageZeroTotal verifies that UsedPercent safely returns 0 when
// the total disk capacity is zero, avoiding a divide-by-zero calculation.
func TestDiskUsageZeroTotal(t *testing.T) {
	d := stats.DiskUsage{}
	if d.UsedPercent() != 0 {
		t.Errorf("UsedPercent with zero total: want 0, got %.2f", d.UsedPercent())
	}
}
