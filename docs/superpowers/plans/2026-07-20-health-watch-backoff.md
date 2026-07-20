# HY2Route Health Watch Backoff Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve Xray crash and RSS recovery while preventing a transient end-to-end chain outage from repeatedly restarting Xray.

**Architecture:** Package the live release 8 supervisor in the repository, then make health recovery conservative: probe two independent HTTP 204 targets, count a failure only when both fail, restart Xray once after consecutive failed rounds, and suppress further health-triggered restarts for 900 seconds. Memory-triggered restarts remain independent, and a real child exit still returns control to procd.

**Tech Stack:** POSIX `sh`/BusyBox ash, OpenWrt procd, Xray, curl, shell regression tests, OpenWrt package Makefile.

## Global Constraints

- Keep `GOMEMLIMIT=80MiB`.
- Keep the RSS limit at `112640` KiB, sampled every 30 seconds, with 3 consecutive breaches.
- Keep procd crash recovery; do not turn it into a timer.
- Default health targets are `https://www.gstatic.com/generate_204` and `https://cp.cloudflare.com/generate_204`.
- Default health restart cooldown is 900 seconds.
- Do not change the relay, landing, routing rules, credentials, or the user's UCI configuration.
- Build release 10 because release 8 was missing from repository history and release 9 exposed an inconsistent LuCI `keep_alive_period` range during final review.

---

### Task 1: Supervisor regression contract

**Files:**
- Create: `tests/test_run_xray_supervisor.sh`
- Create: `files/usr/libexec/hy2route/run-xray.sh`

**Interfaces:**
- Consumes: `HY2ROUTE_XRAY_BIN`, `HY2ROUTE_CURL_BIN`, `HY2ROUTE_LOGGER_BIN`, and `HY2ROUTE_RSS_READER_BIN` test overrides.
- Produces: health logs with `reason=probe-round-failed`, `reason=restart-after-consecutive-health-failures`, `reason=restart-suppressed-cooldown`, and `reason=health-restart-rearmed`.

- [ ] **Step 1: Add failing shell tests with fake Xray, curl, logger, and RSS reader**

The test must create an isolated temporary directory and assert these cases:

```sh
# One target fails and the other returns 204: one Xray start, no health restart.
run_case mixed 4 2
assert_eq 1 "$(count_starts)"
reject_log 'restart-after-consecutive-health-failures'

# Both targets fail: one restart, then failures are suppressed during cooldown.
run_case fail 5 10
assert_eq 2 "$(count_starts)"
require_log 'restart-suppressed-cooldown'

# After a short cooldown, a later failed round may perform one new restart.
run_case fail 8 3
assert_ge 3 "$(count_starts)"
require_log 'health-restart-rearmed'

# RSS protection remains able to restart Xray.
run_memory_case 4
assert_ge 2 "$(count_starts)"
require_log 'restart-after-consecutive-thresholds'
```

- [ ] **Step 2: Run the supervisor test and verify it fails before implementation**

Run: `sh tests/test_run_xray_supervisor.sh`

Expected: FAIL because `files/usr/libexec/hy2route/run-xray.sh` is absent.

- [ ] **Step 3: Import the release 8 supervisor and implement conservative health recovery**

The supervisor must use these defaults:

```sh
HEALTH_INTERVAL_SECONDS="${HY2ROUTE_HEALTH_INTERVAL_SECONDS:-30}"
HEALTH_TIMEOUT_SECONDS="${HY2ROUTE_HEALTH_TIMEOUT_SECONDS:-10}"
HEALTH_FAILURE_LIMIT="${HY2ROUTE_HEALTH_FAILURE_LIMIT:-3}"
HEALTH_URLS="${HY2ROUTE_HEALTH_URLS:-https://www.gstatic.com/generate_204 https://cp.cloudflare.com/generate_204}"
HEALTH_RESTART_COOLDOWN_SECONDS="${HY2ROUTE_HEALTH_RESTART_COOLDOWN_SECONDS:-900}"
```

For each health round, probe every URL until one succeeds with HTTP 204. Increment `health_failures` only if all targets fail. When the limit is reached and no cooldown is active, set a 900-second cooldown and restart the child once. During cooldown, keep Xray running, reset the consecutive counter after each threshold, and log suppression. Decrement cooldown from the supervisor's one-second loop and log when health restart becomes armed again.

- [ ] **Step 4: Run syntax and supervisor tests**

Run:

```sh
sh -n files/usr/libexec/hy2route/run-xray.sh
sh tests/test_run_xray_supervisor.sh
```

Expected: both commands exit 0; the test reports all supervisor cases passed.

### Task 2: Package the live memory guard

**Files:**
- Modify: `files/etc/init.d/hy2route`
- Modify: `files/usr/libexec/hy2route/generate.uc`
- Modify: `files/etc/config/hy2route`
- Modify: `files/www/luci-static/resources/view/hy2route/main.js`
- Modify: `Makefile`

**Interfaces:**
- Consumes: supervisor created in Task 1 and existing `test_socks_port` UCI option.
- Produces: release 10 package that starts the supervisor with `GOMEMLIMIT=80MiB`, generates `keepAlivePeriod=0`, and accepts `0` in LuCI.

