# HY2 Status Monitoring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Report the HY2 transport's last known idle, connected, or degraded state with backward-compatible status fields, then ship and verify the change on the OpenWrt router.

**Architecture:** A concurrency-safe application-owned tracker implements the existing `transport.EventSink`. The HY2 reconnectable client sends successful handshake and TCP/UDP error events to the tracker, and the control snapshot publishes the tracker's immutable last-known state without exposing error text or credentials.

**Tech Stack:** Go 1.25.12, Hysteria 2 reconnectable client events, Unix control socket JSON, OpenWrt 23.05/procd/opkg, nftables, GitHub Actions OpenWrt SDK build.

## Global Constraints

- Preserve the existing `hy2_connected` JSON field for compatibility.
- Add `hy2_state`, `hy2_last_success`, and `hy2_last_error` exactly as specified in the approved design.
- Timestamps use UTC RFC3339Nano and are omitted until their corresponding event occurs.
- Status responses must not contain event reasons, addresses, authentication values, or other secrets.
- Do not add periodic probes or change HY2 reconnect, fail-open, routing, DNS, or landing behavior.
- Preserve `/etc/config/hy2route` byte-for-byte and mode `0600` during deployment.
- Any failed live verification restores the timestamped router backup before further work.

---

### Task 1: Concurrency-safe HY2 status tracker

**Files:**
- Create: `cmd/hy2route-core/hy2_status.go`
- Create: `cmd/hy2route-core/hy2_status_test.go`

**Interfaces:**
- Consumes: `transport.Event{Stage, Reason}`.
- Produces: `newHY2StatusTracker(func() time.Time) *hy2StatusTracker`, `(*hy2StatusTracker).Emit(transport.Event)`, and `(*hy2StatusTracker).Snapshot() hy2StatusSnapshot`.

- [ ] **Step 1: Write the failing state-transition tests**

Create tests in package `main` that use a deterministic clock and assert:

```go
func TestHY2StatusStartsIdle(t *testing.T) {
	tracker := newHY2StatusTracker(func() time.Time { return time.Unix(1, 0).UTC() })
	got := tracker.Snapshot()
	if got.State != "idle" || got.Connected || got.LastSuccess != "" || got.LastError != "" {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestHY2StatusTracksConnectionErrorAndRecovery(t *testing.T) {
	now := time.Date(2026, 8, 11, 7, 0, 0, 123, time.UTC)
	tracker := newHY2StatusTracker(func() time.Time { return now })
	tracker.Emit(transport.Event{Stage: "hy2.connected", Reason: "connected"})
	connected := tracker.Snapshot()
	if !connected.Connected || connected.State != "connected" || connected.LastSuccess != now.Format(time.RFC3339Nano) {
		t.Fatalf("connected = %+v", connected)
	}
	now = now.Add(time.Second)
	tracker.Emit(transport.Event{Stage: "hy2.tcp", Reason: "secret-bearing detail must not be retained"})
	degraded := tracker.Snapshot()
	if degraded.Connected || degraded.State != "degraded" || degraded.LastError != now.Format(time.RFC3339Nano) {
		t.Fatalf("degraded = %+v", degraded)
	}
	now = now.Add(time.Second)
	tracker.Emit(transport.Event{Stage: "hy2.connected"})
	recovered := tracker.Snapshot()
	if !recovered.Connected || recovered.State != "connected" || recovered.LastError != degraded.LastError || recovered.LastSuccess != now.Format(time.RFC3339Nano) {
		t.Fatalf("recovered = %+v", recovered)
	}
}
```

Add separate tests that `hy2.udp` degrades the state and unrelated event stages leave the snapshot unchanged.

- [ ] **Step 2: Run the focused test and observe the expected RED failure**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test ./cmd/hy2route-core -run '^TestHY2Status' -count=1 -v
```

Expected: FAIL to compile because `newHY2StatusTracker` does not exist.

- [ ] **Step 3: Implement the minimal tracker**

Add a `sync.RWMutex` protected tracker and snapshot. Initialize it to `idle`; update only for `hy2.connected`, `hy2.tcp`, and `hy2.udp`; format times with `tracker.now().UTC().Format(time.RFC3339Nano)`. Never store `Event.Reason`.

- [ ] **Step 4: Verify GREEN and race safety**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test -race ./cmd/hy2route-core -run '^TestHY2Status' -count=1 -v
```

