// Package waste contains the adaptive load generators that keep an OCI instance
// above the idle-reclamation thresholds: a memory holder (primary), a CPU
// burner, and a lightweight network pinger. Each generator measures current
// organic utilization and only contributes the minimum extra load required.
package waste

import (
	"log/slog"
	"sync"
)

// pageSize is the granularity at which we touch the held buffer to force the
// kernel to keep pages resident (typical Linux page size).
const pageSize = 4096

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

// Adjust resizes the held buffer toward `want` bytes. Growth allocates a new
// buffer and touches every page so the kernel keeps it resident; shrinking
// releases the buffer so the runtime can return memory to the OS.
func (h *MemoryHolder) Adjust(want uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	target := int(want)
	if target == h.held {
		return
	}

	if target <= 0 {
		if h.held != 0 {
			h.log.Debug("releasing held memory", "from_bytes", h.held)
		}
		h.buf = nil
		h.held = 0
		return
	}

	if target == h.held {
		return
	}

	// Reallocate to the exact target. Reusing the slice for shrink would keep
	// the old backing array alive, so we always allocate fresh for clarity.
	newBuf := make([]byte, target)
	touchPages(newBuf)
	h.log.Debug("adjusted held memory", "from_bytes", h.held, "to_bytes", target)
	h.buf = newBuf
	h.held = target
}

// Release frees all held memory. Called on shutdown.
func (h *MemoryHolder) Release() {
	h.Adjust(0)
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