- [ ] **Step 1: Add a failing packaging contract test**

Extend the shell tests to require:

```sh
grep -Fq 'SUPERVISOR=/usr/libexec/hy2route/run-xray.sh' files/etc/init.d/hy2route
grep -Fq 'GOMEMLIMIT=80MiB' files/etc/init.d/hy2route
grep -Fq "keepAlivePeriod: number(relay.keep_alive_period, 0, 0, 60" files/usr/libexec/hy2route/generate.uc
grep -Fq "option keep_alive_period '0'" files/etc/config/hy2route
grep -Fq "o.datatype = 'range(0,60)';" files/www/luci-static/resources/view/hy2route/main.js
grep -Fq "o.default = '0';" files/www/luci-static/resources/view/hy2route/main.js
grep -Fq 'PKG_RELEASE:=10' Makefile
grep -Fq 'run-xray.sh $(1)/usr/libexec/hy2route/run-xray.sh' Makefile
```

- [ ] **Step 2: Run the contract test and verify it fails**

Run: `sh tests/test_package_contract.sh`

Expected: FAIL on the first missing release 8 packaging requirement.

- [ ] **Step 3: Apply the live release 8 init/generator changes and package the supervisor**

Update procd to execute:

```sh
procd_set_param command "$SUPERVISOR" "$RUNDIR/xray.json" "$test_port"
procd_set_param env XRAY_LOCATION_ASSET=/usr/share/v2ray GOMEMLIMIT=80MiB
```

Install and chmod `run-xray.sh`, set new-install and LuCI keepalive defaults to `0`, allow the LuCI range `0..60`, and bump `PKG_RELEASE` to 10.

- [ ] **Step 4: Run all repository contracts**

Run:

```sh
sh tests/test_udp_route_contract.sh
sh tests/test_run_xray_supervisor.sh
sh tests/test_package_contract.sh
sh -n files/etc/init.d/hy2route
sh -n files/usr/libexec/hy2route/run-xray.sh
```

Expected: all commands exit 0.

### Task 3: Document the operational contract

**Files:**
- Modify: `README.md`
- Modify: `docs/2026-07-18-hy2route-oom-memory-optimization.md`

**Interfaces:**
- Consumes: final supervisor behavior from Tasks 1 and 2.
- Produces: documentation that distinguishes process exit, memory recovery, and health recovery.

- [ ] **Step 1: Update README design goals**

Document one Xray proxy core plus a lightweight supervisor, `GOMEMLIMIT=80MiB`, the RSS guard, and conservative health recovery using two targets and a 15-minute cooldown.

- [ ] **Step 2: Update the incident document**

Correct the statement that the supervisor exits after a memory breach: it internally restarts the child. Add the July 20 evidence that a single-target health check caused repeated restarts during an external chain timeout, and record the release 10 behavior.

- [ ] **Step 3: Scan documentation for obsolete behavior**

Run:

```sh
rg -n 'HEALTH_URL=|restart-after-consecutive-health-failures|仅部署在路由器|仓库没有对应' README.md docs files tests
```

Expected: no text claims that the supervisor is router-only or that every three single-target failures cause an unlimited restart loop.

### Task 4: Build, deploy, and verify release 10

**Files:**
- No repository file changes.

**Interfaces:**
- Consumes: release 10 source and router at `192.168.88.1`.
- Produces: installed release 10 with a recoverable pre-install backup and live verification evidence.

- [ ] **Step 1: Build the OpenWrt package with the repository workflow or matching SDK**

Run the existing package build for OpenWrt 23.05.0 `mediatek/filogic` and verify exactly one `hy2route_0.1.0-10_aarch64_cortex-a53.ipk` artifact is produced.

- [ ] **Step 2: Back up the current release 8 files on the router**

Create a timestamped backup containing `/etc/init.d/hy2route`, `/usr/libexec/hy2route/generate.uc`, `/usr/libexec/hy2route/run-xray.sh`, and package metadata before installing release 10. Do not replace `/etc/config/hy2route`.

- [ ] **Step 3: Install release 10 and verify static configuration**

Run:

```sh
opkg install /tmp/hy2route_0.1.0-10_aarch64_cortex-a53.ipk
opkg status hy2route
hy2route check
sh -n /etc/init.d/hy2route
sh -n /usr/libexec/hy2route/run-xray.sh
```

Expected: package version `0.1.0-10`, configuration valid, syntax checks exit 0.

- [ ] **Step 4: Restart once for deployment and verify normal traffic**

Run the service restart once, then verify service state, Xray environment, stable PID, RSS, no OOM, HTTP 204 through local SOCKS, and egress identity. Observe at least three health intervals without a PID change.

- [ ] **Step 5: Run isolated supervisor fault injection without touching the live instance**

Use fake Xray/curl/logger/RSS helpers in `/tmp` to prove: one target success prevents restart, both failures restart once, cooldown suppresses the next restart, and the 1 KiB RSS test still restarts. Confirm no fake processes remain and the production Xray PID is unchanged.

- [ ] **Step 6: Review final diff and repository status**

Run:

```sh
git diff --check
git status --short
git diff --stat origin/main...HEAD
```

Expected: only the planned package, tests, plan, and documentation files changed; no credentials or router configuration appear in the diff.
