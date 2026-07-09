// Package stats provides collectors that read live system metrics from
// Linux kernel interfaces such as the /proc filesystem and statfs.
package stats

import (
	"bufio" // Used to read /proc/net/dev line by line using a Scanner
	"fmt"
	"io" // Provides io.Reader for parsing from any input source
	"os" // Used to open /proc/net/dev
	"strconv"
	"strings"
)

// NetIO holds cumulative byte/packet counters for a single network
// interface, taken from /proc/net/dev.
type NetIO struct {
	Interface string
	RxBytes   uint64
	RxPackets uint64
	TxBytes   uint64
	TxPackets uint64
}

// ReadNetIO parses /proc/net/dev. The loopback interface is included
// since callers may want to filter it out themselves depending on context.
func ReadNetIO() ([]NetIO, error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return nil, fmt.Errorf("opening /proc/net/dev: %w", err)
	}
	defer f.Close()

	return ParseNetIO(f)
}

// ParseNetIO is the testable core; accepts any io.Reader.
func ParseNetIO(r io.Reader) ([]NetIO, error) {
	var result []NetIO
	scanner := bufio.NewScanner(r)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum <= 2 {
			continue // skip the two header lines
		}

		line := scanner.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}

		iface := strings.TrimSpace(line[:colon])
		fields := strings.Fields(line[colon+1:])

		// Layout after the colon: rx_bytes rx_packets rx_errs rx_drop
		// rx_fifo rx_frame rx_compressed rx_multicast tx_bytes tx_packets ...
		if len(fields) < 10 {
			continue
		}

		rxBytes, err1 := strconv.ParseUint(fields[0], 10, 64)
		rxPackets, err2 := strconv.ParseUint(fields[1], 10, 64)
		txBytes, err3 := strconv.ParseUint(fields[8], 10, 64)
		txPackets, err4 := strconv.ParseUint(fields[9], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			continue
		}

		result = append(result, NetIO{
			Interface: iface,
			RxBytes:   rxBytes,
			RxPackets: rxPackets,
			TxBytes:   txBytes,
			TxPackets: txPackets,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning /proc/net/dev: %w", err)
	}

	return result, nil
}

// NetThroughput holds the per-second rates derived from two NetIO samples.
type NetThroughput struct {
	RxBps float64
	TxBps float64
}

// ComputeNetThroughput returns bytes/sec for each interface present in both
// samples. elapsedSeconds is the wall-clock gap between the two reads.
func ComputeNetThroughput(prev, cur []NetIO, elapsedSeconds float64) map[string]NetThroughput {
	prevByName := make(map[string]NetIO, len(prev))
	for _, p := range prev {
		prevByName[p.Interface] = p
	}

	result := make(map[string]NetThroughput, len(cur))
	if elapsedSeconds <= 0 {
		return result
	}

	for _, c := range cur {
		p, ok := prevByName[c.Interface]
		if !ok {
			continue
		}

		// Counters can wrap or reset (interface replaced); guard against
		// a negative delta producing a nonsensical huge uint64 underflow.
		var rxDelta, txDelta uint64
		if c.RxBytes >= p.RxBytes {
			rxDelta = c.RxBytes - p.RxBytes
		}
		if c.TxBytes >= p.TxBytes {
			txDelta = c.TxBytes - p.TxBytes
		}

		result[c.Interface] = NetThroughput{
			RxBps: float64(rxDelta) / elapsedSeconds,
			TxBps: float64(txDelta) / elapsedSeconds,
		}
	}

	return result
}
