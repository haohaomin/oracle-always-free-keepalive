//go:build !linux && !darwin

package sysstat

// ReadMem is unsupported on this OS.
func ReadMem() (MemInfo, error) {
	return MemInfo{}, ErrUnsupported
}

// SampleCPU is unsupported on this OS.
func SampleCPU() (CPUSample, error) {
	return CPUSample{}, ErrUnsupported
}
