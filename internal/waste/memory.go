// Package waste contains the adaptive load generators that keep an OCI instance
// above the idle-reclamation thresholds: a memory holder (primary), a CPU
// burner, and a lightweight network pinger. Each generator measures current
// organic utilization and only contributes the minimum extra load required.
package waste

import (
	"log/slog"
	"os"
	"runtime/debug"
	"sync"
)

// pageSize is the granularity at which the held buffer is touched to force the
// kernel to keep pages resident. It is the real page size rather than a
// hard-coded 4096 so that platforms with larger pages are still fully backed.
var pageSize = os.Getpagesize()

// Resize dead-band. Acting on sub-megabyte deltas is what caused the original
// over-hold oscillation: every resize allocated a brand-new buffer while the
// previous one was still resident, the next /proc/meminfo read counted both, so
// `organic = used - held` was over-estimated, the target deficit collapsed, and
// the holder then re-grew - repeatedly, with the whole cycle inflating observed
// total utilization far above the configured target.
const (
	// minResizeTolerance is the absolute floor of the dead-band.
	minResizeTolerance = 8 << 20 // 8 MiB
	// toleranceDivisor scales the dead-band with the amount currently held so
	// large targets are not churned by proportional measurement noise (~1.6%).
	toleranceDivisor = 64
)

// MemoryHolder maintains an in-process byte buffer sized so that TOTAL system
// memory utilization reaches a target. It is adaptive: each Adjust call recomputes
// the delta between the desired held bytes and the current held bytes given the
// system's organic usage, then grows or shrinks the buffer accordingly. Held
// pages are touched so they stay resident (counted as used by the kernel).
//
// MemoryHolder is safe for concurrent use.
type MemoryHolder struct {
	log *slog.Logger

	mu   sync.Mutex
	buf  []byte
	held int
}

// NewMemoryHolder returns an empty holder.
func NewMemoryHolder(log *slog.Logger) *MemoryHolder {
	if log == nil {
		log = slog.Default()
	}
	return &MemoryHolder{log: log}
}

// HeldBytes reports how many bytes are currently held resident.
func (h *MemoryHolder) HeldBytes() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.held
}

// TargetHeld computes how many bytes this process should hold so that total
// system usage reaches targetBytes, given the system's current usedBytes (which
// already includes whatever this process currently holds).
//
// The arithmetic: organic usage = usedBytes - currentlyHeld. To reach the
// target we must hold (targetBytes - organic). The result is clamped to
// [0, totalBytes].
func TargetHeld(targetBytes, usedBytes, currentlyHeld, totalBytes uint64) uint64 {
	organic := int64(usedBytes) - int64(currentlyHeld)
	if organic < 0 {
		organic = 0
	}
	want := int64(targetBytes) - organic
	if want < 0 {
		return 0
	}
	if uint64(want) > totalBytes {
		return totalBytes
	}
	return uint64(want)
}

// deadband returns the smallest change in held bytes worth acting on. Deltas
// below it are measurement noise (page-cache movement, GC timing) and are
// ignored so the holder stays put instead of reallocating every tick.
func (h *MemoryHolder) deadband() int {
	tol := h.held / toleranceDivisor
	if tol < minResizeTolerance {
		tol = minResizeTolerance
	}
	return tol
}

// Adjust resizes the held buffer toward `want` bytes.
//
// Two properties matter for correctness:
//
//   - A dead-band suppresses pointless churn from ordinary measurement noise.
//   - A shrink returns the old pages to the OS *before* allocating the
//     replacement and then forces a scavenge, so the next /proc/meminfo reading
//     reflects only what is genuinely held. Without that, the stale-but-not-yet
//     -collected pages inflate the next measurement and the target is overshot.
func (h *MemoryHolder) Adjust(want uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	target := clampToInt(want)
	if target == h.held {
		return
	}

	// Releasing everything is always honoured; otherwise ignore noise.
	if target > 0 {
		delta := target - h.held
		if delta < 0 {
			delta = -delta
		}
		if delta < h.deadband() {
			return
		}
	}

	if target <= 0 {
		if h.held != 0 {
			h.log.Debug("releasing held memory", "from_bytes", h.held)
		}
		h.buf = nil
		h.held = 0
		returnPagesToOS()
		return
	}

	if target > h.held {
		h.growLocked(target)
	} else {
		h.shrinkLocked(target)
	}
}

// growLocked grows the buffer to target. Growth only happens when organic usage
// fell, i.e. while memory is plentiful, so a brief old+new overlap is safe.
func (h *MemoryHolder) growLocked(target int) {
	// Geometric capacity keeps incremental growth from reallocating each tick.
	newCap := cap(h.buf)
	if newCap < target {
		if newCap == 0 {
			newCap = target
		}
		for newCap < target {
			newCap *= 2
		}
	}
	buf := make([]byte, target, newCap)
	// The buffer is filler only; no need to copy previous contents.
	touchPages(buf)
	h.log.Debug("grew held memory", "from_bytes", h.held, "to_bytes", target)
	h.buf = buf
	h.held = target
	// The replaced array is now unreferenced but still resident. Hand it back
	// immediately, otherwise the next /proc/meminfo read counts old+new and the
	// holder looks like it is over-holding (the residual half of the original
	// oscillation).
	returnPagesToOS()
}

// shrinkLocked shrinks the buffer to target. The old array is dropped and its
// pages are handed back to the OS *before* the replacement is allocated, so a
// shrink under memory pressure never transiently requires old+new at once.
func (h *MemoryHolder) shrinkLocked(target int) {
	h.log.Debug("shrank held memory", "from_bytes", h.held, "to_bytes", target)
	h.buf = nil
	h.held = 0
	returnPagesToOS()

	buf := make([]byte, target)
	touchPages(buf)
	h.buf = buf
	h.held = target
}

// Release frees all held memory. Called on shutdown.
func (h *MemoryHolder) Release() {
	h.Adjust(0)
}

// returnPagesToOS forces a collection and hands unreferenced pages back to the
// operating system immediately. This is what makes the *next* memory reading
// trustworthy: Go otherwise keeps freed heap pages resident (and, depending on
// GODEBUG=madvdontneed, may only mark them reclaimable) for an unpredictable
// time, during which they are still counted as "used" by the kernel.
func returnPagesToOS() {
	debug.FreeOSMemory()
}

// clampToInt converts a byte count to int without wrapping on 32-bit platforms.
func clampToInt(v uint64) int {
	maxInt := uint64(^uint(0) >> 1)
	if v > maxInt {
		return int(maxInt)
	}
	return int(v)
}

// touchPages writes one byte per page so the OS faults the pages in and counts
// them as resident. Without this, an untouched large allocation may remain
// unbacked and not contribute to measured utilization.
func touchPages(b []byte) {
	for i := 0; i < len(b); i += pageSize {
		b[i] = 1
	}
	if len(b) > 0 {
		b[len(b)-1] = 1
	}
}
