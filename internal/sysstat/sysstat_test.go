package sysstat

import (
	"math"
	"testing"
)

func TestParseMeminfo(t *testing.T) {
	tests := []struct {
		name      string
		contents  string
		wantTotal uint64
		wantUsed  uint64
		wantErr   bool
	}{
		{
			name: "available present",
			contents: `MemTotal:        8192000 kB
MemFree:         1000000 kB
MemAvailable:    6144000 kB
Buffers:          200000 kB
Cached:          3000000 kB
`,
			wantTotal: 8192000 * 1024,
			// used = total - available = (8192000 - 6144000) kB
			wantUsed: (8192000 - 6144000) * 1024,
		},
		{
			name: "no available falls back to free+buffers+cached",
			contents: `MemTotal:        4000000 kB
MemFree:          500000 kB
Buffers:          100000 kB
Cached:          1400000 kB
`,
			wantTotal: 4000000 * 1024,
			// reclaimable = 500000+100000+1400000 = 2000000 kB
			wantUsed: (4000000 - 2000000) * 1024,
		},
		{
			name: "available clamped to total",
			contents: `MemTotal:        1000000 kB
MemAvailable:    2000000 kB
`,
			wantTotal: 1000000 * 1024,
			wantUsed:  0,
		},
		{
			name:     "missing total errors",
			contents: "MemFree: 100 kB\n",
			wantErr:  true,
		},
		{
			name:     "empty errors",
			contents: "",
			wantErr:  true,
		},
		{
			name: "unparsable lines skipped",
			contents: `garbage line without colon
MemTotal:        2000000 kB
MemAvailable:    1500000 kB
WeirdKey:        notanumber kB
`,
			wantTotal: 2000000 * 1024,
			wantUsed:  (2000000 - 1500000) * 1024,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mi, err := parseMeminfo(tc.contents)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseMeminfo() = %+v, want error", mi)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMeminfo() unexpected error: %v", err)
			}
			if mi.Total != tc.wantTotal {
				t.Errorf("Total = %d, want %d", mi.Total, tc.wantTotal)
			}
			if mi.Used != tc.wantUsed {
				t.Errorf("Used = %d, want %d", mi.Used, tc.wantUsed)
			}
		})
	}
}

func TestParseStatCPU(t *testing.T) {
	tests := []struct {
		name      string
		contents  string
		wantBusy  uint64
		wantTotal uint64
		wantErr   bool
	}{
		{
			name:     "standard line",
			contents: "cpu  100 20 30 1000 50 5 5 0 0 0\ncpu0 50 10 15 500 25 2 2 0 0 0\n",
			// total = 100+20+30+1000+50+5+5 = 1210; busy = total - idle(1000) - iowait(50) = 160
			wantBusy:  160,
			wantTotal: 1210,
		},
		{
			name:     "no iowait field",
			contents: "cpu 10 0 5 100\n",
			// total = 115; idle = 100; iowait = 0; busy = 15
			wantBusy:  15,
			wantTotal: 115,
		},
		{
			name:     "missing cpu line",
			contents: "intr 12345\nctxt 999\n",
			wantErr:  true,
		},
		{
			name:     "malformed numbers",
			contents: "cpu  100 xx 30 1000 50\n",
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cs, err := parseStatCPU(tc.contents)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseStatCPU() = %+v, want error", cs)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseStatCPU() unexpected error: %v", err)
			}
			if cs.Busy != tc.wantBusy {
				t.Errorf("Busy = %d, want %d", cs.Busy, tc.wantBusy)
			}
			if cs.Total != tc.wantTotal {
				t.Errorf("Total = %d, want %d", cs.Total, tc.wantTotal)
			}
		})
	}
}

func TestCPUUtilization(t *testing.T) {
	tests := []struct {
		name string
		prev CPUSample
		cur  CPUSample
		want float64
	}{
		{
			name: "half busy",
			prev: CPUSample{Busy: 100, Total: 1000},
			cur:  CPUSample{Busy: 150, Total: 1100}, // busyDelta 50 / totalDelta 100
			want: 0.5,
		},
		{
			name: "fully idle",
			prev: CPUSample{Busy: 100, Total: 1000},
			cur:  CPUSample{Busy: 100, Total: 1100},
			want: 0.0,
		},
		{
			name: "fully busy",
			prev: CPUSample{Busy: 100, Total: 1000},
			cur:  CPUSample{Busy: 200, Total: 1100},
			want: 1.0,
		},
		{
			name: "no time elapsed",
			prev: CPUSample{Busy: 100, Total: 1000},
			cur:  CPUSample{Busy: 100, Total: 1000},
			want: 0.0,
		},
		{
			name: "counter wraparound",
			prev: CPUSample{Busy: 100, Total: 1000},
			cur:  CPUSample{Busy: 50, Total: 900},
			want: 0.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CPUUtilization(tc.prev, tc.cur)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("CPUUtilization() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMemInfoUsedFraction(t *testing.T) {
	if got := (MemInfo{Total: 0, Used: 0}).UsedFraction(); got != 0 {
		t.Errorf("zero total fraction = %v, want 0", got)
	}
	if got := (MemInfo{Total: 1000, Used: 250}).UsedFraction(); math.Abs(got-0.25) > 1e-9 {
		t.Errorf("fraction = %v, want 0.25", got)
	}
}
