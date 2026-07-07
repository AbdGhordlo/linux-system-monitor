// Package stats provides collectors that read live system metrics directly
// from the Linux filesystem and kernel APIs.
//
// This file implements two distinct, independent features related to disks:
//
// 1. Disk I/O Counters (Performance Metrics)
//   - Tracks cumulative read/write operations and sector throughput.
//   - Parsed directly from /proc/diskstats for top-level block devices.
//   - See: DiskIO, ReadDiskIO()
//
// 2. Disk Usage (Capacity Metrics)
//   - Tracks total, free, and used filesystem space for a given mount point.
//   - Leverages the low-level statfs system call (similar to the 'df' command).
//   - See: DiskUsage, ReadDiskUsage()
package stats

import (
	"bufio" // Used to read /proc/diskstats line by line using a Scanner
	"fmt"
	"os" // Used to open /proc/diskstats
	"strconv"
	"strings"
	"syscall" // Used for the statfs syscall to get filesystem stats
)

// Represents one disk. It holds cumulative I/O counters for a single block device, taken
// from /proc/diskstats. Linux doesn't count bytes, it counts sectors. (1 sector = 512 bytes)
type DiskIO struct {
	// The name of the storage device (e.g., "sda", "nvme0n1p1").
	Device string
	// Total sectors read from the device since boot. Typically 512 bytes per sector.
	ReadSectors uint64
	// Total sectors written to the device since boot.
	WriteSectors uint64
	// Total number of completed read I/O operations.
	ReadOps uint64
	// Total number of completed write I/O operations.
	WriteOps uint64
}

// ReadBytes converts the sector count to bytes.
func (d DiskIO) ReadBytes() uint64 { return d.ReadSectors * 512 }

// WriteBytes converts the sector count to bytes.
func (d DiskIO) WriteBytes() uint64 { return d.WriteSectors * 512 }

// ReadDiskIO parses /proc/diskstats. Only "real" whole-disk devices are
// kept (sdX, nvmeXnY, vdX); loop devices, ram disks and individual
// partitions are skipped to avoid double-counting and noisy output.
func ReadDiskIO() ([]DiskIO, error) {
	f, err := os.Open("/proc/diskstats")
	if err != nil {
		return nil, fmt.Errorf("opening /proc/diskstats: %w", err)
	}
	defer f.Close()

	var result []DiskIO

	// Read /proc/diskstats line by line, parsing each line into a DiskIO struct.
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// Format: major minor name rd_ios rd_merges rd_sectors rd_ticks
		//         wr_ios wr_merges wr_sectors wr_ticks ...
		if len(fields) < 14 {
			continue
		}

		name := fields[2]
		// Only include whole-disk devices, skipping partitions and virtual devices.
		if !isWholeDisk(name) {
			continue
		}

		readOps, err1 := strconv.ParseUint(fields[3], 10, 64)
		readSectors, err2 := strconv.ParseUint(fields[5], 10, 64)
		writeOps, err3 := strconv.ParseUint(fields[7], 10, 64)
		writeSectors, err4 := strconv.ParseUint(fields[9], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			continue // skip malformed line rather than failing the whole read
		}

		result = append(result, DiskIO{
			Device:       name,
			ReadOps:      readOps,
			ReadSectors:  readSectors,
			WriteOps:     writeOps,
			WriteSectors: writeSectors,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning /proc/diskstats: %w", err)
	}

	return result, nil
}

// isWholeDisk filters /proc/diskstats device names down to top-level
// block devices, skipping partitions (sda1, nvme0n1p1) and virtual
// devices (loop0, ram0, dm-0) that would otherwise double-count I/O.
func isWholeDisk(name string) bool {
	switch {
	case strings.HasPrefix(name, "loop"),
		strings.HasPrefix(name, "ram"),
		strings.HasPrefix(name, "dm-"),
		strings.HasPrefix(name, "sr"):
		return false
	case strings.HasPrefix(name, "nvme"):
		// nvme0n1 is the whole disk; nvme0n1p1 is a partition.
		return !strings.Contains(name, "p")
	case strings.HasPrefix(name, "sd"), strings.HasPrefix(name, "vd"), strings.HasPrefix(name, "xvd"):
		// sda is the whole disk; sda1 is a partition. Whole disk names
		// end in a letter, partitions end in a digit.
		last := name[len(name)-1]
		return last < '0' || last > '9'
	default:
		return true
	}
}

// Represents filesystem capacity for a mount point supplied by the caller.
// The statistics are retrieved using the statfs system call.
type DiskUsage struct {
	// The path supplied to ReadDiskUsage (typically a mount point such as "/" or "/home").
	Path string
	// The total storage capacity of the filesystem in bytes.
	TotalBytes uint64
	// The remaining unallocated space in bytes available to non-privileged users.
	FreeBytes uint64
}

// UsedBytes returns the bytes in use on the filesystem.
func (d DiskUsage) UsedBytes() uint64 {
	if d.FreeBytes > d.TotalBytes {
		return 0
	}
	return d.TotalBytes - d.FreeBytes
}

// UsedPercent returns the percentage of the filesystem in use.
func (d DiskUsage) UsedPercent() float64 {
	if d.TotalBytes == 0 {
		return 0
	}
	return (float64(d.UsedBytes()) / float64(d.TotalBytes)) * 100
}

// ReadDiskUsage reports capacity for a single mount point (typically "/").
// It uses the statfs(2) syscall directly rather than shelling out to df.
func ReadDiskUsage(path string) (DiskUsage, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return DiskUsage{}, fmt.Errorf("statfs %q: %w", path, err)
	}

	blockSize := uint64(stat.Bsize)
	return DiskUsage{
		Path:       path,
		TotalBytes: stat.Blocks * blockSize,
		FreeBytes:  stat.Bavail * blockSize, // Bavail (not Bfree): excludes root-reserved blocks, matching `df`
	}, nil
}
