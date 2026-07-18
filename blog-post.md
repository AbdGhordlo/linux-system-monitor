# Building a Linux System Monitor in Go: Reading /proc Directly

Most introductory Go system-monitoring projects reach for
[gopsutil](https://github.com/shirou/gopsutil) — a fine library, but one that
abstracts away the kernel interfaces that actually make the numbers work. I
wanted to understand those interfaces directly, so I built `sysmon`: a
terminal system monitor that reads every metric from the Linux `/proc`
filesystem and the `statfs(2)` syscall with no external dependencies.

This post walks through the interesting parts: what `/proc` actually contains,
where the numbers come from, some non-obvious correctness details, and how to
package the result as a proper Debian `.deb`.

---

## What /proc actually is

`/proc` is not a real filesystem stored on disk. It is a virtual filesystem
the Linux kernel exposes to give userspace a window into kernel data structures.
When you open `/proc/stat` and read it, you are not reading a file from storage
— the kernel is constructing the content in memory in response to your `read(2)`
syscall. Every system monitoring tool you have ever used — `top`, `htop`,
`vmstat`, `netstat` — is reading from here.

This is worth understanding because it changes how you reason about
performance: reading `/proc` has negligible overhead compared to forking a
child process, and there is no stale-cache issue because the data is generated
on demand by the kernel.

---

## CPU utilisation: jiffies and two samples

`/proc/stat` looks like this on a 4-core machine:

```
cpu  24906 0 7297 280421 1136 0 214 0 0 0
cpu0 6309  0 1812 70032  285  0 112 0 0 0
cpu1 6189  0 1832 70083  283  0 50  0 0 0
cpu2 6205  0 1819 70200  287  0 29  0 0 0
cpu3 6203  0 1834 70106  281  0 23  0 0 0
```

The columns are: `user nice system idle iowait irq softirq steal guest
guest_nice`. The unit is *jiffies* — a jiffy is a kernel tick, typically
1/100th or 1/250th of a second depending on the `CONFIG_HZ` kernel
configuration option, though the values in `/proc/stat` are always reported in
`USER_HZ` units (always 100).

**The key insight**: you cannot compute a meaningful CPU percentage from a
single sample. A snapshot of 24906 user jiffies is meaningless in isolation.
You need two samples separated by a known interval and then compute:

```
total_delta = (sum of all columns, sample B) - (sum of all columns, sample A)
idle_delta  = (idle + iowait, sample B) - (idle + iowait, sample A)
busy%       = (total_delta - idle_delta) / total_delta × 100
```

`IOWait` is counted in the idle pool, not the busy pool. This mirrors what
`top` and `mpstat` report: a CPU waiting on a disk read is not executing code,
so it is idle from the process scheduler's perspective (even though the system
is doing work at the storage layer).

The Go implementation reads two `CPUTimes` slices and calls `CPUPercent`:

```go
type CPUTimes struct {
    Name    string
    User, Nice, System, Idle, IOWait, IRQ, SoftIRQ, Steal uint64
    Guest, GuestNice uint64
}

func (c CPUTimes) Total() uint64 {
    return c.User + c.Nice + c.System + c.Idle + c.IOWait + c.IRQ + c.SoftIRQ + c.Steal
}

func CPUPercent(prev, cur []CPUTimes) map[string]float64 {
    // ... delta computation per CPU line
}
```

One edge case: if the two samples are identical (zero delta), the code returns
0 rather than dividing by zero. This happens in tests using static fixtures and
occasionally in practice when the timer fires faster than a jiffy tick.

---

## Memory: why MemFree is the wrong number

`/proc/meminfo` contains dozens of fields. Most system monitors display a
`used` figure, and most newcomers compute it as `MemTotal - MemFree`. This is
wrong in a way that will confuse you.

Linux aggressively uses free memory as a buffer cache for filesystem I/O and a
page cache for recently read files. This makes the system faster, but it means
`MemFree` is typically very low on a healthy system — not because memory is
scarce, but because the kernel is doing its job.

The correct field is `MemAvailable`, added in kernel 3.14. It represents the
kernel's own estimate of how much memory is available to start new
applications without swapping — a figure that includes reclaimable page cache
and buffer pages. The formula `free -h` uses (and that `sysmon` uses) is:

```
used = MemTotal - MemAvailable
```

