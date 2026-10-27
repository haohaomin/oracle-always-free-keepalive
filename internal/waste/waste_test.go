package waste

import (
	"math"
	"testing"
	"time"
)

func TestTargetHeld(t *testing.T) {
	const total = 8 << 30 // 8 GiB

	tests := []struct {
		name          string
		targetBytes   uint64
		usedBytes     uint64
		currentlyHeld uint64
		want          uint64
	}{
		{
			name:          "nothing held yet, organic below target",
			targetBytes:   3 << 30,
			usedBytes:     1 << 30, // organic = 1 GiB
			currentlyHeld: 0,
			want:          2 << 30, // need to hold 2 GiB more
		},
		{
			name:          "already holding, organic grew",
			targetBytes:   3 << 30,
			usedBytes:     3 << 30, // includes 2 GiB held
			currentlyHeld: 2 << 30, // organic = 1 GiB
			want:          2 << 30, // still need 2 GiB held
		},
		{
			name:          "organic exceeds target -> release",
			targetBytes:   2 << 30,
			usedBytes:     4 << 30,
			currentlyHeld: 1 << 30, // organic = 3 GiB > target
			want:          0,
		},
		{
			name:          "box busy, hold less",
			targetBytes:   3 << 30,
			usedBytes:     4 << 30, // includes 2 GiB held => organic 2 GiB
			currentlyHeld: 2 << 30,
			want:          1 << 30, // need only 1 GiB
		},
		{
			name:          "clamped to total",
			targetBytes:   100 << 30,
			usedBytes:     0,
			currentlyHeld: 0,
			want:          total,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := TargetHeld(tc.targetBytes, tc.usedBytes, tc.currentlyHeld, total)
			if got != tc.want {
				t.Errorf("TargetHeld(%d,%d,%d,%d) = %d, want %d",
					tc.targetBytes, tc.usedBytes, tc.currentlyHeld, total, got, tc.want)
			}
		})
	}
}

func TestBurnDuration(t *testing.T) {
	const interval = 30 * time.Second

	tests := []struct {
		name     string
		current  float64
		floor    float64
		interval time.Duration
		want     time.Duration
	}{
		{
			name:     "floor disabled",
			current:  0.1,
			floor:    0,
			interval: interval,
			want:     0,
		},
		{
			name:     "already above floor",
			current:  0.6,
			floor:    0.5,
			interval: interval,
			want:     0,
		},
		{
			name:     "idle box, half floor",
			current:  0.0,
			floor:    0.5,
			interval: interval,
			want:     15 * time.Second, // f = 0.5/1 = 0.5
		},
		{
			name:     "partly busy",
			current:  0.2,
			floor:    0.6,
			interval: interval,
			// f = (0.6-0.2)/(1-0.2) = 0.4/0.8 = 0.5
			want: 15 * time.Second,
		},
		{
			name:     "floor clamped to 1",
			current:  0.0,
			floor:    2.0,
			interval: interval,
			want:     interval, // f=1
		},
		{
			name:     "zero interval",
			current:  0.0,
			floor:    0.5,
			interval: 0,
			want:     0,
		},
		{
			name:     "current at full",
			current:  1.0,
			floor:    0.5,
			interval: interval,
			want:     0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BurnDuration(tc.current, tc.floor, tc.interval)
			// Allow a tiny rounding tolerance.
			if math.Abs(float64(got-tc.want)) > float64(time.Millisecond) {
				t.Errorf("BurnDuration(%v,%v,%v) = %v, want %v",
					tc.current, tc.floor, tc.interval, got, tc.want)
			}
		})
	}
}

const mib = 1 << 20

func TestMemoryHolderAdjust(t *testing.T) {
	h := NewMemoryHolder(nil)

	if h.HeldBytes() != 0 {
		t.Fatalf("new holder held = %d, want 0", h.HeldBytes())
	}

	// Steps deliberately exceed the resize dead-band so exact resizing is exercised.
	h.Adjust(16 * mib)
	if got := h.HeldBytes(); got != 16*mib {
		t.Errorf("after grow held = %d, want %d", got, 16*mib)
	}

	// Grow again.
	h.Adjust(32 * mib)
	if got := h.HeldBytes(); got != 32*mib {
		t.Errorf("after second grow held = %d, want %d", got, 32*mib)
	}

	// Shrink.
	h.Adjust(16 * mib)
	if got := h.HeldBytes(); got != 16*mib {
		t.Errorf("after shrink held = %d, want %d", got, 16*mib)
	}

	// No-op when unchanged.
	h.Adjust(16 * mib)
	if got := h.HeldBytes(); got != 16*mib {
		t.Errorf("after no-op held = %d, want %d", got, 16*mib)
	}

	// Release.
	h.Release()
	if got := h.HeldBytes(); got != 0 {
		t.Errorf("after release held = %d, want 0", got)
	}
}

