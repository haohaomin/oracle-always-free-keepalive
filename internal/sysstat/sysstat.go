// Package sysstat reads basic system memory and CPU utilization statistics
// using only the Go standard library. On Linux it parses /proc/meminfo and
// /proc/stat; on other platforms it degrades gracefully (memory via sysctl on
// darwin, CPU reported as unsupported).
package sysstat

import (
	"errors"
	"fmt"
	"runtime"
)

// ErrUnsupported is returned when a metric cannot be read on the current OS.
var ErrUnsupported = errors.New("sysstat: metric unavailable on this OS")

// MemInfo holds total and used physical memory in bytes.
type MemInfo struct {
	Total uint64
	Used  uint64
}

// UsedFraction returns used/total in [0,1]; 0 when total is unknown.
func (m MemInfo) UsedFraction() float64 {
	if m.Total == 0 {
		return 0
	}
	return float64(m.Used) / float64(m.Total)
}

// CPUSample is a point-in-time snapshot of aggregate CPU jiffies. Compute a
// utilization fraction by taking two samples and calling CPUUtilization.
type CPUSample struct {
	Busy  uint64 // user+nice+system+irq+softirq+steal
	Total uint64 // Busy + idle + iowait
}

// CPUUtilization computes the busy fraction [0,1] between two samples. It
// returns 0 when the total delta is non-positive (no time elapsed / wraparound).
func CPUUtilization(prev, cur CPUSample) float64 {
	totalDelta := int64(cur.Total) - int64(prev.Total)
	busyDelta := int64(cur.Busy) - int64(prev.Busy)
	if totalDelta <= 0 || busyDelta < 0 {
		return 0
	}
	frac := float64(busyDelta) / float64(totalDelta)
	if frac < 0 {
		return 0
	}
	if frac > 1 {
		return 1
	}
	return frac
}

// NumCPU reports the number of logical CPUs visible to the process.
func NumCPU() int { return runtime.NumCPU() }

// SupportsCPU reports whether per-tick CPU sampling is available on this OS.
func SupportsCPU() bool { return runtime.GOOS == "linux" }

// errMem wraps a memory read failure with OS context.
func errMem(err error) error {
	return fmt.Errorf("sysstat: read memory on %s: %w", runtime.GOOS, err)
}
