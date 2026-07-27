# Atomic nftables Heartbeat Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the recurring 98-100 ms firewall fail-open window by refreshing both heartbeat verdict maps in one atomic nftables transaction.

**Architecture:** Keep the existing 3-second Go heartbeat and 10-second nftables timeout. Build one deterministic nft script that flushes and repopulates `core_state` and `output_state`, then send it through one `nft -f -` process so the kernel commits the batch atomically.

**Tech Stack:** Go 1.25.12, nftables 1.0.8 on OpenWrt 23.05, POSIX shell, OpenWrt ARM64 cross-build.

## Global Constraints

- Preserve fail-open behavior when the core stops and the 10-second verdict timeout expires.
- Do not expose or replace `/etc/config/hy2route`.
- Create a router backup before replacing the binary.
- Roll back automatically if service, DNS, HTTPS, egress, or PID checks fail.
- Remove deployment artifacts from `/tmp` after verification.

---

### Task 1: Atomic heartbeat batch and regression test

**Files:**
- Create: `internal/firewall/heartbeat.go`
- Create: `internal/firewall/heartbeat_test.go`
- Modify: `internal/firewall/nft_linux.go:75-93`
- Modify: `tests/test_core_nft_contract.sh`

**Interfaces:**
- Consumes: heartbeat table, map, chain, and TTL values already owned by `NftSetClient`.
- Produces: `buildHeartbeatBatch(table string, ttl time.Duration, entries []heartbeatEntry) string`.

- [ ] **Step 1: Write the failing unit and contract tests**

The unit test must require one batch containing, in order, `flush map` and `add element` for both verdict maps. The contract test must reject the old `delete element` plus multiple `exec.CommandContext` loop.

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/firewall && sh tests/test_core_nft_contract.sh`

Expected: failure because `buildHeartbeatBatch` and the atomic `nft -f -` call do not exist.

- [ ] **Step 3: Implement the minimal atomic batch**

`Heartbeat` must execute exactly one command:

```go
command := exec.CommandContext(ctx, "nft", "-f", "-")
command.Stdin = strings.NewReader(buildHeartbeatBatch(c.table.Name, ttl, entries))
```

The generated script must be:

```text
flush map inet <table> core_state
add element inet <table> core_state { 0x00000001 timeout <ttl> : jump active }
flush map inet <table> output_state
add element inet <table> output_state { 0x00000001 timeout <ttl> : jump output_active }
```

- [ ] **Step 4: Run red-green verification**

Run: `go test ./internal/firewall && sh tests/test_core_nft_contract.sh`

Expected: both pass. Temporarily restore the old loop and confirm the contract test fails, then restore the fix and confirm both pass again.

### Task 2: Build and local release gates

**Files:**
- Modify: `Makefile` to increment package release `1` to `2`.

**Interfaces:**
- Consumes: tested Go source and existing `tools/build-core.sh`.
- Produces: static ARM64 `build/hy2route-core` and unchanged routing data.

- [ ] **Step 1: Run repository contract tests**

Run every `tests/test_*_contract.sh` that is host-compatible and record any platform-only exclusions.

- [ ] **Step 2: Cross-build the release binary**

Run: `./tools/build-core.sh && sh tests/test_core_binary_contract.sh`

Expected: static ARM aarch64 binary, valid routing data, and exit status 0.

- [ ] **Step 3: Inspect the release diff**

Run: `git diff --check && git diff --stat && git status --short`

Expected: only the heartbeat implementation, regression tests, plan, and release metadata are changed.

### Task 3: Reversible router deployment and live proof

**Files:**
- Runtime-only backup under `/root`; no credentials or backup contents enter Git.

**Interfaces:**
- Consumes: verified ARM64 binary and current router configuration.
- Produces: running `hy2route-core` with atomic heartbeat refresh.

- [ ] **Step 1: Back up and preflight**

Record the current PID, binary hash, config hash, package version, nft rules, policy route, and dnsmasq snippet. Save the current binary and installed scripts in a timestamped root-only backup.

- [ ] **Step 2: Stage and atomically replace the binary**

Upload only the verified binary, validate its hash and configuration, then stop the service, atomically replace `/usr/bin/hy2route-core`, and start the service. If any post-start gate fails, restore the backup immediately.

- [ ] **Step 3: Prove the heartbeat has no fail-open gap**

Observe `nft monitor`: one generation must contain both map refreshes. Synchronize at least 20 OpenAI TLS probes with heartbeat refresh and require 20/20 expected HTTP 401 responses.

- [ ] **Step 4: Prove routing and stability**

Require router `ping0.cc` 30/30 and local `curl --noproxy '*' https://ping0.cc` 10/10 to return proxy exit `64.32.180.57`; require local OpenAI TLS 30/30; verify PID stability, RSS, threads, FD, DNS, no SOCKS rejection, no timeout increase, and a download sample no larger than 1 MiB.

- [ ] **Step 5: Clean deployment artifacts**

Remove staged binaries and temporary validation files from `/tmp`; retain only the root-only rollback backup until the soak completes.

### Task 4: Commit and publish

**Files:**
- All verified files from Tasks 1-2.

**Interfaces:**
- Consumes: live-verified source and deployment evidence.
- Produces: one focused commit pushed to `origin/codex/router-local-output`.

- [ ] **Step 1: Commit the focused fix**

Run: `git add internal/firewall tests/test_core_nft_contract.sh docs/superpowers/plans/2026-07-27-atomic-nft-heartbeat.md Makefile && git commit -m "fix: refresh nft heartbeat atomically"`

- [ ] **Step 2: Push and confirm remote parity**

Run: `git push origin codex/router-local-output` and compare local/remote commit IDs.
