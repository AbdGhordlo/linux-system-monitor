package main

import (
	"flag" // for command-line flag parsing
	"fmt"
	"os"        // for file and OS operations
	"os/signal" // for handling OS signals like Ctrl+C
	"strings"
	"syscall" // for low-level OS interactions, including signal handling
	"time"

	// import the stats package for reading system metrics
	"github.com/AbdGhordlo/linux-system-monitor/internal/stats"
)

// ANSI escape codes for terminal control sequences.
const (
	clearScreen = "\033[2J"
	cursorHome  = "\033[H"
	hideCursor  = "\033[?25l"
	showCursor  = "\033[?25h"
)

func main() {
	// Parse command-line flags for refresh interval and filesystem path.
	// "interval" specifies how often the display refreshes
	interval := flag.Duration("interval", 2*time.Second, "refresh interval")
	// "path" specifies the filesystem path to report disk usage for
	mountPath := flag.String("path", "/", "filesystem path to report disk usage for")
	flag.Parse()

	if err := run(*interval, *mountPath); err != nil {
		fmt.Fprintln(os.Stderr, "sysmon:", err)
		os.Exit(1)
	}
}

func run(interval time.Duration, mountPath string) error {
	// Hide the cursor while the program is running, and restore it on exit.
	fmt.Print(hideCursor)
	defer fmt.Print(showCursor)

	// Intercept the Ctrl+C signal so that we can restore the cursor before exiting.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Print(showCursor)
		os.Exit(0)
	}()

	// Read the initial CPU and network stats to compute deltas on each refresh.
	prevCPU, err := stats.ReadCPUTimes()
	if err != nil {
		return err
	}
	prevNet, err := stats.ReadNetIO()
	if err != nil {
		return err
	}
	lastSample := time.Now()

	// Create a ticker that triggers at the specified interval, and enter the main loop.
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		// Wait for the next tick before taking the first sample.x
		<-ticker.C

		// Read the current CPU, memory, disk, and network stats.
		curCPU, err := stats.ReadCPUTimes()
		if err != nil {
			return err
		}
		mem, err := stats.ReadMemInfo()
		if err != nil {
			return err
		}
		diskUsage, err := stats.ReadDiskUsage(mountPath)
		if err != nil {
			return err
		}
		curNet, err := stats.ReadNetIO()
		if err != nil {
			return err
		}

		// Compute the elapsed time since the last sample, and calculate CPU percentages and network throughput rates.
		now := time.Now()
		elapsed := now.Sub(lastSample).Seconds()
		cpuPct := stats.CPUPercent(prevCPU, curCPU)
		netRates := stats.ComputeNetThroughput(prevNet, curNet, elapsed)

		// Render the updated stats to the terminal.
		render(cpuPct, mem, diskUsage, netRates)

		// Update the previous stats and timestamp for the next iteration.
		prevCPU, prevNet, lastSample = curCPU, curNet, now
	}
}

// Renders the system stats to the terminal.
func render(cpuPct map[string]float64, mem stats.MemInfo, disk stats.DiskUsage, net map[string]stats.NetThroughput) {
	var b strings.Builder

	// Erase the terminal screen and move cursor to top left.
	b.WriteString(clearScreen + cursorHome)

	// Title
	b.WriteString("sysmon — live system monitor (Ctrl+C to quit)\n")

	// Divider
	b.WriteString(strings.Repeat("─", 48))
	b.WriteString("\n\n")

	// CPU section
	// Use the "comma ok" idiom to safely read the "cpu" entry.
	if pct, ok := cpuPct["cpu"]; ok {
		// &b lets Fprintf write into the Builder instead of the terminal.
		fmt.Fprintf(&b, "CPU    %s\n", bar(pct))
	}

	// Memory section
	fmt.Fprintf(&b, "Memory %s  (%s / %s)\n",
		bar(mem.UsedPercent()), humanKB(mem.UsedKB()), humanKB(mem.TotalKB))

	// Display swap usage only if swap is available.
	if mem.SwapTotalKB > 0 {
		swapPct := float64(mem.SwapUsedKB()) / float64(mem.SwapTotalKB) * 100
		fmt.Fprintf(&b, "Swap   %s  (%s / %s)\n",
			bar(swapPct), humanKB(mem.SwapUsedKB()), humanKB(mem.SwapTotalKB))
	}

	// Disk section
	fmt.Fprintf(&b, "Disk   %s  (%s / %s) [%s]\n",
		bar(disk.UsedPercent()), humanBytes(disk.UsedBytes()), humanBytes(disk.TotalBytes), disk.Path)

	// Network section
	b.WriteString("\nNetwork:\n")
	for iface, rate := range net {
		if iface == "lo" {
			continue // loopback is rarely interesting in a live view
		}
		fmt.Fprintf(&b, "  %-10s ↓ %-12s ↑ %s\n", iface, humanBpsRate(rate.RxBps), humanBpsRate(rate.TxBps))
	}

	// Final print
	fmt.Print(b.String())
}

// bar renders a fixed-width ASCII percentage bar, e.g. "[████------]  42%".
func bar(pct float64) string {
	const width = 20
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct / 100 * width)
	return fmt.Sprintf("[%s%s] %5.1f%%",
		strings.Repeat("█", filled), strings.Repeat("-", width-filled), pct)
}

// Wrapper function for converting kilobytes to bytes because the humanBytes function expects bytes as input.
func humanKB(kb uint64) string {
	return humanBytes(kb * 1024)
}

// Formats a byte count into a human-readable string with binary prefixes (KiB, MiB, GiB, etc.).
func humanBytes(b uint64) string {
	const unit = 1024
	// If the byte count is less than 1024, return it as bytes.
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}

	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// Wrapper function for formatting bytes per second into a human-readable string with binary prefixes.
func humanBpsRate(bps float64) string {
	return humanBytes(uint64(bps)) + "/s"
}
