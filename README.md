[![Go](https://img.shields.io/badge/go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![eBPF](https://img.shields.io/badge/eBPF-optional-orange?logo=linux&logoColor=white)](https://ebpf.io)
[![Platform](https://img.shields.io/badge/platform-linux-FCC624?logo=linux&logoColor=black)](https://www.kernel.org/)

# sysmon

A lightweight terminal system monitor for Linux, written in Go.

```
sysmon — live system monitor  (Ctrl+C to quit)
──────────────────────────────────────────────────

CPU    [████████████────────]  62.3%
Memory [████████────────────]  41.2%  (6.6 GiB / 16.0 GiB)
Swap   [────────────────────]   0.0%  (0.0 B / 4.0 GiB)
Disk   [████████────────────]  58.7%  (117.4 GiB / 200.0 GiB) [/]

Network:
  eth0       ↓ 1.2 MiB/s       ↑ 340.5 KiB/s
```

With `-ebpf` (see below), a block I/O latency histogram is added:

```
Block I/O Latency (this interval, via eBPF):
  32–64 µs     [████████──────────────────────]    142  (18.3%)
  64–128 µs    [██████████████████████────────]    487  (62.7%)
  128–256 µs   [████████──────────────────────]    140  (18.0%)
  256–512 µs   [──────────────────────────────]      8  ( 1.0%)
```

## Why sysmon?

Most system monitors either shell out to tools like `df`, `vmstat` or `ip`
(introducing process overhead per sample), or pull in large dependencies like
`gopsutil`. sysmon reads everything directly from the Linux kernel's `/proc`
and `statfs(2)` interfaces:

| Metric          | Source                  |
|-----------------|-------------------------|
| CPU utilisation | `/proc/stat`            |
| Memory & swap   | `/proc/meminfo`         |
| Disk I/O        | `/proc/diskstats`       |
| Disk usage      | `statfs(2)` syscall     |
| Network I/O     | `/proc/net/dev`         |
| Block latency   | eBPF tracepoints (opt.) |

## Installation

### Snap (easiest — any Linux distro)

```bash
snap install linux-sysmon
```

Run it:

```bash
linux-sysmon.sysmon                    # the snap name prefixes the command
linux-sysmon.sysmon --interval 1s
```

To use the shorter `sysmon` command, create a snap alias:

```bash
sudo snap alias linux-sysmon.sysmon sysmon
sysmon --interval 1s
```

> **Note:** the snap is built without eBPF support. Snap's strict confinement
> sandbox does not permit the `CAP_BPF` kernel capability that tracepoint
> attachment requires. If you want the eBPF latency histogram, build from
> source with `-tags ebpf` (see the [eBPF section](#ebpf-block-io-latency-histogram) below).

### From a Debian/Ubuntu package

Download the latest `.deb` from the [Releases](https://github.com/AbdGhordlo/linux-system-monitor/releases) page, then:

```bash
sudo dpkg -i sysmon_*.deb
sysmon --interval 1s
```

Or build the `.deb` yourself:

```bash
sudo apt install debhelper devscripts golang-go
git clone https://github.com/AbdGhordlo/linux-system-monitor.git && cd sysmon
dpkg-buildpackage -us -uc -b
sudo dpkg -i ../sysmon_*.deb
```

### From source

Requires Go 1.22+ and Linux.

Build the standard version:
```bash
git clone https://github.com/AbdGhordlo/linux-system-monitor.git
cd sysmon
go build -trimpath -ldflags="-s -w" -o sysmon ./cmd/sysmon
sudo install -m 0755 sysmon /usr/local/bin/sysmon
```

## Usage

```
sysmon [flags]

Flags:
  -interval duration   refresh interval (default 2s)
                        examples: -interval 1s, -interval 500ms
  -path string         mount point to report disk usage for (default "/")
  -ebpf                enable block I/O latency histogram (see below)
```

## eBPF Block I/O Latency Histogram

When built with `-tags ebpf` and run with `-ebpf`, sysmon attaches to the
`block_rq_issue` and `block_rq_complete` kernel tracepoints to measure the
round-trip latency of every block I/O request — without polling. The latency
distribution for the last interval is shown as a power-of-2 histogram.

**Requirements:**
- Linux kernel 5.8+ with `CONFIG_DEBUG_INFO_BTF=y` (standard on Ubuntu 22.04+)
- Root or `CAP_BPF` capability
- `clang`, `llvm`, and `libbpf-dev` (one-time, to compile the BPF C program)

**One-time setup:**

```bash
# Install build dependencies
sudo apt install clang llvm libbpf-dev linux-headers-$(uname -r)

# Install bpf2go (compiles the BPF C program into embedded Go bytecode)
go install github.com/cilium/ebpf/cmd/bpf2go@latest

# Compile block_latency.c → generates block_latency_bpfel.go and block_latency_bpfeb.go
# Commit these generated files; other users won't need clang after that.
go generate ./internal/ebpf/...
```

**Build and run:**

```bash
go build -tags ebpf -trimpath -ldflags="-s -w" -o sysmon-ebpf ./cmd/sysmon
sudo ./sysmon-ebpf -ebpf
```

The eBPF feature is completely optional. Without `-tags ebpf`, the binary
compiles normally on any platform and the `-ebpf` flag prints a clear message
explaining how to enable it.

## Project structure

```
sysmon/
├── cmd/
│   └── sysmon/
│       └── main.go             # CLI entry point and rendering loop
├── internal/
│   └── stats/
│       ├── cpu.go              # /proc/stat parser + CPUPercent
│       ├── disk.go             # /proc/diskstats parser + statfs
│       ├── mem.go              # /proc/meminfo parser
│       ├── net.go              # /proc/net/dev parser + throughput
│       ├── *_test.go           # unit tests using /proc fixture files
│       └── testdata/           # static /proc fixture files
│   └── ebpf/
│       ├── block_latency.c     # BPF C program (compiled by go generate)
│       ├── generate.go         # go:generate directive for bpf2go
│       ├── loader.go           # Go loader (linux && ebpf build tag)
│       └── stub.go             # no-op stub (all other platforms/builds)
├── debian/                     # Debian package metadata
├── snap/
│   └── snapcraft.yaml          # Snap package definition
├── .github/
│   └── workflows/
│       ├── ci.yml              # Build, test, lint, package
│       └── release.yml         # Publish binaries + .deb on git tag
└── go.mod
```

## Running the tests

```bash
go test -race -v ./...
```

The test suite uses static fixture files under `internal/stats/testdata/`
so it runs on any OS and requires neither root nor a live Linux system.

## Design notes

**CPU percentage** mirrors `top` and `mpstat`: two `/proc/stat` samples
are taken an interval apart, and busy% is `(total_delta - idle_delta) /
total_delta`. `IOWait` counts as idle — those jiffies represent the CPU
waiting on I/O, not executing code.

**Memory "used"** follows `free -h`: `MemTotal - MemAvailable`, not
`MemTotal - MemFree`. `MemAvailable` includes reclaimable buffer/cache
pages; `MemFree` does not, and routinely shows near-zero on healthy systems.

**Disk usage** uses `Bavail` from `statfs(2)`, not `Bfree`. `Bavail`
excludes root-reserved blocks, matching `df`.

**Disk I/O filtering** in `/proc/diskstats` skips partition entries,
loop devices, and device-mapper nodes to avoid double-counting I/O that
flows through both a partition and its parent whole-disk entry.

**eBPF counter wraps** in the network throughput code: `/proc/net/dev`
counters are unsigned integers that can roll over, so the code checks
`cur >= prev` before subtracting and returns 0 on underflow rather than
producing a massive spurious rate.

## Contributing

PRs welcome. Please run `go vet ./...` and `golangci-lint run` before
submitting.

## License

MIT — see [LICENSE](LICENSE).