// TestMemoryHolderDeadbandImmuneToNoise is the regression guard for the
// over-hold oscillation: sub-dead-band deltas must not reallocate the buffer.
// The original implementation allocated a fresh array on every call, and while
// the previous array was still resident the next /proc/meminfo sample counted
// both, so `organic` looked ~2x too large, the deficit collapsed, and the holder
// re-grew - driving observed utilization to ~46% for a 30% target.
func TestMemoryHolderDeadbandImmuneToNoise(t *testing.T) {
	h := NewMemoryHolder(nil)
	h.Adjust(64 * mib)
	if got := h.HeldBytes(); got != 64*mib {
		t.Fatalf("setup held = %d, want %d", got, 64*mib)
	}
	before := &h.buf[0]

	// Ordinary measurement noise: a few MiB either way.
	for _, v := range []int{66 * mib, 63 * mib, 65 * mib, 64*mib + 1024, 64*mib - 1024, 64 * mib} {
		h.Adjust(uint64(v))
		if got := h.HeldBytes(); got != 64*mib {
			t.Errorf("Adjust(%d) moved held to %d, want it pinned at %d", v, got, 64*mib)
		}
		if &h.buf[0] != before {
			t.Errorf("Adjust(%d) reallocated the buffer despite being inside the dead-band", v)
		}
	}
}

// TestMemoryHolderActsBeyondDeadband verifies the dead-band is not so wide that
// genuine changes are ignored (e.g. a business workload freeing memory).
func TestMemoryHolderActsBeyondDeadband(t *testing.T) {
	h := NewMemoryHolder(nil)
	h.Adjust(64 * mib)
	before := &h.buf[0]

	h.Adjust(96 * mib) // +32 MiB, well outside the dead-band
	if got := h.HeldBytes(); got != 96*mib {
		t.Errorf("held = %d, want %d", got, 96*mib)
	}
	if &h.buf[0] == before {
		t.Error("buffer was not reallocated on a genuine 32 MiB growth")
	}

	// A real release must be honoured even though it is a "delta".
	h.Adjust(0)
	if got := h.HeldBytes(); got != 0 {
		t.Errorf("after release to 0 held = %d, want 0", got)
	}
	if len(h.buf) != 0 {
		t.Errorf("buffer not dropped on release: len = %d", len(h.buf))
	}
}

// TestMemoryHolderDeadbandScales asserts the dead-band grows with the amount
// held so a large target is not churned by proportionally larger noise.
func TestMemoryHolderDeadbandScales(t *testing.T) {
	h := NewMemoryHolder(nil)
	if got := h.deadband(); got != minResizeTolerance {
		t.Errorf("deadband at 0 held = %d, want %d", got, minResizeTolerance)
	}
	h.Adjust(1 << 30) // 1 GiB
	if got, want := h.deadband(), (1<<30)/toleranceDivisor; got != want {
		t.Errorf("deadband at 1 GiB = %d, want %d", got, want)
	}
	h.Release()
}

func TestNewNetPingerDefaults(t *testing.T) {
	p := NewNetPinger("", 0, nil)
	if p.url != DefaultNetURL {
		t.Errorf("empty url = %q, want default %q", p.url, DefaultNetURL)
	}
	if p.client.Timeout != 60*time.Second {
		t.Errorf("default timeout = %v, want 60s", p.client.Timeout)
	}

	custom := NewNetPinger("https://example.com/x", 5*time.Second, nil)
	if custom.url != "https://example.com/x" {
		t.Errorf("custom url = %q", custom.url)
	}
	if custom.client.Timeout != 5*time.Second {
		t.Errorf("custom timeout = %v, want 5s", custom.client.Timeout)
	}
}
