# Hybrid VLESS TCP Relay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep proxied UDP on the HY2 relay while moving proxied TCP and remote DNS to the existing VLESS Reality TCP relay before the landing proxy.

**Architecture:** Add an optional `vless` UCI section named `tcp_relay`. When enabled, the landing outbound dials through a `tcp-relay` VLESS Reality outbound and DNS exits through the same relay; ordinary proxied UDP continues to select `hy2-relay`. When disabled, release 11 behavior remains unchanged for upgrade compatibility.

**Tech Stack:** OpenWrt UCI, ucode, Xray VLESS/REALITY, LuCI JavaScript, POSIX shell contract tests.

## Global Constraints

- TCP remains `LAN -> relay -> SOCKS5/HTTP landing -> Internet`.
- UDP remains `LAN -> HY2 relay -> Internet` and never uses the landing.
- Existing installations without a `tcp_relay` section keep the HY2-only chain.
- Credentials stay in `/etc/config/hy2route`, whose installed mode is `0600`.
- Deployment must preserve the current router configuration and create a rollback archive first.

---

### Task 1: Add the hybrid relay contract

**Files:**
- Create: `tests/test_tcp_relay_contract.sh`
- Modify: `tests/test_package_contract.sh`

**Interfaces:**
- Consumes: existing generator tags `chain`, `hy2-relay`, and `direct`.
- Produces: static contract for optional outbound tag `tcp-relay` and package release 12.

- [ ] **Step 1: Write a failing shell contract test**

Require the generator to parse `tcp_relay`, emit a VLESS Reality outbound, select it for landing transport and DNS only when enabled, keep UDP on `hy2-relay`, and bypass the relay server address in nftables.

- [ ] **Step 2: Run the new test and verify it fails**

Run: `tests/test_tcp_relay_contract.sh`

Expected: FAIL because `tcp_relay` and `make_tcp_relay()` do not exist.

- [ ] **Step 3: Update the package contract for release 12**

Change the expected `PKG_RELEASE` from `11` to `12` and require the new UCI and LuCI relay fields.

### Task 2: Generate the hybrid Xray topology

**Files:**
- Modify: `files/usr/libexec/hy2route/generate.uc`
- Modify: `files/etc/config/hy2route`
- Modify: `files/www/luci-static/resources/view/hy2route/main.js`

**Interfaces:**
- Consumes: `tcp_relay.enabled`, `server`, `port`, `id`, `server_name`, `reality_password`, `short_id`, `fingerprint`, and `flow`.
- Produces: `make_tcp_relay()` returning the Xray outbound tagged `tcp-relay`.

- [ ] **Step 1: Parse and validate the optional VLESS section**

Treat a missing section as disabled. If enabled, require a valid host, non-empty client ID, server name, REALITY password, even-length hexadecimal short ID, and one of the supported uTLS fingerprints.

- [ ] **Step 2: Emit the VLESS Reality outbound**

Use protocol `vless`, encryption `none`, RAW/TCP transport, REALITY security, and the configured server name, fingerprint, password, and short ID.

- [ ] **Step 3: Select relay tags by protocol**

Use `tcp-relay` for the landing outbound and `dns-proxy` when enabled. Keep every ordinary UDP route on `hy2-relay`; keep the legacy HY2 tag when disabled.

- [ ] **Step 4: Protect relay bootstrap traffic**

Add the VLESS server IP to `bypass4`, and resolve a hostname-form server through `bootstrap_dns` in dnsmasq.

- [ ] **Step 5: Expose explicit LuCI fields**

Add a “VLESS TCP 中转” section with masked client ID, REALITY password, and short ID fields. Describe that it affects TCP and remote DNS only.

- [ ] **Step 6: Run contract tests**

Run: `for test in tests/test_*.sh; do "$test"; done`

Expected: every test exits 0.

### Task 3: Document and package release 12

**Files:**
- Modify: `README.md`
- Modify: `Makefile`

**Interfaces:**
- Consumes: the hybrid route behavior from Task 2.
- Produces: release 12 package documentation and metadata.

- [ ] **Step 1: Update topology documentation**

Document optional VLESS/TCP transport for proxied TCP and DNS, legacy fallback when disabled, and unchanged HY2 UDP egress.

- [ ] **Step 2: Bump the package release**

Set `PKG_RELEASE:=12`.

- [ ] **Step 3: Run all repository tests and inspect the diff**

Run: `for test in tests/test_*.sh; do "$test"; done && git diff --check && git status --short`

Expected: tests pass, `git diff --check` exits 0, and only planned files are modified.

### Task 4: Publish and deploy safely

**Files:**
- No additional source files.

**Interfaces:**
- Consumes: release 12 package artifact and existing `grom` VLESS Reality client parameters from the relay server.
- Produces: live router topology `tcp -> VLESS relay -> landing`, `udp -> HY2 relay`, and `dns -> VLESS relay`.

- [ ] **Step 1: Commit, push, and create a reviewable PR**

Use a Conventional Commit, push `codex/hybrid-vless-tcp-relay`, and open a PR against `main`.

- [ ] **Step 2: Merge only after checks pass**

Confirm the PR is mergeable and GitHub Actions succeeds, then merge to `main` and download the release 12 `.ipk` artifact.

- [ ] **Step 3: Back up and configure the router**

Archive `/etc/config/hy2route` and installed package files, verify its checksum, install release 12, then set the VLESS fields without printing credentials.

- [ ] **Step 4: Validate before switching traffic**

Run `hy2route check` and inspect `/tmp/hy2route/xray.check.json` with `jsonfilter` to prove `chain -> tcp-relay`, `dns-proxy -> tcp-relay`, and `udp -> hy2-relay`.

- [ ] **Step 5: Restart and run live probes**

Restart hy2route, run repeated SOCKS HTTP 204 probes, query DNS through port 1053, verify the Xray PID remains stable, and confirm the watchdog reports healthy rounds.

