# hy2route Xray OOM 排查与内存治理记录

- 记录日期：2026-07-18（America/Los_Angeles）
- 设备：Xiaomi Mi Router WR30U
- 系统：OpenWrt 23.05.0，Linux 5.15.134，arm64
- 运行时：Xray 26.2.6，Go 1.26.1
- 时间基准：本文设备日志均为 2026-07-19 UTC

## 1. 结论摘要

本次故障的直接原因是 Xray 实际物理内存增长到约 150 MiB，在约 236 MiB 可管理内存且没有 Swap 的路由器上触发全局 OOM，随后被内核强制杀死。

排查先移除了 Xray 中与 nft `bypass4` / `china4` 重复的 `geoip:private` 和 `geoip:cn` 判断。实测该项只能降低约 0.8～0.9 MiB 空载 RSS、约 4 MiB 启动峰值，不是 150 MiB OOM 的主要来源。

随后增加三层内存治理：

1. 将 `keepAlivePeriod` 设为 `0`，允许空闲 HY2/QUIC 连接按 `maxIdleTimeout=60` 秒关闭。
2. 为 Xray 设置 `GOMEMLIMIT=80MiB`，要求 Go 运行时更积极地控制托管内存。
3. 增加 RSS 看门狗：每 30 秒读取一次 Xray `VmRSS`，连续 3 次超过 110 MiB 才记录错误并重启 Xray。

上线后约 6 分钟的实际流量观察中，Xray RSS 峰值为 55,924 KiB，随后在 PID 不变、没有重启的情况下回落到 36,320 KiB，证明 Go GC/后台内存归还机制在工作。观察窗口内没有新增 OOM、看门狗告警或进程重启。该结果只能证明短期运行正常，仍需覆盖数小时和高流量场景。

## 2. 故障时间线

| UTC 时间 | 事件 | 证据 |
| --- | --- | --- |
| 02:43:39 | Xray 触发全局 OOM | `xray invoked oom-killer` |
| 02:43:39 | 内核杀死 Xray PID 3958 | `Out of memory: Killed process 3958 (xray)` |
| 02:45:16 | 人工关闭 hy2route | `user.notice hy2route: stopped` |
| 03:33:36 | 完成治理后重新启用 hy2route | `user.notice hy2route: started` |
| 03:36:07～03:42:12 | 连续采样实际运行内存 | PID 6658 始终不变，无新增 OOM/重启 |

OOM 时的关键数据：

```text
managed RAM: 241768 KiB
swap: 0 KiB
xray total-vm: 1427944 KiB
xray anon-rss: 150200 KiB
free memory: about 20320 KiB
```

`total-vm` 是 Go 在 64 位平台上的虚拟地址空间预留，不能直接当作物理内存使用量；本次判断以 RSS、匿名 RSS 和内核 OOM 记录为准。

## 3. 原始实现与风险

hy2route 使用 Xray 同时承担以下职责：

- HY2/QUIC 中继；
- 经 HY2 再连接 SOCKS/HTTP 落地的双跳链路；
- TCP redirect 与 UDP TProxy；
- HTTP、TLS、QUIC 嗅探；
- DNS 转发；
- 自定义域名/IP 路由。

原始关键配置：

```text
udpIdleTimeout=60
maxIdleTimeout=60
keepAlivePeriod=15
procd respawn=3600 5 5
GOMEMLIMIT=未设置
```

各机制的实际含义：

- `udpIdleTimeout=60` 只回收连续 60 秒无流量的单个 UDP 会话。
- `maxIdleTimeout=60` 允许关闭空闲 QUIC 连接，但 `keepAlivePeriod=15` 会持续发送保活包，使可达链路通常不会进入真正空闲。
- Go GC 会回收不可达对象并逐步归还内存，但不会按时间清空仍被活动会话、缓存或缓冲区引用的对象。
- `procd_set_param respawn 3600 5 5` 是异常退出后的重启策略，不是每小时定时重启。

因此原实现有会话级回收和 GC，但没有进程级内存预算，也没有在内核 OOM 之前主动重启的保护线。

## 4. 重复 GeoIP 路由分析

nft 在流量进入 Xray 前已经执行：

- 私网、保留地址和中继/落地地址进入 `bypass4` 后直接返回；
- 中国 IPv4 地址命中 `china4` 后直接返回；
- 强制代理与强制直连使用独立 nft set。

Xray 路由又包含：

```json
{ "ip": ["geoip:private"], "outboundTag": "direct" }
{ "ip": ["geoip:cn"], "outboundTag": "direct" }
```

透明代理主路径中这两条判断与 nft 前置路由重复，因此从 Xray 配置生成器中移除。自定义域名/IP 规则、DNS 专用规则和最终双跳规则保持不变。

### 4.1 空载 A/B 数据

测试方法：服务保持关闭，仅分别用旧/新配置启动 Xray，不安装 nft、不修改策略路由、不重启 dnsmasq；等待进程稳定后读取 `/proc/<pid>/status`。

