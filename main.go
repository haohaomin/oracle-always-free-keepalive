// Command keepwarm is a tiny, dependency-free daemon that keeps an OCI Always
// Free compute instance from being reclaimed for idleness. OCI flags an instance
// as idle only when, over a rolling 7-day window, ALL of CPU (95th percentile),
// network, and memory utilization fall below threshold. Because that is an AND
// condition, keeping ANY ONE dimension above threshold is sufficient.
//
// keepwarm is memory-first (holding memory is the cheapest, most reliable
// defeater) and adaptive (it measures the instance's own utilization and adds
// only the minimum extra load needed to stay above a floor).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/0xarkstar/keepwarm/internal/sysstat"
	"github.com/0xarkstar/keepwarm/internal/waste"
)

// Version is overridable at build time via -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	cfg, action, err := parseConfig(os.Args[1:], os.LookupEnv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "keepwarm:", err)
		os.Exit(2)
	}

	switch action {
	case actionVersion:
		fmt.Printf("keepwarm %s\n", Version)
		return
	case actionStatus:
		if err := printStatus(cfg, true); err != nil {
			fmt.Fprintln(os.Stderr, "keepwarm:", err)
			os.Exit(1)
		}
		return
	case actionOnce:
		if err := printStatus(cfg, false); err != nil {
			fmt.Fprintln(os.Stderr, "keepwarm:", err)
			os.Exit(1)
		}
		return
	}

	log := newLogger(cfg.Verbose)

	if err := setPriority(cfg.Priority); err != nil {
		// Niceness is best-effort; lacking permission to RAISE priority is
		// normal, and lowering should always succeed. Warn, do not abort.
		log.Warn("could not set process priority", "nice", cfg.Priority, "err", err)
	} else {
		log.Debug("process priority set", "nice", cfg.Priority)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg, log); err != nil {
		log.Error("keepwarm exited with error", "err", err)
		os.Exit(1)
	}
}

// newLogger builds a slog text logger. Verbose enables debug-level action logs.
func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	return slog.New(h)
}

// run executes the control loop until the context is cancelled, then releases
// all held resources.
func run(ctx context.Context, cfg Config, log *slog.Logger) error {
	log.Info("keepwarm starting",
		"version", Version,
		"mem_target", cfg.Mem.String(),
		"cpu_floor", cfg.CPUFloor,
		"net_interval", cfg.NetInterval.String(),
		"interval", cfg.Interval.String(),
		"cpus", sysstat.NumCPU(),
	)

	mem := waste.NewMemoryHolder(log)
	cpu := waste.NewCPUBurner(sysstat.NumCPU(), log)
	var net *waste.NetPinger
	if cfg.NetInterval > 0 {
		net = waste.NewNetPinger(cfg.NetURL, cfg.NetTimeout, log)
	}

	defer func() {
		mem.Release()
		log.Info("keepwarm stopped, memory released")
	}()

	// Prime the CPU sampler so the first tick has a baseline delta.
	prevCPU, cpuOK := primeCPU(log)

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	// A separate clock for network pings so they are infrequent regardless of
	// the (short) control interval.
	lastPing := time.Time{}

	// Run one tick immediately rather than waiting a full interval.
	prevCPU, cpuOK = tick(ctx, cfg, log, mem, cpu, net, prevCPU, cpuOK, &lastPing)

	for {
		select {
		case <-ctx.Done():
			log.Info("shutdown signal received")
			return nil
		case <-ticker.C:
			prevCPU, cpuOK = tick(ctx, cfg, log, mem, cpu, net, prevCPU, cpuOK, &lastPing)
		}
	}
}

// primeCPU takes an initial CPU sample (Linux only). cpuOK is false when CPU
// sampling is unsupported on this OS.
func primeCPU(log *slog.Logger) (sysstat.CPUSample, bool) {
	if !sysstat.SupportsCPU() {
		return sysstat.CPUSample{}, false
	}
	s, err := sysstat.SampleCPU()
	if err != nil {
		log.Warn("initial cpu sample failed; cpu floor disabled", "err", err)
		return sysstat.CPUSample{}, false
	}
	return s, true
}