On kernels older than 3.14 that lack `MemAvailable`, `sysmon` falls back to
`MemFree + Buffers + Cached`, which is the approximation that older `free`
versions used. This is handled defensively in the parser:

```go
if mi.AvailableKB == 0 {
    mi.AvailableKB = mi.FreeKB + mi.BuffersKB + mi.CachedKB
}
```

---

## Disk usage: statfs(2) and the reserved-blocks subtlety

For disk *capacity*, `/proc` is not the right source. The `statfs(2)` syscall
is: it returns block counts for any mounted filesystem directly from the VFS
layer. The Go standard library exposes this as `syscall.Statfs`:

```go
var stat syscall.Statfs_t
syscall.Statfs(path, &stat)

blockSize := uint64(stat.Bsize)
total := stat.Blocks * blockSize
free  := stat.Bavail * blockSize  // NOT stat.Bfree
```

The subtlety here is `Bavail` versus `Bfree`. `Bfree` is the total number of
free blocks on the filesystem. `Bavail` is the number available to
*non-root users* — it excludes the reserved-block quota that ext4 (and other
filesystems) hold back for the root user to allow recovery when a filesystem
fills up. `df` uses `Bavail`. Using `Bfree` would show you a few percent more
free space than is actually available to you as a normal user.

---

## Disk I/O and device filtering

`/proc/diskstats` lists I/O statistics for every block device the kernel knows
about. On a real machine this includes:

- Whole disks: `sda`, `nvme0n1`, `vda`
- Partitions: `sda1`, `nvme0n1p1`
- Device-mapper nodes: `dm-0`, `dm-1` (LVM logical volumes)
- Loop devices: `loop0`, `loop1` (used by snap packages, among others)

If you sum I/O across all of these, you double-count: a write to `sda1` also
appears in `sda`'s counters because the I/O flows through both. `sysmon`
filters down to whole-disk entries only using naming heuristics:

- `loop*`, `ram*`, `dm-*`, `sr*` → always virtual or noise, skip
- `sd*`, `vd*`, `xvd*` → whole disk if the last character is a letter (sda),
  partition if it ends in a digit (sda1)
- `nvme*` → whole disk if there is no `p` in the name (nvme0n1),
  partition if there is (nvme0n1p1)

This is the same approach taken by `iostat`.

---

## Testing without a live /proc

The parsers accept `io.Reader` rather than a file path. This is idiomatic Go
and makes testing trivial: the test suite opens small fixture files from
`testdata/` that contain representative samples of real `/proc` output. No
root access required, no side effects, and the tests run identically on macOS
CI runners in GitHub Actions even though `/proc` doesn't exist there.

```go
// In production:
f, _ := os.Open("/proc/stat")
stats.ParseCPUTimes(f)

// In tests:
f, _ := os.Open("testdata/proc_stat_a")
stats.ParseCPUTimes(f)

// Or inline for simple cases:
stats.ParseMemInfo(strings.NewReader("MemTotal: 8192000 kB\n..."))
```

The CPU percentage test also validates a specific expected value, not just "it
should be between 0 and 100". Between the two fixture files, the idle delta is
200 and the total delta is 515, so the expected result is 315/515 ≈ 61.2%.
Testing concrete values catches regressions that a range check would miss.

---

## Packaging as a .deb

The project includes a `debian/` directory so it builds into a proper `.deb`
with `dpkg-buildpackage`. This is worth doing because:

1. **Installation is clean**: `sudo dpkg -i sysmon_0.1.0-1_amd64.deb` places
   the binary in `/usr/bin/` and registers it with the package manager, so
   `sudo apt remove sysmon` works cleanly.
2. **It teaches you how Ubuntu packages actually work**, which is directly
   relevant to contributing to the Ubuntu archive.
3. **lintian** (the Debian package checker) is a useful quality gate that
   catches missing fields, incorrect permissions, and policy violations.

The key files are `debian/control` (package metadata and dependencies),
`debian/rules` (the build script, a `Makefile`), `debian/changelog`
(version history in a specific machine-readable format), and
`debian/copyright` (machine-readable SPDX-style licensing).

For a Go binary the `rules` file is simple: `go build`, then
`install -D -m 0755` the resulting binary into the package staging tree.
The version is injected at build time via `-ldflags`:

```makefile
go build \
    -trimpath \
    -ldflags="-s -w -X main.version=$(shell dpkg-parsechangelog -S Version)" \
    -o $(CURDIR)/sysmon \
    ./cmd/sysmon
```