Expected: all HY2 status tests PASS with no race report.

---

### Task 2: Wire tracker into HY2 and the control snapshot

**Files:**
- Modify: `internal/control/control.go`
- Modify: `internal/control/control_test.go`
- Modify: `cmd/hy2route-core/application.go`
- Modify: `cmd/hy2route-core/hy2_status_test.go`

**Interfaces:**
- `control.Snapshot` retains `HY2Connected bool` and adds `HY2State`, `HY2LastSuccess`, and `HY2LastError` string fields with the approved JSON names and `omitempty` on timestamps.
- `application` owns `hy2Status *hy2StatusTracker`; `newApplication` passes it to `hy2.New`.

- [ ] **Step 1: Write failing application/control contract tests**

Add `TestApplicationSnapshotReportsHY2Status`, construct a tracker, emit a successful event, call `application.snapshot()`, and require all connected fields and the exact timestamp. Extend `TestControlSocketIs0600AndNeverReturnsSecrets` to require `"hy2_state":"connected"`, both timestamps, and the existing boolean while still rejecting `auth`, `password`, and the test event reason.

- [ ] **Step 2: Run and observe RED**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test ./cmd/hy2route-core ./internal/control -run 'Test(ApplicationSnapshotReportsHY2Status|ControlSocketIs0600AndNeverReturnsSecrets)' -count=1 -v
```

Expected: FAIL because the control fields and application tracker wiring are absent.

- [ ] **Step 3: Add control fields and application wiring**

Extend `control.Snapshot`, create the tracker before `hy2.New`, pass it instead of `nil`, store it on `application`, and map its snapshot into the control snapshot. A nil tracker defensively reports `idle` and false so unit fixtures cannot panic.

- [ ] **Step 4: Verify focused and package tests**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test -race ./cmd/hy2route-core ./internal/control ./internal/transport/hy2 -count=1
```

Expected: PASS with no races, warnings, or secret-bearing output.

---

### Task 3: Package and router-verification contracts

**Files:**
- Modify: `Makefile`
- Modify: `tests/test_package_contract.sh`
- Modify: `tools/verify-core-router.sh`
- Modify: `tests/test_verify_core_router_contract.sh`

**Interfaces:**
- Produces OpenWrt package `hy2route_0.2.0-2_aarch64_cortex-a53.ipk`.
- Router verification requires the backward-compatible boolean and new state field in the control response.

- [ ] **Step 1: Extend shell contracts first**

Require `PKG_RELEASE:=2` in `tests/test_package_contract.sh`. Require the router verification script to check both `"hy2_connected"` and `"hy2_state"`; update its contract test to require those literals.

- [ ] **Step 2: Run and observe RED**

Run:

```bash
./tests/test_package_contract.sh
./tests/test_verify_core_router_contract.sh
```

Expected: both fail because release 2 and the new status checks are absent.

- [ ] **Step 3: Implement the package and verification changes**

Set `PKG_RELEASE:=2`. After writing the control response to `/tmp/hy2route-core-status.json`, make `tools/verify-core-router.sh` require exact occurrences of `"hy2_connected"` and `"hy2_state"` in addition to its existing checks.

- [ ] **Step 4: Verify GREEN**

Run the two shell contract tests again and require both PASS.

---

### Task 4: Full verification, commit, and push

**Files:**
- All files modified in Tasks 1-3.
- Existing design and plan documents under `docs/superpowers/`.

**Interfaces:**
- Produces a clean `main` branch pushed to `origin/main` and triggers the package workflow.

- [ ] **Step 1: Format and run the full local suite**

Run:

