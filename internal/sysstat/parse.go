package sysstat

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// parseMeminfo parses the contents of /proc/meminfo into a MemInfo. Used memory
// is computed as Total - Available (the kernel's estimate of memory that can be
// handed to a new workload without swapping), which matches what OCI's idle
// detector and tools like `free` consider "used" for utilization purposes.
//
// Values in /proc/meminfo are reported in kibibytes (the "kB" suffix is a
// historical misnomer for 1024-byte units).
func parseMeminfo(contents string) (MemInfo, error) {
	const kib = 1024
	var total, available, free, buffers, cached uint64
	var haveTotal, haveAvailable, haveFree bool

	sc := bufio.NewScanner(strings.NewReader(contents))
	for sc.Scan() {
		line := sc.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		fields := strings.Fields(line[colon+1:])
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue // skip unparsable lines rather than failing the whole read
		}
		switch key {
		case "MemTotal":
			total, haveTotal = v*kib, true
		case "MemAvailable":
			available, haveAvailable = v*kib, true
		case "MemFree":
			free, haveFree = v*kib, true
		case "Buffers":
			buffers = v * kib
		case "Cached":
			cached = v * kib
		}
	}
	if err := sc.Err(); err != nil {
		return MemInfo{}, fmt.Errorf("scan meminfo: %w", err)
	}
	if !haveTotal || total == 0 {
		return MemInfo{}, fmt.Errorf("meminfo: missing MemTotal")
	}

	var used uint64
	switch {
	case haveAvailable:
		if available > total {
			available = total
		}
		used = total - available
	case haveFree:
		// Fallback for older kernels without MemAvailable.
		reclaimable := free + buffers + cached
		if reclaimable > total {
			reclaimable = total
		}
		used = total - reclaimable
	default:
		return MemInfo{}, fmt.Errorf("meminfo: missing MemAvailable and MemFree")
	}

	return MemInfo{Total: total, Used: used}, nil
}

// parseStatCPU parses the aggregate "cpu" line from /proc/stat into a CPUSample.
// The line format is:
//
//	cpu  user nice system idle iowait irq softirq steal guest guest_nice
//
// Busy excludes idle and iowait; Total includes everything through steal.
func parseStatCPU(contents string) (CPUSample, error) {
	sc := bufio.NewScanner(strings.NewReader(contents))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu ") && line != "cpu" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "cpu" {
			continue
		}
		vals := make([]uint64, 0, len(fields)-1)
		for _, f := range fields[1:] {
			v, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				return CPUSample{}, fmt.Errorf("stat: parse %q: %w", f, err)
			}
			vals = append(vals, v)
		}
		// Indices: 0=user 1=nice 2=system 3=idle 4=iowait 5=irq 6=softirq 7=steal
		var total uint64
		for _, v := range vals {
			total += v
		}
		idle := vals[3]
		var iowait uint64
		if len(vals) > 4 {
			iowait = vals[4]
		}
		// guest/guest_nice (indices 8,9) are already accounted for inside
		// user/nice by the kernel, so leave Total as the raw sum.
		busy := total - idle - iowait
		return CPUSample{Busy: busy, Total: total}, nil
	}
	if err := sc.Err(); err != nil {
		return CPUSample{}, fmt.Errorf("scan stat: %w", err)
	}
	return CPUSample{}, fmt.Errorf("stat: aggregate cpu line not found")
}
