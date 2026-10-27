package main

import (
	"fmt"
	"strconv"
	"strings"
)

// memTarget describes a parsed -mem value. It is either a percentage of total
// RAM (isPercent == true, percent in [0,100]) or an absolute byte count.
type memTarget struct {
	isPercent bool
	percent   float64 // valid when isPercent
	bytes     uint64  // valid when !isPercent
}

// resolveBytes converts the target into an absolute byte count given the total
// amount of RAM on the machine.
func (m memTarget) resolveBytes(total uint64) uint64 {
	if m.isPercent {
		return uint64(float64(total) * m.percent / 100.0)
	}
	return m.bytes
}

// String renders the target back into a human-readable form.
func (m memTarget) String() string {
	if m.isPercent {
		return strconv.FormatFloat(m.percent, 'g', -1, 64) + "%"
	}
	return strconv.FormatUint(m.bytes, 10) + "B"
}

// byteUnits maps a (case-insensitive) suffix to its multiplier. Both IEC
// (KiB/MiB/GiB) and the common SI-style shorthand (KB/MB/GB, treated as
// powers of 1024 for convenience) are accepted.
var byteUnits = []struct {
	suffix string
	mult   uint64
}{
	{"GIB", 1 << 30},
	{"MIB", 1 << 20},
	{"KIB", 1 << 10},
	{"GB", 1 << 30},
	{"MB", 1 << 20},
	{"KB", 1 << 10},
	{"G", 1 << 30},
	{"M", 1 << 20},
	{"K", 1 << 10},
	{"B", 1},
}

// parseMemTarget parses values like "30%", "512MiB", "2GiB", "1073741824".
func parseMemTarget(s string) (memTarget, error) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return memTarget{}, fmt.Errorf("empty memory target")
	}

	if strings.HasSuffix(trimmed, "%") {
		num := strings.TrimSpace(strings.TrimSuffix(trimmed, "%"))
		pct, err := strconv.ParseFloat(num, 64)
		if err != nil {
			return memTarget{}, fmt.Errorf("invalid percent %q: %w", s, err)
		}
		if pct < 0 || pct > 100 {
			return memTarget{}, fmt.Errorf("percent out of range [0,100]: %g", pct)
		}
		return memTarget{isPercent: true, percent: pct}, nil
	}

	upper := strings.ToUpper(trimmed)
	for _, u := range byteUnits {
		if strings.HasSuffix(upper, u.suffix) {
			num := strings.TrimSpace(upper[:len(upper)-len(u.suffix)])
			val, err := strconv.ParseFloat(num, 64)
			if err != nil {
				return memTarget{}, fmt.Errorf("invalid size %q: %w", s, err)
			}
			if val < 0 {
				return memTarget{}, fmt.Errorf("size cannot be negative: %s", s)
			}
			return memTarget{bytes: uint64(val * float64(u.mult))}, nil
		}
	}

	// Bare number => raw bytes.
	val, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil {
		return memTarget{}, fmt.Errorf("invalid memory target %q: %w", s, err)
	}
	return memTarget{bytes: val}, nil
}

// humanBytes renders a byte count as a friendly IEC string (e.g. "1.5 GiB").
func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
