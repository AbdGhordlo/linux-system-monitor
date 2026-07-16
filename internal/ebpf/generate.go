// Package ebpf provides an optional eBPF-based I/O latency collector that
// uses kernel tracepoints to measure block request round-trip times without
// any polling overhead.
//
// This package is only compiled when the "ebpf" build tag is provided:
//
//	go build -tags ebpf ./cmd/sysmon
//
// # Generating the BPF bytecode
//
// Before building with -tags ebpf you must run:
//
//	go generate ./internal/ebpf/...
//
// This requires clang/LLVM and libbpf-dev to be installed:
//
//	sudo apt install clang llvm libbpf-dev linux-headers-$(uname -r)
//	go install github.com/cilium/ebpf/cmd/bpf2go@latest
//
// bpf2go compiles block_latency.c into BPF bytecode and generates
// block_latency_bpfel.go (little-endian) and block_latency_bpfeb.go
// (big-endian) containing the bytecode as embedded byte slices. Commit
// these generated files so that downstream builds don't need clang.
package ebpf

// Compile block_latency.c → block_latency_bpf{el,eb}.go
// The generated types will be: blockLatencyObjects, blockLatencyPrograms,
// blockLatencyMaps (with fields TraceRqIssue, TraceRqComplete,
// StartTimes, IoLatencyHist).
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall" blockLatency block_latency.c
