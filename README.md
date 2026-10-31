# keepwarm — 甲骨文云 / Oracle Cloud (OCI) Always Free 实例保活 · 防闲置回收

**简体中文** | [English](README.en.md)

[![CI](https://github.com/haohaomin/oracle-always-free-keepalive/actions/workflows/ci.yml/badge.svg)](https://github.com/haohaomin/oracle-always-free-keepalive/actions/workflows/ci.yml)
[![GitHub stars](https://img.shields.io/github/stars/haohaomin/oracle-always-free-keepalive?style=social)](https://github.com/haohaomin/oracle-always-free-keepalive/stargazers)
[![Release](https://img.shields.io/github/v/release/haohaomin/oracle-always-free-keepalive)](https://github.com/haohaomin/oracle-always-free-keepalive/releases)
[![Downloads](https://img.shields.io/github/downloads/haohaomin/oracle-always-free-keepalive/total)](https://github.com/haohaomin/oracle-always-free-keepalive/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go 1.23+](https://img.shields.io/badge/Go-1.23+-00ADD8.svg)](https://go.dev/)
[![Platform](https://img.shields.io/badge/platform-linux%2Farm64%20%7C%20linux%2Famd64-blue.svg)](#平台支持)

甲骨文云（Oracle Cloud / OCI）**永久免费（Always Free）**服务器保活工具：按实例自身用量
**自适应**补足内存或 CPU，防止免费实例被判定为闲置而回收。同时支持
**VM.Standard.A1.Flex（ARM64，Ampere）** 与 **VM.Standard.E2.1.Micro（AMD64）**，
systemd 常驻、纯 Go 标准库、零外部依赖、可静态交叉编译。

> Adaptive keepalive for Oracle Cloud (OCI) Always Free instances — stops idle
> reclamation without burning CPU or bandwidth you don't need. Static, dependency-free.

## 目录

- [为什么需要它](#为什么需要它)
- [为什么优先用内存](#为什么优先用内存)
- [与 NeverIdle 的区别](#与-neveridle-的区别)
- [安装](#安装)
- [快速开始](#快速开始)
- [参数](#参数)
- [自适应原理](#自适应原理)
- [平台支持](#平台支持)
- [从源码构建](#从源码构建)
- [安全与合理使用](#安全与合理使用)
- [更新记录](#更新记录)
- [相关关键词](#相关关键词)
- [Star 趋势](#star-趋势)

## 为什么需要它

OCI 会周期性回收闲置的 Always Free 计算实例。按 Oracle 的政策，只有在**滚动 7 天窗口**内，
下面**三项全部**低于阈值（常见说法约 20%，早期文档写 10%）时，实例才算「闲置」：

1. **CPU** 利用率（95 分位）
2. **网络** 利用率
3. **内存** 利用率

因为这是 **AND** 条件，所以**任意一项**高于阈值即可避免回收。

`keepwarm` 就利用这一点：默认把**内存**这一维顶住，因为占住内存是代价最低的做法。

## 为什么优先用内存

- **CPU 代价近乎为零。** 占住的内存只是静静地待着，不空转核心、不发热、不打扰邻居。
- **95 分位极稳。** 常驻内存量恒定，利用率曲线平坦且高，无论 OCI 什么时候采样，95 分位都在阈值之上。
- **不浪费带宽。** 完全不碰出网流量（免费额度对流量另有约束）。

CPU 与网络两种保活方式也实现了，但**默认关闭**。

## 与 NeverIdle 的区别

知名的 [`layou233/NeverIdle`](https://github.com/layou233/NeverIdle) 已长期不更新，思路比较粗暴。
`keepwarm` 有意做得不同：

| | NeverIdle | keepwarm |
|---|---|---|
| 主要机制 | 烧 CPU | **占内存**（最便宜、95 分位平坦） |
| 负载强度 | 固定不变 | **自适应**：先量自己的用量，只补差额 |
| 机器本来就忙时 | 照样烧 | **几乎什么都不做** |
| 网络保活 | 完整 Ookla 测速（吃带宽） | 可选，一次极小的 GET（约 10 MB），默认关 |
| 依赖 | — | **仅标准库**（无 `go.sum`，可完整审计） |
| 持久化 | 常见 `nohup`/`screen` 挂在 `/tmp`（重启即失） | **正规 systemd unit + 安装脚本** |

## 安装

> **关于本项目**：`keepwarm` 的维护分支，基于 MIT 许可的原始项目（作者 `0xarkstar`）
> 继续开发，并已修复内存持有超额振荡问题（详见[更新记录](#更新记录)）。
> **下面的安装方式都指向本仓库的发布产物。**
>
> **发布策略**：每次向 `main` 推送**非文档**改动（源码 / `go.mod` / unit / `install.sh` /
> workflow），CI 会跑 `vet` + `test`，然后交叉编译 `linux/arm64`、`linux/amd64`、`darwin/arm64`，
> 生成 `SHA256SUMS`，并以**日期标签**自动发布：`vYYYY.MM.DD`，
> 同一天多次发布则为 `vYYYY.MM.DD.2`、`.3`…… 纯文档改动不会触发发布。
> 产物为**静态链接**（`CGO_ENABLED=0`），不依赖目标机 glibc，
> 可直接在 Oracle Linux 9（glibc 2.34）与 Ubuntu 24.04（glibc 2.39）上运行。

### 最快：一键安装（无需 Go）

下载**本仓库**最新 release 二进制，校验 `SHA256SUMS`，
装到 `/usr/local/bin`，并启用 systemd 服务：

```sh
curl -fsSL https://raw.githubusercontent.com/haohaomin/oracle-always-free-keepalive/main/scripts/install.sh | sudo bash
```

固定某个日期版本：`KEEPWARM_VERSION=v2026.10.30 curl ... | sudo bash`
卸载：`curl ... | sudo bash -s -- --uninstall`

### 方式 A — `go install`

```sh
go install github.com/haohaomin/oracle-always-free-keepalive@latest
```

二进制名跟随模块名，装好后重命名为 `keepwarm`：

```sh
sudo install -m 0755 "$(go env GOPATH)/bin/oracle-always-free-keepalive" /usr/local/bin/keepwarm
```

再按下面的 systemd 方式配置常驻。

### 方式 B — 克隆后跑安装脚本（服务器上推荐）

脚本会构建（或使用预编译产物）、装到 `/usr/local/bin`、放入 systemd unit 并启用服务：

```sh
git clone https://github.com/haohaomin/oracle-always-free-keepalive.git
cd oracle-always-free-keepalive
sudo ./scripts/install.sh
```

检查：

```sh
keepwarm -status
systemctl status keepwarm
journalctl -u keepwarm -f
```

卸载：

```sh
sudo ./scripts/install.sh --uninstall
```

### 方式 C — 手动下载 release 二进制

从 **[本仓库 Releases](https://github.com/haohaomin/oracle-always-free-keepalive/releases)** 下载对应架构
（A1 用 `keepwarm-linux-arm64`，E2 Micro 用 `keepwarm-linux-amd64`），并核对 `SHA256SUMS`：

```sh
curl -fsSLO https://github.com/haohaomin/oracle-always-free-keepalive/releases/latest/download/keepwarm-linux-arm64
curl -fsSLO https://github.com/haohaomin/oracle-always-free-keepalive/releases/latest/download/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
chmod +x keepwarm-linux-arm64 && sudo mv keepwarm-linux-arm64 /usr/local/bin/keepwarm
```

（上面的一键脚本就是把这个流程加上 systemd 配置自动做完。）

### 方式 D — Docker

```sh
docker build -t keepwarm .
docker run -d --name keepwarm --restart unless-stopped keepwarm -mem 30%
```

> 容器里能看到宿主机的 `/proc`，所以检测是准的；但占住的内存会算进容器的 cgroup 限制。
> 请据此设置 `-mem` 与 `--memory`。

## 快速开始

默认参数适用于最常见场景（把总内存利用率维持在 30%，每 30 秒检查一次）：

```sh
keepwarm
```

只看它会做什么、不改动任何东西：

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

上例中机器内存利用率 12.9%、目标 30%（=7.2 GiB），所以 keepwarm 只占住 **4.1 GiB 的差额**，
多一点都不占。之后你自己的业务用得更多，它会自动缩回去。

## 参数

所有参数也可用 `KEEPWARM_*` 环境变量设置（大写，短横线换下划线）。
**优先级：命令行参数 > 环境变量 > 默认值。**

| 参数 | 环境变量 | 默认值 | 说明 |
|---|---|---|---|
| `-mem` | `KEEPWARM_MEM` | `30%` | 要维持的**总**内存利用率目标。可写百分比（`30%`）或绝对大小（`512MiB`、`2GiB`）。自适应：只占差额。**主机制，默认开启。** |
| `-cpu` | `KEEPWARM_CPU` | `0.0`（关） | CPU 利用率**下限**，取全部核心的比例 `[0.0–1.0]`。自适应：近期利用率低于下限时，按算出的时间片做 SHA-256 运算；有机负载已达下限则什么都不做。 |
| `-net` | `KEEPWARM_NET` | `""`（关） | 设为时长（如 `6h`）后，按该频率发一次轻量 HTTPS GET，让网络维度保持非闲置。 |
| `-net-url` | `KEEPWARM_NET_URL` | `https://speed.cloudflare.com/__down?bytes=10000000` | 网络探测的目标地址（约 10 MB），可改成任意地址；响应体读完即丢。 |
| `-net-timeout` | `KEEPWARM_NET_TIMEOUT` | `60s` | 单次网络探测的超时。 |
| `-interval` | `KEEPWARM_INTERVAL` | `30s` | 控制循环周期。 |
| `-priority` | `KEEPWARM_PRIORITY` | `19` | Linux 下的进程 nice 值（`-20`..`19`，`19` 最低）。保证真实业务永远抢占它。非 Linux 无效果。 |
| `-once` / `-status` | — | — | 打印检测到的内存、当前 CPU/内存占比、以及它**将会**做什么，然后退出。 |
| `-verbose` | `KEEPWARM_VERBOSE` | `false` | 每次动作输出结构化（`log/slog`）调试日志。 |
| `-version` | — | — | 打印版本后退出。 |

### 示例

```sh
# 用绝对大小而不是百分比：
keepwarm -mem 2GiB

# 同时把 CPU 托到 10% 下限（只在机器空闲时才烧）：
keepwarm -mem 30% -cpu 0.10

# 只用网络维度保活（每小时一次极小 GET）：
keepwarm -mem 0% -net 1h

# 调试用的详细日志 + 快速循环：
keepwarm -verbose -interval 5s -status
```

## 自适应原理

**内存。** 每个周期读一次总内存与已用内存。你的**有机用量**是 `used − 它当前占住的量`。
为了达到目标，它需要占住 `target − organic`，并夹到 `[0, total]` 区间内。
占住的页会按页大小写一遍，让内核保持其常驻并计入已用。
业务涨了它就让，业务落了它再补回去。

有两个细节保证这个回路不跑偏（详见[更新记录](#更新记录)）：

- **重分配死区。** 小于当前持有量约 1.6%（且不低于 8 MiB）的变化直接忽略。
  原先对 `MemAvailable` 上几 MiB 的抖动也做出反应，导致毫无意义的重分配抖动。
- **立即归还页。** 重分配时，旧缓冲区**立刻**交还操作系统（`debug.FreeOSMemory`），
  而不是等 Go 运行时慢慢回收。否则那些「已释放但仍驻留」的页会被下一次 `/proc/meminfo`
  读成 **旧 + 新** 两份，于是 `organic` 被高估、缺口算成 0、随后又重新长回去——这个正反馈
  会把实测利用率推到远超配置目标。

**CPU。** 每个周期对 `/proc/stat` 采样两次（跨一个周期）得到当前利用率 `u`。
要把该周期的**平均**利用率从 `u` 提到下限 `f`，它需要打满 CPU 的时间占比为：

```
burnFraction = (f − u) / (1 − u)
```

若 `u ≥ f` 则完全不动。计算任务是每核心一个 goroutine 跑 SHA-256，nice 19，绝不饿死真实业务。

## 平台支持

- **Linux**（OCI 目标平台）— 完整支持：`/proc/meminfo`、`/proc/stat`、`Setpriority`。
- **darwin/arm64**（本地开发）— 内存检测走 `sysctl`/`vm_stat`；CPU 监控报告**不可用**
  （不影响内存机制）；nice 为空操作。
- 两个平台都能构建运行；非 Linux 会优雅降级并明确说明。

## 从源码构建

```sh
make build      # 本机二进制 -> ./keepwarm
make test       # go test ./...
make vet        # go vet ./...
make cross      # 静态 linux/amd64 + linux/arm64 -> ./dist
make clean
```

## 安全与合理使用

`keepwarm` **仅**用于让你**自己的、合法开通**的 OCI Always Free 实例不被回收。
它在设计上就是**低浪费**的：只补到刚好越过闲置阈值、以最低调度优先级运行、
从不跑吃带宽的测速。请遵守 Oracle 的服务条款与资源额度，
不要用它绕过配额，或保留你并不应得的资源。

## 更新记录

本仓库在 MIT 许可的 `keepwarm`（原始作者 `0xarkstar`）之上持续维护，
基线为 commit `24cde41bbf7522b77b4308829764e710f1d5ddc9`，当前包含以下改动。

### `v0.1.1-fix` — 内存持有量超出目标（超调振荡）

**现象。** 在 12 GB 的 Ampere A1 实例上用 `-mem 30%`，实测总利用率**并不稳定在 30%**，
而是在大约 **15% ↔ 47%** 之间来回跳；持有者的 `RSS` 反复翻倍（约 1.5 GiB → 约 3.1 GiB）
再塌回去。也就是说：只需要占 1.8 GiB 时它占了 3.1 GiB——既浪费，配置的目标也从没真正兑现过。

**根因。** `MemoryHolder.Adjust` 对**每一次**调用都分配一块全新的缓冲区（哪怕只差几 MiB），
而旧数组要等 Go 运行时碰巧回收并归还才会消失。两个后果叠加：

1. `MemAvailable` 上一次性几 MiB 的抖动，就足以触发一次几百 MiB 的整块重分配。
2. 在旧数组仍驻留的那个窗口里，下一次 `/proc/meminfo` 读到的是 **旧 + 新**。
   `TargetHeld` 于是用被高估的 `used` 去算 `organic = used − held`，得出「机器比实际忙得多」，
   缺口塌成 0，持有者随后又长回去——如此反复，没有尽头。

**修复**（`internal/waste/memory.go`）：

- 新增**重分配死区**：小于 `max(8 MiB, held/64)` 的变化忽略，测量噪声不再引起重分配抖动。
- **收缩**时先丢弃旧缓冲区、把页还给操作系统，然后再分配新的——内存紧张时收缩不会瞬时需要「旧 + 新」。
- **增长**时在替换后立刻归还被换下的数组的页，堵住这个回路的另一半。
- 增长采用几何扩容，避免逐周期重分配。
- `pageSize` 改用 `os.Getpagesize()`，不再硬编码 4096。
- `clampToInt` 防止 32 位平台上夹取大目标时 `int` 溢出。

**验证**（Oracle Cloud `VM.Standard.A1.Flex`，2 OCPU / 12 GB，
`-mem 30% -cpu 0 -interval 30s`，400 秒、每 20 秒采样一次）：

| | 修复前 | 修复后 |
|---|---|---|
| 实测总内存利用率 | 15.5% ↔ 46.6% | **29.9% – 30.3%** |
| 持有者 RSS | 17 MiB ↔ 3191 MiB（翻倍） | **1551 MiB，恒定** |
| OOM 次数 / 重启 | 0 / 0 | 0 / 0 |

单测回归护栏 `TestMemoryHolderDeadbandImmuneToNoise` 断言：死区内的变化绝不重分配底层数组；
在未打补丁的实现上该断言会失败（实测为 1 KiB 的变化重分配了 67,109,888 字节的缓冲区）。

本修复不影响 CPU 与网络两种模式。

## 相关关键词

如果你是从搜索引擎来到这里的，以下说法指的都是同一件事：

`甲骨文云 保活` · `甲骨文 免费服务器 防回收` · `Oracle Cloud 保活` · `OCI 保活脚本` ·
`Oracle Always Free keepalive` · `OCI always free idle reclamation` · `oracle free tier keep alive` ·
`oracle cloud instance reclaimed idle` · `neveridle 替代` · `neveridle alternative` ·
`VM.Standard.A1.Flex` · `VM.Standard.E2.1.Micro` · `OCI Ampere A1 保活` · `甲骨文 ARM 保活` ·
`systemd keepalive` · `oracle cloud 闲置回收`

## Star 趋势

如果这个项目帮你保住了机器，点个 Star 就是最好的支持 ⭐

[![Star History Chart](https://api.star-history.com/svg?repos=haohaomin/oracle-always-free-keepalive&type=Date)](https://star-history.com/#haohaomin/oracle-always-free-keepalive&Date)

## License

[MIT](LICENSE) © 2026 haohaomin · 原始项目 © 2026 0xarkstar