`-trimpath` removes build-machine paths from the binary (important for
reproducible builds), and `-s -w` strips the symbol table and debug
information to reduce binary size.

Prebuilt binaries, Debian packages, and Snap releases are available from the GitHub Releases page, so most users don't need to build from source.

---

## eBPF: going beyond polling

The `/proc` approach samples the system every N seconds. That's fine for a
dashboard, but it misses short-lived spikes and gives you no visibility into
*per-request* latency distributions — only aggregates. For that, you need
something event-driven. Enter eBPF.

`sysmon` ships an optional eBPF collector in `internal/ebpf/` that traces
the Linux block layer's `block_rq_issue` and `block_rq_complete` kernel
tracepoints using the [`cilium/ebpf`](https://github.com/cilium/ebpf) Go
library. Enable it at build time:

```bash
# One-time setup: compile the BPF C program into embedded Go bytecode
go generate ./internal/ebpf/...

# Build with eBPF support
go build -tags ebpf -o sysmon ./cmd/sysmon

# Run (root required for tracepoint attachment)
sudo ./sysmon -ebpf
```

With `-ebpf` enabled, sysmon adds an I/O latency histogram to the display:

```
Block I/O Latency (this interval, via eBPF):
  <1 µs        [──────────────────────────────]      0  ( 0.0%)
  1–2 µs       [──────────────────────────────]      0  ( 0.0%)
  32–64 µs     [████████──────────────────────]    142  (18.3%)
  64–128 µs    [██████████████████████────────]    487  (62.7%)
  128–256 µs   [████████──────────────────────]    140  (18.0%)
  256–512 µs   [──────────────────────────────]      8  ( 1.0%)
```

This is qualitatively different from anything `/proc` can tell you. The
latency distribution above is characteristic of an NVMe SSD under moderate
sequential load — the 64–128 µs bucket dominates. A spinning HDD would
produce a very different shape, with a long tail stretching into the
millisecond range.

**How it works**: the BPF C program (`internal/ebpf/block_latency.c`) runs
inside the kernel. When a request is issued, the `trace_rq_issue` function
records the current kernel monotonic timestamp keyed by `(dev, sector)`.
When the request completes, `trace_rq_complete` looks up that timestamp,
computes the delta, converts it to microseconds, and atomically increments
the appropriate bucket in a BPF array map. The Go loader reads the map
from userspace every interval, renders the histogram, and resets the
counts for the next frame.

The `bpf2go` tool (from the `cilium/ebpf` project) compiles the C program
at `go generate` time and embeds the resulting BPF bytecode as a Go byte
slice. The generated file is committed to the repo, so users building the
standard binary don't need clang installed — the bytecode is pre-compiled.
Only regenerating after C changes requires clang.

The entire eBPF path is gated behind the `ebpf` build tag. Without it,
the stub in `internal/ebpf/stub.go` satisfies the same interface with
no-op implementations, so the standard binary compiles cleanly everywhere
including macOS and non-BTF kernels.

## Snap packaging

Alongside the `.deb`, `sysmon` is also packaged as a [snap](https://snapcraft.io/).
The `snap/snapcraft.yaml` defines a `strict` confinement snap using the `go`
plugin — it declares only the specific `/proc` paths it needs via the
`system-files` plug rather than requesting broad filesystem access.

To build and install locally:

```bash
sudo snap install snapcraft --classic
snapcraft                                         # builds linux-sysmon_0.1.0_amd64.snap
sudo snap install linux-sysmon_0.1.0_amd64.snap --dangerous
sysmon --interval 1s
```


## Future improvements

**Top processes**: `/proc/[pid]/stat` exposes per-process CPU time in the same
jiffie format as `/proc/stat`. Reading this for every PID at each sample
interval would allow sorting processes by CPU or memory usage, like `top`.

---

## Conclusion

The interesting thing about building a `/proc`-native system monitor isn't the
Go code itself — it's what you learn about how the kernel actually represents
system state. `MemAvailable` vs `MemFree`, the two-sample requirement for CPU
percentages, the `Bavail`/`Bfree` distinction, the double-counting problem in
`/proc/diskstats`: these are the kinds of details that matter when you're
writing reliable systems software.

The source is on GitHub at [AbdGhordlo/linux-system-monitor](https://github.com/AbdGhordlo/linux-system-monitor).