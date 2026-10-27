package waste

import (
	"context"
	"crypto/sha256"
	"log/slog"
	"sync"
	"time"
)

// BurnDuration computes how long to run full-tilt CPU work within a control
// interval so that the *average* utilization over that interval rises from the
// currently-observed `current` fraction to the `floor` fraction.
//
// Model: over the interval the box already does `current` worth of work
// organically. If we additionally peg the CPU for a fraction f of the interval,
// the expected average becomes roughly current + f*(1-current). Solving for the
// f that reaches `floor`:
//
//	f = (floor - current) / (1 - current)
//
// Returns 0 when organic load already meets the floor (do nothing this tick) or
// when the floor is disabled (<= 0). The result never exceeds `interval`.
func BurnDuration(current, floor float64, interval time.Duration) time.Duration {
	if floor <= 0 || interval <= 0 {
		return 0
	}
	if floor > 1 {
		floor = 1
	}
	if current >= floor {
		return 0
	}
	headroom := 1 - current
	if headroom <= 0 {
		return 0
	}
	f := (floor - current) / headroom
	if f <= 0 {
		return 0
	}
	if f > 1 {
		f = 1
	}
	return time.Duration(f * float64(interval))
}

// CPUBurner runs SHA-256 churn across a pool of goroutines for a bounded
// duration to raise CPU utilization toward a floor. It is adaptive: the caller
// passes the burn duration computed by BurnDuration each tick, so an already-busy
// box does no extra work.
type CPUBurner struct {
	log     *slog.Logger
	workers int
}

// NewCPUBurner returns a burner that will use `workers` goroutines (sized to
// the number of cores by the caller). A minimum of one worker is enforced.
func NewCPUBurner(workers int, log *slog.Logger) *CPUBurner {
	if workers < 1 {
		workers = 1
	}
	if log == nil {
		log = slog.Default()
	}
	return &CPUBurner{log: log, workers: workers}
}

// Burn pegs the CPU for up to `d` by hashing across the worker pool. It returns
// early if ctx is cancelled. A non-positive duration is a no-op.
func (c *CPUBurner) Burn(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	deadline := time.Now().Add(d)
	c.log.Debug("cpu burn", "duration", d, "workers", c.workers)

	var wg sync.WaitGroup
	for i := 0; i < c.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			spin(ctx, deadline)
		}()
	}
	wg.Wait()
}

// spin performs CPU-bound work (hashing a small buffer in a tight loop) until
// the deadline passes or the context is cancelled. The deadline is checked in
// batches to avoid the time.Now() overhead dominating the actual work.
func spin(ctx context.Context, deadline time.Time) {
	buf := make([]byte, 4096)
	for {
		// Do a batch of hashing between deadline checks.
		for i := 0; i < 1024; i++ {
			sum := sha256.Sum256(buf)
			copy(buf, sum[:])
		}
		if time.Now().After(deadline) {
			return
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}
