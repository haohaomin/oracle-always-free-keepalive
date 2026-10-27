//go:build linux

package sysstat

import "os"

// ReadMem reads total and used physical memory from /proc/meminfo.
func ReadMem() (MemInfo, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return MemInfo{}, errMem(err)
	}
	mi, err := parseMeminfo(string(data))
	if err != nil {
		return MemInfo{}, errMem(err)
	}
	return mi, nil
}

// SampleCPU reads one aggregate CPU sample from /proc/stat. Take two samples
// over an interval and pass them to CPUUtilization.
func SampleCPU() (CPUSample, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return CPUSample{}, err
	}
	return parseStatCPU(string(data))
}
