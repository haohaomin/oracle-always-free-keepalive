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

func TestMemoryHolderAdjust(t *testing.T) {
	h := NewMemoryHolder(nil)

	if h.HeldBytes() != 0 {
		t.Fatalf("new holder held = %d, want 0", h.HeldBytes())
	}

	h.Adjust(2 * pageSize)
	if got := h.HeldBytes(); got != 2*pageSize {
		t.Errorf("after grow held = %d, want %d", got, 2*pageSize)
	}

	// Grow again.
	h.Adjust(10 * pageSize)
	if got := h.HeldBytes(); got != 10*pageSize {
		t.Errorf("after second grow held = %d, want %d", got, 10*pageSize)
	}

	// Shrink.
	h.Adjust(pageSize)
	if got := h.HeldBytes(); got != pageSize {
		t.Errorf("after shrink held = %d, want %d", got, pageSize)
	}

	// No-op when unchanged.
	h.Adjust(pageSize)
	if got := h.HeldBytes(); got != pageSize {
		t.Errorf("after no-op held = %d, want %d", got, pageSize)
	}

	// Release.
	h.Release()
	if got := h.HeldBytes(); got != 0 {
		t.Errorf("after release held = %d, want 0", got)
	}
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
