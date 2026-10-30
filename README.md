# keepwarm — Oracle Cloud (OCI) Always Free 实例保活 / 防闲置回收

[![CI](https://github.com/haohaomin/oracle-always-free-keepalive/actions/workflows/ci.yml/badge.svg)](https://github.com/haohaomin/oracle-always-free-keepalive/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go 1.23+](https://img.shields.io/badge/Go-1.23+-00ADD8.svg)](https://go.dev/)
[![Platform](https://img.shields.io/badge/platform-linux%2Farm64%20%7C%20linux%2Famd64-blue.svg)](#platform-support)

**Adaptive keepalive for Oracle Cloud Always Free instances** — stops OCI from
reclaiming your free-tier VM for idleness. 甲骨文云（Oracle Cloud / OCI）**永久免费**
服务器保活工具：按实例自身用量**自适应**补足内存或 CPU，防止被判定为闲置而回收。
同时支持 **VM.Standard.A1.Flex（ARM64）** 与 **VM.Standard.E2.1.Micro（AMD64）**，
systemd 常驻、纯标准库、无外部依赖、可交叉编译。

A tiny, **dependency-free** daemon that stops your Oracle Cloud (OCI) **Always Free** compute instance from being reclaimed for being idle — without burning CPU or bandwidth you don't need. It is **memory-first** (the cheapest, most reliable defeater) and **adaptive** (it measures your instance's own utilization and adds only the minimum extra load required).

## Why this exists

OCI periodically reclaims idle Always Free compute instances. Per Oracle's policy, an instance is considered **idle** only when, over a **rolling 7-day window**, **ALL THREE** of the following are below threshold (commonly cited as ~20%, historically 10%):

1. **CPU** utilization (95th percentile)
2. **Network** utilization
3. **Memory** utilization

Because it is an **AND** condition, keeping **any single one** of these dimensions above threshold is sufficient to prevent reclamation.

`keepwarm` exploits that: it keeps the **memory** dimension up by default, because holding memory is by far the cheapest defeater.

### Why memory-first?

- **Near-zero CPU cost.** Held memory just sits there. No spinning cores, no heat, no noisy neighbor impact.
- **Rock-solid 95th percentile.** A constant amount of resident memory means its utilization is flat and high — its 95th percentile is always above threshold, no matter when OCI samples.
- **No bandwidth waste.** It doesn't touch your network egress (which on free tier has its own limits).

CPU and network defeaters are available too, but they are **off by default**.

## How it differs from `NeverIdle`

The well-known [`layou233/NeverIdle`](https://github.com/layou233/NeverIdle) is dormant and takes a blunt approach. `keepwarm` is deliberately different:

| | NeverIdle | keepwarm |
|---|---|---|
| Primary mechanism | Burns CPU | **Holds memory** (cheapest, flat 95th-pct) |
| Load level | Blindly constant | **Adaptive** — measures your own usage, adds only the deficit |
| If the box is already busy | Still burns | **Does almost nothing** |
| Network keep-alive | Full Ookla speedtest (bandwidth-heavy) | Optional, tiny single GET (~10 MB), off by default |
| Dependencies | — | **Standard library only** (no `go.sum`, fully auditable) |
| Persistence | Often `nohup`/`screen` from `/tmp` (dies on reboot) | **Proper systemd unit + installer** |

## Install

> ⚠️ **先说清楚**：本仓库是 `0xarkstar/keepwarm` 的修复分支，上游**尚未**合并
> `v0.1.1-fix`（内存持有超额振荡修复，见 [Fixes](#fixes-over-0xarkstarkeepwarm)）。
> **Option A / Option B 安装的是本仓库的源码，含修复。**
> 下面那个"一键脚本"和 Option C 下载的是**上游 release 二进制**，
> **不含本次修复** —— 除非你就是要复现上游行为，否则不要用。

### Quickest — one-liner (no Go needed) ⚠️ 装的是上游版，无修复

Downloads the latest release binary for your architecture, verifies its SHA-256 checksum, installs it to `/usr/local/bin`, and enables the systemd service:

```sh
curl -fsSL https://raw.githubusercontent.com/0xarkstar/keepwarm/main/scripts/install.sh | sudo bash
```

Pin a version with `KEEPWARM_VERSION=v0.1.0 curl ... | sudo bash`. Uninstall with `curl ... | sudo bash -s -- --uninstall`.

### Option A — `go install`

```sh
go install github.com/0xarkstar/keepwarm@latest
```

The binary lands in `$(go env GOPATH)/bin/keepwarm`. Move it to `/usr/local/bin` and use the systemd path below for persistence.

### Option B — clone + install script (recommended for a server)

This builds (or uses a prebuilt binary), installs to `/usr/local/bin`, drops in the systemd unit, and enables the service:

```sh
git clone https://github.com/0xarkstar/keepwarm.git
cd keepwarm
sudo ./scripts/install.sh
```

Check it:

```sh
keepwarm -status
systemctl status keepwarm
journalctl -u keepwarm -f
```

Uninstall:

```sh
sudo ./scripts/install.sh --uninstall
```

### Option C — release binary (manual) ⚠️ 上游 release，无修复

> 这一节指向**上游**发布页，其二进制**不含** `v0.1.1-fix`。
> 想要含修复的二进制，请用 Option A/B 自行构建（或在 CI/本地交叉编译后分发）。

Download the binary for your architecture from the [Releases](https://github.com/0xarkstar/keepwarm/releases) page (`keepwarm-linux-arm64` for the OCI Ampere A1 shape, `keepwarm-linux-amd64` for the AMD shape) and verify it against `SHA256SUMS`:

```sh
curl -fsSLO https://github.com/0xarkstar/keepwarm/releases/latest/download/keepwarm-linux-arm64
chmod +x keepwarm-linux-arm64 && sudo mv keepwarm-linux-arm64 /usr/local/bin/keepwarm
```

(The one-liner above automates exactly this plus the systemd setup.)

### Option D — Docker

```sh
docker build -t keepwarm .
docker run -d --name keepwarm --restart unless-stopped keepwarm -mem 30%
```

> A container sees the host `/proc`, so detection works, but held memory counts against the container's cgroup limit. Size `-mem` (and any `--memory` limit) accordingly.

## Quick start

The defaults are sensible for the common case (keep memory at 30% of total RAM, check every 30 s):

```sh
keepwarm
```

See what it would do without changing anything:

```sh
keepwarm -status
```

```text
keepwarm v1.0.0 — status
  OS / CPUs:        linux, 4 logical cores
  Total RAM:        24.00 GiB
  Used RAM:         3.10 GiB (12.9%)
  Mem target:       30% (=7.20 GiB)
  Would hold:       4.10 GiB
  CPU floor:        disabled
  Network:          disabled
```

In that example the box is at 12.9% memory and the 30% target is 7.2 GiB, so keepwarm would hold the **4.1 GiB deficit** — and nothing more. If your own workload later uses more memory, keepwarm shrinks its buffer automatically.

## Flags

All flags can also be set via `KEEPWARM_*` environment variables (uppercase, dashes → underscores). **Flags take precedence over env vars, which take precedence over defaults.**

| Flag | Env var | Default | Description |
|---|---|---|---|
| `-mem` | `KEEPWARM_MEM` | `30%` | Target **total** memory utilization to maintain. Accepts a percent (`30%`) or an absolute size (`512MiB`, `2GiB`). Adaptive: holds only the deficit needed. **Primary mechanism, on by default.** |
| `-cpu` | `KEEPWARM_CPU` | `0.0` (off) | Target CPU utilization **floor** as a fraction of all cores `[0.0–1.0]`. Adaptive: if recent utilization is below the floor, does SHA-256 work for a computed slice of the interval; if organic load already meets it, does nothing. |
| `-net` | `KEEPWARM_NET` | `""` (off) | If set to a duration (e.g. `6h`), performs one lightweight outbound HTTPS GET that often to keep the network dimension non-idle. |
| `-net-url` | `KEEPWARM_NET_URL` | `https://speed.cloudflare.com/__down?bytes=10000000` | Endpoint for the network ping (~10 MB). Point it anywhere you like; the body is read and discarded. |
| `-net-timeout` | `KEEPWARM_NET_TIMEOUT` | `60s` | Per-request timeout for the network ping. |
| `-interval` | `KEEPWARM_INTERVAL` | `30s` | Control-loop tick. |
| `-priority` | `KEEPWARM_PRIORITY` | `19` | Process niceness on Linux (`-20`..`19`, `19` = lowest). Ensures real workloads always preempt keepwarm. No-op on non-Linux. |
| `-once` / `-status` | — | — | Print detected RAM, current CPU%/mem%, and what keepwarm **would** do, then exit. |
| `-verbose` | `KEEPWARM_VERBOSE` | `false` | Structured (`log/slog`) debug lines on every action. |
| `-version` | — | — | Print version and exit. |

### Examples

```sh
# Hold an absolute amount instead of a percentage:
keepwarm -mem 2GiB

# Also keep CPU above a 10% floor (only burns when the box is otherwise idle):
keepwarm -mem 30% -cpu 0.10

# Memory off-loaded to the network dimension only (hourly tiny GET):
keepwarm -mem 0% -net 1h

# Verbose, fast loop for debugging:
keepwarm -verbose -interval 5s -status
```

## How the adaptive math works

**Memory.** Each tick it reads total and used memory. Your *organic* usage is `used − whatKeepwarmHolds`. To reach the target it holds `target − organic`, clamped to `[0, total]`. Held pages are touched (one page size apart) so the kernel keeps them resident and counts them as used. If your workload grows, keepwarm shrinks; if it shrinks, keepwarm grows back.

Two details keep the loop honest (see [Fixes](#fixes-over-0xarkstarkeepwarm) below):

- **Resize dead-band.** Changes smaller than ~1.6 % of what is currently held (minimum 8 MiB) are ignored. Reacting to every few-MiB wobble in `MemAvailable` caused pointless reallocation churn.
- **Immediate page return.** When the holder resizes, the previous buffer is handed back to the OS straight away (`debug.FreeOSMemory`) instead of waiting for the Go runtime to scavenge it. While those stale pages stayed resident, the next `/proc/meminfo` read counted *old + new* at once, so `organic` looked far larger than it was, the deficit collapsed, and the holder re-grew — a feedback loop that drove observed utilization well above the configured target.

**CPU.** Each tick it samples `/proc/stat` twice (across the interval) to get current utilization `u`. To raise the *average* over the interval from `u` to the floor `f`, it pegs the CPU for a fraction of the interval:

```
burnFraction = (f − u) / (1 − u)
```

If `u ≥ f` it does nothing. The work is SHA-256 churn across one goroutine per core, at niceness 19 so it never starves real work.

## Platform support

- **Linux** (the OCI target) — full support: `/proc/meminfo`, `/proc/stat`, `Setpriority`.
- **darwin/arm64** (local dev) — memory detection via `sysctl`/`vm_stat`; CPU monitor reports *unavailable* (the memory mechanism is unaffected); priority is a no-op.
- Builds and runs on both; non-Linux degrades gracefully and says so.

## Build from source

```sh
make build      # host binary -> ./keepwarm
make test       # go test ./...
make vet        # go vet ./...
make cross      # static linux/amd64 + linux/arm64 -> ./dist
make clean
```

## Safety & ethics

`keepwarm` is intended **only** for keeping **your own, legitimately provisioned** OCI Always Free instance alive. It is designed to be **low-waste by construction** — it adds the minimum load needed to clear the idle threshold and nothing more, runs at the lowest scheduling priority, and never runs bandwidth-heavy speedtests. Please respect Oracle's terms of service and resource limits; don't use it to circumvent quotas or to keep resources you aren't entitled to.

## Fixes over `0xarkstar/keepwarm`

This tree is based on `0xarkstar/keepwarm` at commit
`24cde41bbf7522b77b4308829764e710f1d5ddc9` and adds the following.

### `v0.1.1-fix` — memory holder overshot its target (over-hold oscillation)

**Symptom.** With `-mem 30%` on a 12 GB (Ampere A1) instance, observed total
utilization did not settle at 30 %. It cycled between roughly **15 % and 47 %**,
and `RSS` of the holder repeatedly doubled (≈1.5 GiB → ≈3.1 GiB) before
collapsing. The instance was therefore holding ~3.1 GiB when only ~1.8 GiB was
needed — wasteful, and the configured target was never actually maintained.

**Root cause.** `MemoryHolder.Adjust` allocated a brand-new buffer on *every*
call, however small the delta, and the previous array stayed resident until the
Go runtime happened to collect and scavenge it. Two consequences compounded:

1. A one-off few-MiB wobble in `MemAvailable` was enough to trigger a full
   re-allocation of a multi-hundred-MiB buffer.
2. During the window where the old array was still resident, the next
   `/proc/meminfo` reading counted **old + new**. `TargetHeld` then computed
   `organic = used − held` from an inflated `used`, concluded the box was far
   busier than it really was, collapsed the deficit, and the holder re-grew on a
   later tick — repeating indefinitely.

**Fix** (`internal/waste/memory.go`):

- Added a **resize dead-band**: deltas below `max(8 MiB, held/64)` are ignored,
  so measurement noise no longer causes reallocation churn.
- **Shrink** now drops the old buffer, returns its pages to the OS, and only then
  allocates the replacement — so a shrink under memory pressure never
  transiently needs old + new.
- **Grow** now returns the replaced array's pages to the OS immediately after the
  swap, closing the residual half of the loop.
- Growth uses geometric capacity so incremental growth stops reallocating.
- `pageSize` is taken from `os.Getpagesize()` instead of a hard-coded 4096.
- `clampToInt` prevents `int` overflow when clamping huge targets on 32-bit.

**Verification** (Oracle Cloud `VM.Standard.A1.Flex`, 2 OCPU / 12 GB,
`-mem 30% -cpu 0 -interval 30s`, 400 s of sampling at 20 s intervals):

| | before | after |
|---|---|---|
| observed total memory utilization | 15.5 % ↔ 46.6 % | **29.9 % – 30.3 %** |
| holder RSS | 17 MiB ↔ 3191 MiB (doubling) | **1551 MiB, constant** |
| OOM events / restarts | 0 / 0 | 0 / 0 |

A unit-test regression guard (`TestMemoryHolderDeadbandImmuneToNoise`) asserts
that sub-dead-band deltas never reallocate the backing array; against the
unpatched implementation the equivalent assertion fails (a 67 109 888-byte
buffer was reallocated for a 1 KiB delta).

CPU and network modes are unchanged by this fix.

## 相关关键词 / Search terms

如果你是从搜索引擎来到这里的，以下说法指的都是同一件事：

`甲骨文云 保活` · `甲骨文 免费服务器 防回收` · `Oracle Cloud 保活` · `OCI 保活脚本` ·
`Oracle Always Free keepalive` · `OCI always free idle reclamation` · `oracle free tier keep alive` ·
`oracle cloud instance reclaimed idle` · `neveridle 替代` · `neveridle alternative` ·
`VM.Standard.A1.Flex` · `VM.Standard.E2.1.Micro` · `OCI Ampere A1 保活` · `甲骨文 ARM 保活` ·
`systemd keepalive` · `oracle cloud 闲置回收`

## License

[MIT](LICENSE) © 2026 0xarkstar · modifications in this tree © 2026 haohaomin
