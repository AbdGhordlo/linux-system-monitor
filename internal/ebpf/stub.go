//go:build !ebpf || !linux

// This stub is compiled when the "ebpf" build tag is absent or on non-Linux
// systems. It exports the same types as loader.go so callers in main.go can
// reference them unconditionally; all operations return a "not compiled in"
// error so users get a clear message rather than a missing-symbol panic.

package ebpf

import "fmt"

// Collector is the stub version — it holds no BPF state.
type Collector struct{}

// LatencyBucket mirrors the real type so main.go compiles without build tags.
type LatencyBucket struct {
	Label string
	Count uint64
}

// NewCollector always returns an error explaining how to enable eBPF support.
func NewCollector() (*Collector, error) {
	return nil, fmt.Errorf(
		"eBPF support not compiled in — rebuild with: go build -tags ebpf ./cmd/sysmon\n" +
			"  Also requires: Linux 5.8+, root/CAP_BPF, and running `go generate ./internal/ebpf/...` first",
	)
}

func (c *Collector) ReadHistogram() ([]LatencyBucket, error) {
	return nil, fmt.Errorf("eBPF support not compiled in")
}

func (c *Collector) ResetHistogram() error { return nil }
func (c *Collector) Close()                {}

// RenderASCII is a no-op stub so callers don't need build tags.
func RenderASCII(_ []LatencyBucket) string {
	return "  (eBPF not compiled in — rebuild with -tags ebpf)\n"
}
