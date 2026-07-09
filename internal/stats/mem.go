// Package stats provides collectors that read live system metrics from
// Linux kernel interfaces such as the /proc filesystem and statfs.
package stats

import (
	"bufio" // Used to read /proc/meminfo line by line using a Scanner
	"fmt"
	"io" // Provides io.Reader for parsing from any input source
	"os" // Used to open /proc/meminfo
	"strconv"
	"strings"
)

// MemInfo holds the subset of /proc/meminfo fields useful for a
// system monitor display. All values are in kibibytes (Kilobinary bytes),
// matching the kernel's native units in that file.
type MemInfo struct {
	// Total usable RAM (excluding the tiny fraction reserved by the kernel binary/BIOS).
	TotalKB uint64
	// Raw unused RAM that is completely empty and sitting idle.
	FreeKB uint64
	// Estimate of how much memory is actually available to start new applications
	// without causing the system to swap. (Calculated by the kernel as Free + reclaimable Cache/Buffers).
	AvailableKB uint64
	// Temporary storage for raw disk blocks (mostly filesystem metadata like directory structures).
	BuffersKB uint64
	// Page cache for files read from disk. The kernel keeps recently used file data
	// in RAM here to speed up performance, but will instantly reclaim it if applications need RAM.
	CachedKB uint64
	// Total capacity of the configured disk swap space (swap files or partitions).
	SwapTotalKB uint64
	// Amount of swap space on the disk that is currently empty and available for use.
	SwapFreeKB uint64
}

// UsedKB returns memory considered "in use" by applications, using
// MemAvailable rather than MemFree so buffer/cache pages reclaimable
// by the kernel aren't counted as used (this matches what `free -h`
// reports as "used").
func (m MemInfo) UsedKB() uint64 {
	if m.AvailableKB > m.TotalKB {
		return 0
	}
	return m.TotalKB - m.AvailableKB
}

// UsedPercent returns the percentage of total memory in use.
func (m MemInfo) UsedPercent() float64 {
	if m.TotalKB == 0 {
		return 0
	}
	return (float64(m.UsedKB()) / float64(m.TotalKB)) * 100
}

// SwapUsedKB returns swap space currently in use.
func (m MemInfo) SwapUsedKB() uint64 {
	if m.SwapFreeKB > m.SwapTotalKB {
		return 0
	}
	return m.SwapTotalKB - m.SwapFreeKB
}

// ReadMemInfo parses /proc/meminfo into a MemInfo struct.
func ReadMemInfo() (MemInfo, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return MemInfo{}, fmt.Errorf("opening /proc/meminfo: %w", err)
	}
	defer f.Close()

	return ParseMemInfo(f)
}

// ParseMemInfo is the testable core; accepts any io.Reader.
func ParseMemInfo(r io.Reader) (MemInfo, error) {
	targets := map[string]*uint64{}
	var mi MemInfo
	targets["MemTotal"] = &mi.TotalKB
	targets["MemFree"] = &mi.FreeKB
	targets["MemAvailable"] = &mi.AvailableKB
	targets["Buffers"] = &mi.BuffersKB
	targets["Cached"] = &mi.CachedKB
	targets["SwapTotal"] = &mi.SwapTotalKB
	targets["SwapFree"] = &mi.SwapFreeKB

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}

		key := line[:colon]
		target, ok := targets[key]
		if !ok {
			continue // not a field we care about
		}

		valuePart := strings.TrimSpace(line[colon+1:])
		valuePart = strings.TrimSuffix(valuePart, " kB")
		valuePart = strings.TrimSpace(valuePart)

		v, err := strconv.ParseUint(valuePart, 10, 64)
		if err != nil {
			return MemInfo{}, fmt.Errorf("parsing /proc/meminfo line %q: %w", line, err)
		}
		*target = v
	}

	if err := scanner.Err(); err != nil {
		return MemInfo{}, fmt.Errorf("scanning /proc/meminfo: %w", err)
	}

	// Older kernels (pre-3.14) don't expose MemAvailable; fall back to
	// a conservative approximation so the percentage stays meaningful.
	if mi.AvailableKB == 0 {
		mi.AvailableKB = mi.FreeKB + mi.BuffersKB + mi.CachedKB
	}

	return mi, nil
}
