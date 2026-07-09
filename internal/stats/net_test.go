package stats_test

import (
	"testing"

	"github.com/AbdGhordlo/linux-system-monitor/internal/stats"
)

// TestParseNetIOInterfaceCount verifies that ParseNetIO parses every
// network interface present in the fixture file.
func TestParseNetIOInterfaceCount(t *testing.T) {
	f := openFixture(t, "testdata/proc_net_dev")
	defer f.Close()

	ifaces, err := stats.ParseNetIO(f)
	if err != nil {
		t.Fatalf("ParseNetIO: %v", err)
	}

	// fixture has lo, eth0, eth1, wlan0 — all 4 should be parsed
	if len(ifaces) != 4 {
		t.Fatalf("expected 4 interfaces, got %d", len(ifaces))
	}
}

// TestParseNetIOValues verifies that ParseNetIO correctly parses the byte
// and packet counters for a network interface.
func TestParseNetIOValues(t *testing.T) {
	f := openFixture(t, "testdata/proc_net_dev")
	defer f.Close()

	ifaces, _ := stats.ParseNetIO(f)

	var eth0 *stats.NetIO
	for i := range ifaces {
		if ifaces[i].Interface == "eth0" {
			eth0 = &ifaces[i]
		}
	}
	if eth0 == nil {
		t.Fatal("eth0 not found in parsed interfaces")
	}

	if eth0.RxBytes != 5000000 {
		t.Errorf("eth0 RxBytes: want 5000000, got %d", eth0.RxBytes)
	}
	if eth0.TxBytes != 2000000 {
		t.Errorf("eth0 TxBytes: want 2000000, got %d", eth0.TxBytes)
	}
	if eth0.RxPackets != 50000 {
		t.Errorf("eth0 RxPackets: want 50000, got %d", eth0.RxPackets)
	}
}

// TestComputeNetThroughput verifies that ComputeNetThroughput correctly
// calculates receive and transmit rates from two interface snapshots.
func TestComputeNetThroughput(t *testing.T) {
	prev := []stats.NetIO{
		{Interface: "eth0", RxBytes: 1000, TxBytes: 500},
	}
	cur := []stats.NetIO{
		{Interface: "eth0", RxBytes: 3000, TxBytes: 1500},
	}

	// Over 2 seconds: Rx gained 2000 bytes → 1000 B/s; Tx gained 1000 → 500 B/s
	rates := stats.ComputeNetThroughput(prev, cur, 2.0)

	r, ok := rates["eth0"]
	if !ok {
		t.Fatal("eth0 missing from throughput map")
	}
	if r.RxBps != 1000 {
		t.Errorf("eth0 RxBps: want 1000, got %.1f", r.RxBps)
	}
	if r.TxBps != 500 {
		t.Errorf("eth0 TxBps: want 500, got %.1f", r.TxBps)
	}
}

// TestComputeNetThroughputCounterWrap verifies that ComputeNetThroughput
// safely handles counter resets or wraps without producing invalid rates.
func TestComputeNetThroughputCounterWrap(t *testing.T) {
	// Simulate a counter reset (cur < prev); result should be 0, not a
	// massive uint64 underflow.
	prev := []stats.NetIO{{Interface: "eth0", RxBytes: 9999, TxBytes: 9999}}
	cur := []stats.NetIO{{Interface: "eth0", RxBytes: 100, TxBytes: 100}}

	rates := stats.ComputeNetThroughput(prev, cur, 1.0)
	r := rates["eth0"]
	if r.RxBps != 0 || r.TxBps != 0 {
		t.Errorf("counter wrap: expected 0 rates, got Rx=%.1f Tx=%.1f", r.RxBps, r.TxBps)
	}
}

// TestComputeNetThroughputZeroElapsed verifies that ComputeNetThroughput
// returns an empty result when the elapsed time is zero, avoiding a
// divide-by-zero calculation.
func TestComputeNetThroughputZeroElapsed(t *testing.T) {
	prev := []stats.NetIO{{Interface: "eth0", RxBytes: 1000}}
	cur := []stats.NetIO{{Interface: "eth0", RxBytes: 2000}}

	// Zero elapsed time → should return empty map rather than divide by zero
	rates := stats.ComputeNetThroughput(prev, cur, 0)
	if len(rates) != 0 {
		t.Errorf("expected empty result for zero elapsed, got %d entries", len(rates))
	}
}