// tick performs one control-loop iteration: adjust memory, optionally burn CPU,
// optionally ping the network. It returns the latest CPU sample and whether CPU
// sampling remains available.
func tick(
	ctx context.Context,
	cfg Config,
	log *slog.Logger,
	mem *waste.MemoryHolder,
	cpu *waste.CPUBurner,
	net *waste.NetPinger,
	prevCPU sysstat.CPUSample,
	cpuOK bool,
	lastPing *time.Time,
) (sysstat.CPUSample, bool) {
	// --- Memory (primary) ---
	mi, err := sysstat.ReadMem()
	if err != nil {
		log.Warn("memory read failed; skipping memory adjustment this tick", "err", err)
	} else {
		targetBytes := cfg.Mem.resolveBytes(mi.Total)
		held := uint64(mem.HeldBytes())
		want := waste.TargetHeld(targetBytes, mi.Used, held, mi.Total)
		mem.Adjust(want)
		log.Debug("memory tick",
			"total", mi.Total,
			"used", mi.Used,
			"used_pct", pct(mi.UsedFraction()),
			"target", targetBytes,
			"held", mem.HeldBytes(),
		)
	}

	// --- CPU (off by default) ---
	if cfg.CPUFloor > 0 && cpuOK {
		cur, err := sysstat.SampleCPU()
		if err != nil {
			log.Warn("cpu sample failed; disabling cpu floor", "err", err)
			cpuOK = false
		} else {
			util := sysstat.CPUUtilization(prevCPU, cur)
			d := waste.BurnDuration(util, cfg.CPUFloor, cfg.Interval)
			log.Debug("cpu tick", "util_pct", pct(util), "floor_pct", pct(cfg.CPUFloor), "burn", d.String())
			if d > 0 {
				cpu.Burn(ctx, d)
			}
			// Re-sample after burning so the next interval's delta is fresh.
			if after, err := sysstat.SampleCPU(); err == nil {
				prevCPU = after
			} else {
				prevCPU = cur
			}
		}
	}

	// --- Network (off by default) ---
	if net != nil && cfg.NetInterval > 0 {
		if lastPing.IsZero() || time.Since(*lastPing) >= cfg.NetInterval {
			if read, err := net.Ping(ctx); err == nil {
				log.Debug("network ping", "read_bytes", read)
			}
			*lastPing = time.Now()
		}
	}

	return prevCPU, cpuOK
}

// printStatus reads current stats and reports what keepwarm would do. When
// sampleCPU is true (the -status path) it takes a short CPU sample window.
func printStatus(cfg Config, sampleCPU bool) error {
	mi, memErr := sysstat.ReadMem()

	fmt.Printf("keepwarm %s — status\n", Version)
	fmt.Printf("  OS / CPUs:        %s, %d logical cores\n", osName(), sysstat.NumCPU())

	if memErr != nil {
		fmt.Printf("  Memory:           unavailable (%v)\n", memErr)
	} else {
		fmt.Printf("  Total RAM:        %s\n", humanBytes(mi.Total))
		fmt.Printf("  Used RAM:         %s (%s)\n", humanBytes(mi.Used), pct(mi.UsedFraction()))
		targetBytes := cfg.Mem.resolveBytes(mi.Total)
		want := waste.TargetHeld(targetBytes, mi.Used, 0, mi.Total)
		fmt.Printf("  Mem target:       %s (=%s)\n", cfg.Mem.String(), humanBytes(targetBytes))
		fmt.Printf("  Would hold:       %s\n", humanBytes(want))
	}

	if !sysstat.SupportsCPU() {
		fmt.Printf("  CPU monitor:      unavailable on %s (memory mechanism unaffected)\n", osName())
	} else if cfg.CPUFloor <= 0 {
		fmt.Printf("  CPU floor:        disabled\n")
	} else {
		util, err := sampleCPUWindow(sampleCPU)
		if err != nil {
			fmt.Printf("  CPU util:         unavailable (%v)\n", err)
		} else {
			d := waste.BurnDuration(util, cfg.CPUFloor, cfg.Interval)
			fmt.Printf("  CPU util:         %s\n", pct(util))
			fmt.Printf("  CPU floor:        %s\n", pct(cfg.CPUFloor))
			fmt.Printf("  Would burn:       %s per %s interval\n", d.String(), cfg.Interval.String())
		}
	}

	if cfg.NetInterval > 0 {
		fmt.Printf("  Network:          GET %s every %s\n", cfg.NetURL, cfg.NetInterval.String())
	} else {
		fmt.Printf("  Network:          disabled\n")
	}

	return nil
}

// sampleCPUWindow takes two CPU samples ~250ms apart and returns utilization.
// When window is false it returns a single-shot estimate (delta from a zeroed
// baseline is meaningless, so it still does a brief sample to be useful).
func sampleCPUWindow(window bool) (float64, error) {
	first, err := sysstat.SampleCPU()
	if err != nil {
		return 0, err
	}
	d := 250 * time.Millisecond
	if !window {
		d = 100 * time.Millisecond
	}
	time.Sleep(d)
	second, err := sysstat.SampleCPU()
	if err != nil {
		return 0, err
	}
	return sysstat.CPUUtilization(first, second), nil
}

// pct formats a [0,1] fraction as a percentage string.
func pct(f float64) string { return fmt.Sprintf("%.1f%%", f*100) }

// osName returns the runtime OS for status output.
func osName() string { return runtime.GOOS }
