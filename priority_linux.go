//go:build linux

package main

import (
	"fmt"
	"syscall"
)

// setPriority lowers (or raises) this process's scheduling niceness. nice ranges
// from -20 (highest priority) to 19 (lowest). keepwarm defaults to 19 so any
// real workload always preempts it. Uses the PRIO_PROCESS target with pid 0
// (the calling process).
func setPriority(nice int) error {
	if nice < -20 {
		nice = -20
	}
	if nice > 19 {
		nice = 19
	}
	if err := syscall.Setpriority(syscall.PRIO_PROCESS, 0, nice); err != nil {
		return fmt.Errorf("setpriority(nice=%d): %w", nice, err)
	}
	return nil
}
