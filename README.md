# sysmon

A lightweight terminal system monitor for Linux, written in Go.

```
sysmon — live system monitor  (Ctrl+C to quit)
──────────────────────────────────────────────────

CPU    [████████████────────]  62.3%
Memory [████████────────────]  41.2%  (6.6 GiB / 16.0 GiB)
Swap   [────────────────────]   0.0%  (0.0 B / 4.0 GiB)
Disk   [████████████────────]  58.7%  (117.4 GiB / 200.0 GiB) [/]

Network:
  eth0       ↓ 1.2 MiB/s       ↑ 340.5 KiB/s
  wlan0      ↓ 0.0 B/s         ↑ 0.0 B/s
```

## Why sysmon?

Most system monitors either shell out to tools like `df`, `vmstat` or `ip`
(introducing process overhead per sample), or pull in large dependencies like
`gopsutil`. `sysmon` reads everything directly from the Linux kernel's `/proc`
and `statfs(2)` interfaces:

| Metric          | Source                  |
|-----------------|-------------------------|
| CPU utilisation | `/proc/stat`            |
| Memory & swap   | `/proc/meminfo`         |
| Disk I/O        | `/proc/diskstats`       |
| Disk usage      | `statfs(2)` syscall     |
| Network I/O     | `/proc/net/dev`         |

This keeps the binary small (no CGo, no external runtime), makes the parsers
trivially unit-testable with fixture files, and means it runs on minimal
container images or embedded Linux systems where `procps` may not be installed.

## Installation

### From a Debian/Ubuntu package (recommended)

Download the `.deb` from the [Releases](https://github.com/AbdGhordlo/linux-system-monitor/releases) page, then:

```bash
sudo dpkg -i sysmon_0.1.0-1_amd64.deb
```

### From source

Requires Go 1.22 or later and a Linux host.

```bash
git clone https://github.com/John/linux-system-monitor.git
cd linux-system-monitor
go build -o sysmon ./cmd/sysmon
sudo install -m 0755 sysmon /usr/local/bin/sysmon
```

### Build the Debian package yourself

```bash
sudo apt install debhelper devscripts golang-go
dpkg-buildpackage -us -uc -b
sudo dpkg -i ../sysmon_*.deb
```

## Usage

```
sysmon [flags]

Flags:
  -interval duration   refresh interval (default 2s)
                        examples: -interval 1s, -interval 500ms
  -path string         mount point to report disk usage for (default "/")
```

Examples:

```bash
# Default: refresh every 2 seconds, report disk usage for /
sysmon

# Faster refresh, report usage for /home
sysmon -interval 1s -path /home
```

## Project structure

```
sysmon/
├── cmd/
│   └── sysmon/
│       └── main.go             # CLI entry point, rendering loop
├── internal/
│   └── stats/
│       ├── cpu.go              # /proc/stat parser + CPUPercent
│       ├── cpu_test.go
│       ├── disk.go             # /proc/diskstats parser + statfs
│       ├── disk_test.go
│       ├── mem.go              # /proc/meminfo parser
│       ├── mem_test.go
│       ├── net.go              # /proc/net/dev parser + throughput
│       ├── net_test.go
│       └── testdata/           # /proc fixture files for unit tests
├── debian/                     # Debian packaging metadata
├── .github/workflows/ci.yml    # Build, test, lint, package CI
├── go.mod
└── README.md
```

## Running the tests

```bash
go test -race -v ./...
```

The test suite uses static fixture files under `internal/stats/testdata/`
that mimic real `/proc` output, so tests run on any OS (including macOS CI
runners) and don't require root or a live Linux filesystem.

## Design notes

**CPU percentage calculation** mirrors the approach used by `top` and `mpstat`:
two `/proc/stat` samples are taken an interval apart, and busy% is computed as
`(total_delta - idle_delta) / total_delta`. `IOWait` is counted as idle, since
those jiffies represent the CPU waiting on I/O rather than executing code.

**Memory "used"** follows `free -h`'s definition: `MemTotal - MemAvailable`
(not `MemTotal - MemFree`). `MemAvailable` accounts for the kernel's reclaimable
buffer and cache pages — this is the realistic "pressure" figure, not `MemFree`
which counts pages the kernel hasn't yet reclaimed.

**Disk filtering** in `/proc/diskstats` skips partition entries (`sda1`,
`nvme0n1p1`), loop devices, device-mapper nodes, and optical drives to avoid
double-counting I/O that flows through both the partition and the whole-disk
entry.

**Counter wrap protection** in the network throughput code: because the
`/proc/net/dev` counters are unsigned integers that can roll over (or reset if
an interface is removed and re-added), the code guards against negative deltas
by returning 0 rather than producing a nonsensical uint64 underflow.

## Contributing

Issues and pull requests welcome. Please run `go vet ./...` and
`golangci-lint run` before submitting.

## License

MIT — see [debian/copyright](debian/copyright).