| 指标 | 旧配置 | 新配置 | 差值 |
| --- | ---: | ---: | ---: |
| 稳态 RSS（第一轮） | 18,284 KiB | 17,400 KiB | 884 KiB |
| 稳态 RSS（复测） | 18,188 KiB | 17,400 KiB | 788 KiB |
| 启动高水位 RSS | 21,908 KiB | 17,948 KiB | 3,960 KiB |
| 虚拟内存峰值 | 1,317,832 KiB | 1,290,536 KiB | 27,296 KiB |

结论：移除重复 GeoIP 判断是合理精简，但物理内存收益主要是个位数 MiB，不能解释或单独解决 150 MiB OOM。

## 5. 已部署的治理措施

部署文件位于路由器，不在本仓库源代码中：

```text
/usr/libexec/hy2route/generate.uc
/usr/libexec/hy2route/run-xray.sh
/etc/init.d/hy2route
/etc/config/hy2route
```

### 5.1 允许空闲 QUIC 连接关闭

生成器将 `keep_alive_period` 校验范围从 `2..60` 调整为 `0..60`，默认值改为 `0`；UCI 当前值同步设为：

```text
hy2route.relay.keep_alive_period=0
```

保留 `max_idle_timeout=60`，因此没有业务流量时不再用主动保活无限维持底层 QUIC 连接。代价是下一次流量可能需要重新握手。

### 5.2 设置 Go 运行时软内存上限

procd 实例增加：

```sh
procd_set_param env XRAY_LOCATION_ASSET=/usr/share/v2ray GOMEMLIMIT=80MiB
```

`GOMEMLIMIT` 是 Go 运行时软限制，不是 Linux 硬内存上限。它不包含所有文件映射和内核代持内存；当存活对象本身超过限制或 GC 为避免持续抖动时，进程仍可能超过 80 MiB。

### 5.3 RSS 看门狗与自动拉起

procd 不再直接执行 Xray，而是执行 `/usr/libexec/hy2route/run-xray.sh`。包装脚本启动 Xray 子进程并监控：

```text
RSS_LIMIT_KB=112640       # 110 MiB
CHECK_INTERVAL_SECONDS=30
BREACH_LIMIT=3
```

处理规则：

1. 每秒确认 Xray 子进程仍存活，每 30 秒读取一次 `VmRSS`。
2. RSS 低于或等于阈值时，连续超限计数清零。
3. 每次超限记录 warning，包含 `stage`、`label`、`elapsed`、`rss_kb`、`limit_kb`、`breaches`、`reason`。
4. 连续 3 次超限后记录 error，终止 Xray，包装脚本退出 `1`。
5. procd 等待 5 秒后重新拉起包装脚本和 Xray。

关键日志形态：

```text
stage=memory-watch label=xray-rss elapsed=... rss_kb=... limit_kb=112640 breaches=... reason=threshold-exceeded
stage=memory-watch label=xray-rss elapsed=... rss_kb=... limit_kb=112640 breaches=3 reason=restart-after-consecutive-thresholds
stage=process-watch label=xray elapsed=... exit_status=... reason=process-exited
```

如果 Xray 先被内核 OOM Killer 杀死，包装脚本会在约 1 秒内发现子进程退出并结束；procd 再等待 5 秒拉起，单次故障预计约 5～6 秒恢复。若一小时内反复崩溃并超过 `respawn 3600 5 5` 的重试限制，procd 会停止继续拉起，避免无限重启循环。

## 6. 部署验证

### 6.1 配置与语法

| 验证 | 结果 |
| --- | --- |
| 生成配置中的 `keepAlivePeriod` | `0` |
| 生成配置中的 `geoip:` 引用 | 无 |
| `xray run -test` | `Configuration OK`，exit 0 |
| `nft -c -f` | exit 0 |
| `dnsmasq --test` | `syntax check OK`，exit 0 |
| init 与看门狗 `sh -n` | exit 0 |

### 6.2 看门狗故障注入

测试时仅通过环境变量把阈值临时设为 1 KiB、采样周期设为 1 秒、连续次数设为 2，不修改正式配置：

```text
elapsed=1s rss_kb=17240 limit_kb=1 breaches=1 reason=threshold-exceeded
elapsed=2s rss_kb=17240 limit_kb=1 breaches=2 reason=threshold-exceeded
elapsed=2s rss_kb=17240 limit_kb=1 breaches=2 reason=restart-after-consecutive-thresholds
```

包装脚本按预期返回 `1`，测试后没有残留 Xray。停止信号测试中，包装脚本和 Xray 在 1 秒内退出；Xray 子进程环境确认包含：

```text
GOMEMLIMIT=80MiB
XRAY_LOCATION_ASSET=/usr/share/v2ray
```

测试日志中的 `limit_kb=1` 是故障注入证据，正式阈值仍为 `112640`。

## 7. 启用后的短期监控

