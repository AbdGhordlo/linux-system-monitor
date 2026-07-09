// Package stats provides collectors that read live system metrics from
// Linux kernel interfaces such as the /proc filesystem and statfs.
package stats

import (
	"bufio" // Used to read /proc/stat line by line using a Scanner
	"fmt"
	"io" // Defines io.Reader so ParseCPUTimes can read from any source (files, buffers, etc.)
	"os" // Used to open /proc/stat
	"strconv"
	"strings"
)

// This represents one line of /proc/stat, i.e., it holds the raw jiffie (scheduler tick)
// counters for a single CPU line from /proc/stat.
type CPUTimes struct {
	// "cpu" shows the total combined stats for all cores;
	// "cpu0", "cpu1", etc., track each individual core.
	Name string

	User      uint64 // Time spent in user space (normal priority)
	Nice      uint64 // Time spent in user space with low priority (niced)
	System    uint64 // Time spent in kernel space
	Idle      uint64 // Time spent doing nothing
	IOWait    uint64 // Time spent waiting for I/O operations to complete
	IRQ       uint64 // Time spent servicing hardware interrupts
	SoftIRQ   uint64 // Time spent servicing software interrupts
	Steal     uint64 // Time stolen by the hypervisor (in virtualized environments)
	Guest     uint64 // Time spent running a virtual CPU for guest operating systems
	GuestNice uint64 // Time spent running a low-priority guest operating system
}

// Total returns the sum of every counted jiffie, used as the denominator
// when computing a percentage.
func (c CPUTimes) Total() uint64 {
	// Notice Guest and GuestNice aren't included because they're already included inside User/Nice.
	return c.User + c.Nice + c.System + c.Idle + c.IOWait +
		c.IRQ + c.SoftIRQ + c.Steal
}

// IdleTotal returns the jiffies considered "idle" (idle + iowait),
// matching the convention used by tools like top and mpstat.
func (c CPUTimes) IdleTotal() uint64 {
	return c.Idle + c.IOWait
}

// ReadCPUTimes parses /proc/stat and returns one CPUTimes entry per line
// that starts with "cpu" (the aggregate line plus one line per core).
func ReadCPUTimes() ([]CPUTimes, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return nil, fmt.Errorf("opening /proc/stat: %w", err)
	}
	defer f.Close()

	return ParseCPUTimes(f)
}

// ParseCPUTimes is the testable core; it accepts any io.Reader so unit
// tests can feed fixture files without requiring the live /proc filesystem.
func ParseCPUTimes(r io.Reader) ([]CPUTimes, error) {
	var result []CPUTimes
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()

		// Ignore non-CPU lines
		if !strings.HasPrefix(line, "cpu") {
			continue
		}

		// Split the line into fields
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue // malformed or unexpectedly short line — skip defensively
		}

		vals := make([]uint64, 0, len(fields)-1)

		// The first field in each line is the CPU name, which we ignore by starting from index 1.
		for _, fld := range fields[1:] {
			// Convert the string to a uint64
			v, err := strconv.ParseUint(fld, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parsing /proc/stat field %q: %w", fld, err)
			}
			vals = append(vals, v)
		}

		// Create a CPUTimes struct from the parsed values
		ct := CPUTimes{Name: fields[0]}

		// Guard against kernels that omit the trailing guest/guest_nice
		// columns (older kernels only expose the first 8 fields).
		assign := []*uint64{
			&ct.User, &ct.Nice, &ct.System, &ct.Idle,
			&ct.IOWait, &ct.IRQ, &ct.SoftIRQ, &ct.Steal,
			&ct.Guest, &ct.GuestNice,
		}
		for i, v := range vals {
			if i >= len(assign) {
				break
			}
			*assign[i] = v
		}

		result = append(result, ct)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning /proc/stat: %w", err)
	}

	return result, nil
}

// CPUPercent computes the busy percentage for each CPU line between two
// samples taken a known interval apart. prev and cur must be slices
// returned by ReadCPUTimes, in matching order.
func CPUPercent(prev, cur []CPUTimes) map[string]float64 {

	// Create a lookup map from the previous sample for efficient access by CPU name.
	prevByName := make(map[string]CPUTimes, len(prev))
	for _, p := range prev {
		prevByName[p.Name] = p
	}

	// Create a result map to hold the computed percentages for each CPU line.
	result := make(map[string]float64, len(cur))

	// For each CPU line in the current sample, compute the busy percentage based on the previous sample.
	for _, c := range cur {
		p, ok := prevByName[c.Name]
		if !ok {
			continue
		}

		totalDelta := float64(c.Total() - p.Total())
		idleDelta := float64(c.IdleTotal() - p.IdleTotal())

		if totalDelta <= 0 {
			result[c.Name] = 0
			continue
		}

		busy := totalDelta - idleDelta
		result[c.Name] = (busy / totalDelta) * 100
	}

	return result
}
