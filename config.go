package main

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/haohaomin/oracle-always-free-keepalive/internal/waste"
)

// Config holds the resolved runtime settings for the daemon.
type Config struct {
	Mem         memTarget     // target total memory utilization (primary)
	CPUFloor    float64       // target CPU utilization floor [0,1]; 0 = off
	NetInterval time.Duration // how often to ping the network; 0 = off
	NetURL      string        // endpoint for the network ping
	NetTimeout  time.Duration // per-request timeout for the network ping
	Interval    time.Duration // control-loop tick
	Priority    int           // process niceness (Linux)
	Verbose     bool          // debug-level action logs
}

// action selects a non-daemon mode (version/status/once) or actionRun.
type action int

const (
	actionRun action = iota
	actionVersion
	actionStatus
	actionOnce
)

// envLookup matches os.LookupEnv; injected for testability.
type envLookup func(key string) (string, bool)

// parseConfig parses CLI args and KEEPWARM_* environment variables into a
// Config. Flags take precedence over environment variables, which take
// precedence over defaults. It returns the selected action so main can branch
// to the version/status/once paths.
func parseConfig(args []string, env envLookup) (Config, action, error) {
	fs := flag.NewFlagSet("keepwarm", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we format our own error

	// Defaults, overridable by KEEPWARM_* env vars before flags are applied.
	var (
		memStr      = envOr(env, "KEEPWARM_MEM", "30%")
		cpuFloor    = envFloatOr(env, "KEEPWARM_CPU", 0.0)
		netStr      = envOr(env, "KEEPWARM_NET", "")
		netURL      = envOr(env, "KEEPWARM_NET_URL", waste.DefaultNetURL)
		netTimeout  = envDurationOr(env, "KEEPWARM_NET_TIMEOUT", 60*time.Second)
		interval    = envDurationOr(env, "KEEPWARM_INTERVAL", 30*time.Second)
		priority    = envIntOr(env, "KEEPWARM_PRIORITY", 19)
		verbose     = envBoolOr(env, "KEEPWARM_VERBOSE", false)
		showVersion bool
		showStatus  bool
		runOnce     bool
	)

	fs.StringVar(&memStr, "mem", memStr, `target TOTAL memory utilization, e.g. "30%", "512MiB", "2GiB"`)
	fs.Float64Var(&cpuFloor, "cpu", cpuFloor, "target CPU utilization floor [0.0-1.0] (0 = off)")
	fs.StringVar(&netStr, "net", netStr, `network ping interval as a duration, e.g. "6h" (empty = off)`)
	fs.StringVar(&netURL, "net-url", netURL, "URL for the lightweight network ping")
	fs.DurationVar(&netTimeout, "net-timeout", netTimeout, "per-request timeout for the network ping")
	fs.DurationVar(&interval, "interval", interval, "control-loop tick interval")
	fs.IntVar(&priority, "priority", priority, "process niceness on Linux (-20..19; 19 = lowest)")
	fs.BoolVar(&verbose, "verbose", verbose, "enable debug-level action logs")
	fs.BoolVar(&showVersion, "version", false, "print version and exit")
	fs.BoolVar(&showStatus, "status", false, "print detected stats and planned actions, then exit")
	fs.BoolVar(&runOnce, "once", false, "alias for -status: run a single diagnostic pass, then exit")

	if err := fs.Parse(args); err != nil {
		return Config{}, actionRun, fmt.Errorf("%w", err)
	}

	mem, err := parseMemTarget(memStr)
	if err != nil {
		return Config{}, actionRun, fmt.Errorf("invalid -mem: %w", err)
	}

	if cpuFloor < 0 || cpuFloor > 1 {
		return Config{}, actionRun, fmt.Errorf("invalid -cpu: %g out of range [0.0,1.0]", cpuFloor)
	}

	var netInterval time.Duration
	if netStr != "" {
		netInterval, err = time.ParseDuration(netStr)
		if err != nil {
			return Config{}, actionRun, fmt.Errorf("invalid -net duration %q: %w", netStr, err)
		}
		if netInterval <= 0 {
			return Config{}, actionRun, fmt.Errorf("invalid -net: duration must be positive")
		}
	}

	if interval <= 0 {
		return Config{}, actionRun, fmt.Errorf("invalid -interval: must be positive")
	}
	if netTimeout <= 0 {
		return Config{}, actionRun, fmt.Errorf("invalid -net-timeout: must be positive")
	}

	cfg := Config{
		Mem:         mem,
		CPUFloor:    cpuFloor,
		NetInterval: netInterval,
		NetURL:      netURL,
		NetTimeout:  netTimeout,
		Interval:    interval,
		Priority:    priority,
		Verbose:     verbose,
	}

	switch {
	case showVersion:
		return cfg, actionVersion, nil
	case showStatus:
		return cfg, actionStatus, nil
	case runOnce:
		return cfg, actionOnce, nil
	default:
		return cfg, actionRun, nil
	}
}

func envOr(env envLookup, key, def string) string {
	if v, ok := env(key); ok && v != "" {
		return v
	}
	return def
}

func envFloatOr(env envLookup, key string, def float64) float64 {
	if v, ok := env(key); ok && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envIntOr(env envLookup, key string, def int) int {
	if v, ok := env(key); ok && v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return def
}

func envBoolOr(env envLookup, key string, def bool) bool {
	if v, ok := env(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envDurationOr(env envLookup, key string, def time.Duration) time.Duration {
	if v, ok := env(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