hy2route 于 03:33:36 启用。03:36:07～03:42:12 每 30 秒读取 Xray RSS、匿名 RSS、线程数、FD 数、系统可用内存、conntrack 数量、看门狗日志计数和 OOM 计数。

关键采样：

| UTC 时间 | PID | RSS | 匿名 RSS | conntrack | 系统可用内存 | 说明 |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| 03:35:26 | 6658 | 23,324 KiB | 9,308 KiB | - | 119,648 KiB | 启动后初检 |
| 03:36:07 | 6658 | 32,488 KiB | 18,412 KiB | 600 | 111,100 KiB | 实际流量进入 |
| 03:37:08 | 6658 | 46,188 KiB | 32,080 KiB | 992 | 98,824 KiB | conntrack 峰值 |
| 03:38:38 | 6658 | 55,548 KiB | 41,428 KiB | 251 | 88,604 KiB | 连接下降、内存暂未回落 |
| 03:39:38 | 6658 | 55,740 KiB | 41,620 KiB | 139 | 86,072 KiB | RSS 平台期 |
| 03:40:38 | 6658 | 36,320 KiB | 22,200 KiB | 133 | 96,056 KiB | 同 PID 主动回收约 19 MiB |
| 03:42:12 | 6658 | 43,424 KiB | 29,304 KiB | - | 91,044 KiB | 观察结束 |

观察结果：

- PID 始终为 6658，未发生进程重启。
- `VmHWM` 为 55,924 KiB，未接近 110 MiB 看门狗阈值。
- conntrack 从 992 回落后，RSS 延迟约 2～3 分钟才明显下降，符合 GC/后台归还而非即时逐连接释放。
- 没有新增 `threshold-exceeded`、`restart-after-consecutive-thresholds` 或 `process-exited` 日志。
- OOM 记录仍只有优化前 02:43:39 的一次。
- nft 表、优先级 10066 的策略路由和 dnsmasq 片段均存在，DNS 解析正常。

## 8. 运维检查

### 8.1 服务与内存

```sh
/etc/init.d/hy2route status
ubus call service list '{"name":"hy2route"}'
pidof xray
grep -E 'VmHWM|VmRSS|RssAnon|RssFile|Threads' /proc/$(pidof xray)/status
free -m
```

### 8.2 看门狗与 OOM 日志

```sh
logread -e hy2route | tail -n 50
logread | grep -E 'invoked oom-killer|Out of memory: Killed process|oom-kill:'
```

### 8.3 路由状态

```sh
nft list table inet hy2route
ip rule show | grep 10066
test -f /tmp/dnsmasq.d/hy2route.conf
nslookup openwrt.org 127.0.0.1
```

需要关注的信号：

- RSS 长时间接近 80 MiB，说明 Go 软限制开始承压。
- 出现 `breaches=1/2` 后又恢复，表示瞬时峰值被容忍。
- 出现 `breaches=3`，表示看门狗主动重启。
- `process-exited` 且 `exit_status=137`，通常表示收到 SIGKILL，应同时核对内核 OOM 日志。
- Xray PID 频繁变化或 procd 实例不再运行，表示达到重试上限或存在持续故障。

## 9. 回滚

本次部署前保留：

```text
/usr/libexec/hy2route/generate.uc.bak-20260719-memory-guard
/etc/init.d/hy2route.bak-20260719-memory-guard
/etc/config/hy2route.bak-20260719-memory-guard
```

其中 `generate.uc.bak-20260719-memory-guard` 已包含“移除重复 GeoIP 判断”的改动，只回滚 keepalive、Go 限额和看门狗。如需回到移除 GeoIP 之前，另有：

```text
/usr/libexec/hy2route/generate.uc.bak-20260719-geoip
```

回滚前先停止服务，恢复目标备份并检查权限，再执行配置生成、Xray、nft、dnsmasq 离线校验。`/etc/config/hy2route.bak-20260719-memory-guard` 保存时 `enabled=0`、`keep_alive_period=15`，恢复后必须根据维护窗口明确决定是否重新启用，不要依赖隐含状态。

## 10. 尚未关闭的风险

- 当前只观察了约 6 分钟，原 OOM 出现在长时间运行后，不能据此宣称根因已完全消除。
- `GOMEMLIMIT` 是软限制；高存活堆、QUIC 缓冲、文件映射或内核网络缓冲仍可能使 RSS 超过 80 MiB。
- 看门狗重启会造成约 5～6 秒代理中断；连续故障超过 procd 重试限制后需要人工恢复。
- 当前脚本仅部署在路由器，仓库没有对应安装源。后续若要在多台设备复用，应将 init、生成器、看门狗和安装/回滚流程纳入受版本控制的独立部署目录，而不是继续只维护设备内文件。

下一轮验收至少覆盖：数小时持续流量、UDP/QUIC 高并发、大文件下载、空闲后重新握手、主动看门狗重启以及真实 OOM 后的 procd 自动拉起。
