package main

import (
	"testing"
	"time"
)

// fakeEnv builds an envLookup from a map.
func fakeEnv(m map[string]string) envLookup {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

func TestParseConfigDefaults(t *testing.T) {
	cfg, act, err := parseConfig(nil, fakeEnv(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if act != actionRun {
		t.Errorf("action = %v, want actionRun", act)
	}
	if !cfg.Mem.isPercent || cfg.Mem.percent != 30 {
		t.Errorf("default mem = %+v, want 30%%", cfg.Mem)
	}
	if cfg.CPUFloor != 0 {
		t.Errorf("default cpu floor = %v, want 0", cfg.CPUFloor)
	}
	if cfg.NetInterval != 0 {
		t.Errorf("default net interval = %v, want 0", cfg.NetInterval)
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("default interval = %v, want 30s", cfg.Interval)
	}
	if cfg.Priority != 19 {
		t.Errorf("default priority = %v, want 19", cfg.Priority)
	}
}

func TestParseConfigFlags(t *testing.T) {
	args := []string{
		"-mem", "512MiB",
		"-cpu", "0.5",
		"-net", "6h",
		"-net-url", "https://example.com/down",
		"-interval", "10s",
		"-priority", "10",
		"-verbose",
	}
	cfg, act, err := parseConfig(args, fakeEnv(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if act != actionRun {
		t.Errorf("action = %v, want actionRun", act)
	}
	if cfg.Mem.isPercent || cfg.Mem.bytes != 512*(1<<20) {
		t.Errorf("mem = %+v, want 512MiB", cfg.Mem)
	}
	if cfg.CPUFloor != 0.5 {
		t.Errorf("cpu floor = %v, want 0.5", cfg.CPUFloor)
	}
	if cfg.NetInterval != 6*time.Hour {
		t.Errorf("net interval = %v, want 6h", cfg.NetInterval)
	}
	if cfg.NetURL != "https://example.com/down" {
		t.Errorf("net url = %q", cfg.NetURL)
	}
	if cfg.Interval != 10*time.Second {
		t.Errorf("interval = %v, want 10s", cfg.Interval)
	}
	if cfg.Priority != 10 {
		t.Errorf("priority = %v, want 10", cfg.Priority)
	}
	if !cfg.Verbose {
		t.Errorf("verbose = false, want true")
	}
}

func TestParseConfigEnv(t *testing.T) {
	env := fakeEnv(map[string]string{
		"KEEPWARM_MEM":      "1GiB",
		"KEEPWARM_CPU":      "0.25",
		"KEEPWARM_INTERVAL": "5s",
		"KEEPWARM_VERBOSE":  "true",
	})
	cfg, _, err := parseConfig(nil, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Mem.bytes != 1<<30 {
		t.Errorf("mem = %+v, want 1GiB", cfg.Mem)
	}
	if cfg.CPUFloor != 0.25 {
		t.Errorf("cpu floor = %v, want 0.25", cfg.CPUFloor)
	}
	if cfg.Interval != 5*time.Second {
		t.Errorf("interval = %v, want 5s", cfg.Interval)
	}
	if !cfg.Verbose {
		t.Errorf("verbose = false, want true")
	}
}

func TestParseConfigFlagOverridesEnv(t *testing.T) {
	env := fakeEnv(map[string]string{"KEEPWARM_MEM": "1GiB"})
	cfg, _, err := parseConfig([]string{"-mem", "20%"}, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.Mem.isPercent || cfg.Mem.percent != 20 {
		t.Errorf("flag should override env: got %+v, want 20%%", cfg.Mem)
	}
}

func TestParseConfigActions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want action
	}{
		{"version", []string{"-version"}, actionVersion},
		{"status", []string{"-status"}, actionStatus},
		{"once", []string{"-once"}, actionOnce},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, act, err := parseConfig(tc.args, fakeEnv(nil))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if act != tc.want {
				t.Errorf("action = %v, want %v", act, tc.want)
			}
		})
	}
}

func TestParseConfigErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"bad mem", []string{"-mem", "abc"}},
		{"cpu out of range", []string{"-cpu", "1.5"}},
		{"bad net duration", []string{"-net", "notaduration"}},
		{"negative net", []string{"-net", "-6h"}},
		{"zero interval", []string{"-interval", "0s"}},
		{"unknown flag", []string{"-nope"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := parseConfig(tc.args, fakeEnv(nil)); err == nil {
				t.Errorf("parseConfig(%v) = nil error, want error", tc.args)
			}
		})
	}
}
