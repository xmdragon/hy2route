# hy2route-core Implementation Roadmap

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Xray and chinadns-ng runtime with one bounded-memory Go daemon while preserving dnsmasq, UCI, LuCI, nftables, rollback, IPv4 China bypass, HY2 transport, optional HTTP/SOCKS5 landing, and direct HY2 UDP.

**Architecture:** Work is split into four sequential plans. Each phase has a reviewer gate and a runnable deliverable; no phase changes the production router until Phase 4 canary. Interfaces named here are stable contracts between plans.

**Tech Stack:** Go 1.25.12, Hysteria core v2.10.0, miekg/dns v1.1.72, google/nftables v0.3.0, Linux TPROXY, nftables, OpenWrt procd/UCI/LuCI, POSIX shell.

## Global Constraints

- Target runtime: OpenWrt 23.05.0, Linux 5.15.134, `aarch64_cortex-a53`, 256 MB RAM.
- IPv4 only; local AAAA queries return NODATA.
- Runtime keeps dnsmasq, procd, UCI and nftables; Xray and chinadns-ng are removed only after final acceptance.
- TCP supports HY2 direct, HY2 to HTTP CONNECT landing, and HY2 to SOCKS5 CONNECT landing.
- UDP never uses the landing proxy: China IPv4 is direct, all other IPv4 uses HY2, and HY2 failure degrades new UDP sessions to direct.
- Explicit rules win; TCP SNI/Host wins over learned IP state; conflicts without a domain use proxy.
- China-only direct flows stay in nftables and retain flow-offload eligibility.
- Overseas failure policy is fail-open to direct, with rate-limited transition logs.
- Runtime caches, sessions, buffers and nftables elements are bounded.
- Empty RSS target is at most 24 MB; ordinary 20-device RSS is at most 40 MB; stress peak is at most 64 MB and must fall afterward.
- Domestic throughput regression is at most 10%; overseas target is at least 280 Mbps when the line and server baseline reach 300 Mbps.
- Do not deploy to the production ports until the one-client canary and rollback drill pass.

---

## Phase sequence

1. [Foundation and Smart DNS](2026-07-24-hy2route-core-foundation-dns.md)
   - Produces a host-runnable `hy2route-core` with validated JSON configuration, China-domain/IPv4 policy, bounded DNS cache, concurrent domestic/trusted resolution, AAAA filtering and unit/fuzz tests.
   - Stable interfaces: `config.Config`, `policy.Classifier`, `dnsproxy.Exchanger`, `dnsproxy.Learner`.

2. [HY2 Transport, Landing and Fail-open](2026-07-24-hy2route-core-hy2-transport.md)
   - Produces `transport.StreamDialer`, `transport.PacketDialer`, the Hysteria adapter, direct/HTTP/SOCKS5 TCP dialers, trusted DNS over HY2, bounded UDP sessions and a hysteretic fail-open controller.
   - Depends only on Phase 1 public interfaces.

3. [Transparent Dataplane and Dynamic nftables](2026-07-24-hy2route-core-transparent-dataplane.md)
   - Produces TCP/UDP TPROXY listeners, bounded SNI/Host inspection, dynamic `direct4`/`inspect4` updates, ordered nftables rules and network-namespace integration tests.
   - Domestic fast-path behavior is proven before any OpenWrt service change.

4. [OpenWrt Integration, Canary and Migration](2026-07-24-hy2route-core-openwrt-migration.md)
   - Produces the arm64 artifact, UCI/LuCI/CLI/procd integration, package dependency reduction, staged one-client canary, performance report, 72-hour soak and reversible removal of Xray/chinadns-ng.

## Cross-phase review gates

- After Phase 1: `go test ./...`, fuzz smoke tests, race tests and DNS integration tests pass; no router deployment.
- After Phase 2: a local Hysteria test server and fake landing proxies prove every TCP/UDP/fail-open path; no production port change.
- After Phase 3: privileged namespace tests prove TPROXY, original-destination recovery, rule ordering and dynamic-set expiry.
- During Phase 4: build and run a shadow instance on alternate ports, then one client, then all clients. Keep the existing Xray service and timestamped backup through the 72-hour soak.

## Security dependency gate

Pin all modules in `go.mod` and commit `go.sum`. Run:

```bash
GOTOOLCHAIN=go1.25.12 go mod verify
GOTOOLCHAIN=go1.25.12 go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

The Hysteria advisory GHSA-9fw6-xgg2-mq9q affects server sniffing in versions through 2.8.1. This project pins client core v2.10.0 and does not link the Hysteria server or its sniff feature. A vulnerability report is still a release blocker if `govulncheck` shows a reachable call path from `cmd/hy2route-core`.

## Primary implementation references

- Hysteria v2.10.0 client API: <https://pkg.go.dev/github.com/apernet/hysteria/core/v2@v2.10.0/client>
- Hysteria 2 TCP/UDP protocol behavior: <https://github.com/apernet/hysteria/blob/master/PROTOCOL.md>
- Hysteria security advisory: <https://github.com/apernet/hysteria/security/advisories/GHSA-9fw6-xgg2-mq9q>
- nftables Go set and timeout API: <https://pkg.go.dev/github.com/google/nftables>
- Current OpenWrt Go package pattern: <https://github.com/openwrt/packages/blob/master/net/sing-box/Makefile>

## Completion rule

Do not delete the legacy generator, Xray supervisor, chinadns configuration, or their backup until every Phase 4 acceptance command has passed and the 72-hour soak has completed. If a gate fails, retain the last independently passing phase and fix that phase before continuing.
