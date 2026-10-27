//go:build darwin

package sysstat

import (
	"os/exec"
	"strconv"
	"strings"
)

// ReadMem reports total physical memory via `sysctl hw.memsize` and a
// best-effort used estimate via `vm_stat`. Both are standard macOS utilities;
// we shell out rather than cgo to keep the build dependency-free. If the used
// estimate cannot be obtained, Total is still returned with Used == 0 so memory
// detection degrades gracefully instead of failing.
func ReadMem() (MemInfo, error) {
	total, err := sysctlUint("hw.memsize")
	if err != nil {
		return MemInfo{}, errMem(err)
	}
	mi := MemInfo{Total: total}
	if used, ok := vmStatUsed(total); ok {
		mi.Used = used
	}
	return mi, nil
}

// SampleCPU is unsupported on darwin; the memory mechanism is the primary
// defeater anyway and does not need CPU sampling.
func SampleCPU() (CPUSample, error) {
	return CPUSample{}, ErrUnsupported
}

func sysctlUint(key string) (uint64, error) {
	out, err := exec.Command("sysctl", "-n", key).Output()
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
}

// vmStatUsed estimates used memory as total minus (free + inactive) pages, a
// reasonable analogue of Linux's MemAvailable. Returns false if vm_stat is
// unavailable or unparsable.
func vmStatUsed(total uint64) (uint64, bool) {
	out, err := exec.Command("vm_stat").Output()
	if err != nil {
		return 0, false
	}
	pageSize := uint64(4096)
	var freePages, inactivePages uint64
	var sawFree bool

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Mach Virtual Memory Statistics:"):
			if i := strings.Index(line, "page size of "); i >= 0 {
				rest := line[i+len("page size of "):]
				rest = strings.TrimSuffix(strings.TrimSpace(rest), " bytes)")
				if v, e := strconv.ParseUint(strings.Fields(rest)[0], 10, 64); e == nil && v > 0 {
					pageSize = v
				}
			}
		case strings.HasPrefix(line, "Pages free:"):
			freePages = parseVMStatValue(line)
			sawFree = true
		case strings.HasPrefix(line, "Pages inactive:"):
			inactivePages = parseVMStatValue(line)
		}
	}
	if !sawFree {
		return 0, false
	}
	available := (freePages + inactivePages) * pageSize
	if available > total {
		return 0, true // effectively all free
	}
	return total - available, true
}

// parseVMStatValue extracts the trailing integer from a vm_stat line such as
// "Pages free:                          123456.".
func parseVMStatValue(line string) uint64 {
	colon := strings.IndexByte(line, ':')
	if colon < 0 {
		return 0
	}
	num := strings.TrimSpace(line[colon+1:])
	num = strings.TrimSuffix(num, ".")
	v, _ := strconv.ParseUint(num, 10, 64)
	return v
}
