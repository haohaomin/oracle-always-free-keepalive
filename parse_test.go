package main

import "testing"

func TestParseMemTarget(t *testing.T) {
	var total uint64 = 8 * (1 << 30) // 8 GiB (runtime var so float math isn't constant-folded)

	tests := []struct {
		name      string
		in        string
		wantErr   bool
		wantBytes uint64 // expected resolved bytes against `total`
	}{
		{name: "percent", in: "30%", wantBytes: uint64(float64(total) * 30 / 100.0)},
		{name: "percent with spaces", in: " 50 % ", wantBytes: total / 2},
		{name: "zero percent", in: "0%", wantBytes: 0},
		{name: "hundred percent", in: "100%", wantBytes: total},
		{name: "mebibytes", in: "512MiB", wantBytes: 512 * (1 << 20)},
		{name: "gibibytes", in: "2GiB", wantBytes: 2 * (1 << 30)},
		{name: "mb shorthand", in: "256MB", wantBytes: 256 * (1 << 20)},
		{name: "lowercase unit", in: "1gib", wantBytes: 1 << 30},
		{name: "bare bytes", in: "1048576", wantBytes: 1 << 20},
		{name: "bytes suffix", in: "1024B", wantBytes: 1024},
		{name: "fractional gib", in: "1.5GiB", wantBytes: uint64(1.5 * float64(1<<30))},
		{name: "empty", in: "", wantErr: true},
		{name: "garbage", in: "abc", wantErr: true},
		{name: "percent over 100", in: "150%", wantErr: true},
		{name: "negative percent", in: "-5%", wantErr: true},
		{name: "negative size", in: "-1GiB", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mt, err := parseMemTarget(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseMemTarget(%q) = %v, want error", tc.in, mt)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseMemTarget(%q) unexpected error: %v", tc.in, err)
			}
			got := mt.resolveBytes(total)
			if got != tc.wantBytes {
				t.Errorf("parseMemTarget(%q).resolveBytes(%d) = %d, want %d", tc.in, total, got, tc.wantBytes)
			}
		})
	}
}

func TestHumanBytes(t *testing.T) {
	tests := []struct {
		in   uint64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.00 KiB"},
		{1 << 20, "1.00 MiB"},
		{1 << 30, "1.00 GiB"},
		{3 * (1 << 30) / 2, "1.50 GiB"},
	}
	for _, tc := range tests {
		if got := humanBytes(tc.in); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