```bash
gofmt -w cmd/hy2route-core/hy2_status.go cmd/hy2route-core/hy2_status_test.go internal/control/control.go internal/control/control_test.go cmd/hy2route-core/application.go
GOTOOLCHAIN=go1.25.12 go test -race ./...
for test in tests/test_*.sh; do "$test"; done
GOTOOLCHAIN=go1.25.12 go vet ./...
git diff --check
```

Expected: all Go and shell tests PASS, `go vet` emits no findings, and `git diff --check` is clean.

- [ ] **Step 2: Inspect and commit intentional changes**

Run `git status`, `git diff --stat`, `git diff`, and a credential-pattern scan. Stage only the planned source, tests, metadata, and documents; do not stage `build/` artifacts. Commit with:

```text
fix: report HY2 transport health
```

- [ ] **Step 3: Build and inspect the release binary from the committed tree**

Run:

```bash
./tools/build-core.sh
./tests/test_core_binary_contract.sh
file build/hy2route-core
go version -m build/hy2route-core
```

Expected: static ARM64 binary contract PASS and build metadata identifies the current module. Verify the embedded application commit by running the binary during the router canary because the ARM64 executable cannot run on the x86 workstation.

- [ ] **Step 4: Push without force and verify CI**

Run:

```bash
git push origin main
gh run list --workflow build.yml --branch main --limit 1
gh run watch RUN_ID --exit-status
```

Expected: push succeeds and the OpenWrt package workflow completes successfully.

---

### Task 5: Canary, production deployment, and live acceptance

**Files:**
- Downloaded CI artifact under an ignored temporary/artifact directory only.
- Router backup under `/root/hy2route-backup-<timestamp>-status-monitoring`.

**Interfaces:**
- Consumes the successful GitHub Actions release-2 IPK.
- Produces a running release 2 on `192.168.80.1` with preserved UCI configuration and verified `64.32.180.57` TCP exit.

- [ ] **Step 1: Download and verify the CI package artifact**

Use `gh run download RUN_ID -n hy2route-openwrt-23.05.0` into a temporary directory, require exactly one `hy2route_0.2.0-2_aarch64_cortex-a53.ipk`, and record its SHA-256.

- [ ] **Step 2: Run the one-client canary**

Run `tools/deploy-core-canary.sh` with router `192.168.80.1`, the current workstation LAN source `192.168.80.168`, the release binary/data artifacts, and a unique root-only backup path. Require `core canary passed`; allow the script to remove only its owned canary table, rule, and staging directory.

- [ ] **Step 3: Back up production and install release 2**

Before mutation, record SHA-256 and mode for `/etc/config/hy2route`, copy the current package-owned files and `opkg status hy2route` into a unique mode-0700 backup directory, upload the verified IPK to `/tmp`, and install it with `opkg install`. Confirm the UCI checksum and mode are unchanged, then restart only `/etc/init.d/hy2route`.

If install, restart, or verification fails, restore the backed-up binary and package-owned files, restore `/etc/config/hy2route`, restart only `hy2route`, and rerun the pre-deployment endpoint check before continuing.

- [ ] **Step 4: Trigger HY2 and verify the status contract**

From the LAN workstation, bind curl to `192.168.80.168` and request `https://api.ipify.org`; require response `64.32.180.57`. Then require router status to contain:

```json
"mode":"proxy"
"hy2_connected":true
"hy2_state":"connected"
"hy2_last_success":"<non-empty RFC3339 timestamp>"
```

Run `tools/verify-core-router.sh --router 192.168.80.1 --expect core`, DNS queries for `wechat.com` and `www.google.com`, and nftables heartbeat verification.

- [ ] **Step 5: Verify resource and error gates**

Require the core process to remain running, RSS below 40 MiB, no OOM record, no new `SOCKS5 method rejected`, authentication rejection, panic, or procd crash-loop entry, and a second bound HTTP 204 request to succeed. Report the retained backup path and package/binary commit.

- [ ] **Step 6: Confirm repository and deployed state**

Run `git status --short`, `git log --oneline -2`, `git ls-remote origin refs/heads/main`, `opkg status hy2route`, and `hy2route-core version`. Require a clean local tree, matching remote commit, installed `0.2.0-2`, and the deployed binary commit matching the pushed implementation commit.
