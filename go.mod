module github.com/AbdGhordlo/linux-system-monitor

go 1.26.4

require (
	// Only required when building with -tags ebpf.
	// Run `go mod tidy` after installing the ebpf tag dependencies.
	github.com/cilium/ebpf v0.16.0
)
