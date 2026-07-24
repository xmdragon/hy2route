# hy2route-core OpenWrt Integration and Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Package the single arm64 daemon, replace Xray/chinadns-ng service wiring, preserve UCI/LuCI/CLI, validate a one-client canary, meet resource targets and keep an exact rollback path.

**Architecture:** ucode remains a non-resident UCI adapter that emits root-only JSON, dnsmasq and nftables snippets. procd supervises one `hy2route-core`. Migration uses separate canary ports/table, then an atomic all-network cutover; legacy binaries and backups remain through the 72-hour soak.

**Tech Stack:** OpenWrt 23.05 rc.common/procd/UCI/LuCI, Go 1.25.12 cross-build for `aarch64_cortex-a53`, POSIX shell, dnsmasq, nftables, iproute2, iperf3 and curl/dig from the test workstation.

## Global Constraints

- Target router: Xiaomi WR30U, OpenWrt 23.05.0, Linux 5.15.134, ARM Cortex-A53, approximately 256 MB RAM and no swap.
- Production ports remain TCP/UDP 12345 for TPROXY and 127.0.0.1:1053 for core DNS.
- Canary uses TCP/UDP 22345, DNS 2053 and nft table `hy2route_canary`.
- Keep `/etc/config/hy2route` mode 0600 and never put auth/password values in argv or logs.
- The package no longer declares Xray or chinadns-ng dependencies, but installed legacy packages are not removed before soak acceptance and explicit confirmation.
- Any failed live verification triggers immediate restoration of the exact timestamped backup.
- The existing unrelated Passwall2 conflict guard remains.

---

### Task 1: Static arm64 build and package payload

**Files:**
- Create: `tools/build-core.sh`
- Create: `tests/test_core_binary_contract.sh`
- Modify: `Makefile`
- Modify: `.gitignore`
- Create during build: `build/hy2route-core`
- Create during build: `build/hy2route-data.bin`

**Interfaces:**
- Produces: stripped static ARM64 executable and compiled routing data installed under `/usr/bin` and `/usr/share/hy2route`.

- [ ] **Step 1: Write the failing binary/package contract**

```sh
#!/bin/sh
set -eu
bin="${1:-build/hy2route-core}"
data="${2:-build/hy2route-data.bin}"
test -x "$bin"
test -s "$data"
file "$bin" | grep -Fq 'ARM aarch64'
file "$bin" | grep -Fq 'statically linked'
grep -Fq 'PKG_VERSION:=0.2.0' Makefile
grep -Fq '$(INSTALL_BIN) ./build/hy2route-core $(1)/usr/bin/hy2route-core' Makefile
grep -Fq '$(INSTALL_DATA) ./build/hy2route-data.bin $(1)/usr/share/hy2route/routing.bin' Makefile
! grep -F 'DEPENDS:=' Makefile | grep -Eq 'xray|chinadns'
echo 'core binary contract passed'
```

- [ ] **Step 2: Run and verify the missing artifact failure**

Run:

```bash
chmod +x tests/test_core_binary_contract.sh
./tests/test_core_binary_contract.sh
```

Expected: FAIL because build artifacts and package entries do not exist.

- [ ] **Step 3: Add deterministic cross-build script**

```sh
#!/bin/sh
set -eu
repo="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
out="$repo/build"
mkdir -p "$out"
commit="$(git -C "$repo" rev-parse --short=12 HEAD)"
toolchain="${HY2ROUTE_GO_TOOLCHAIN:-go1.25.12}"
(
	cd "$repo"
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOARM64=v8.0 \
		GOTOOLCHAIN="$toolchain" go build -trimpath \
		-ldflags="-s -w -buildid= -X github.com/xmdragon/hy2route/internal/buildinfo.Version=0.2.0 -X github.com/xmdragon/hy2route/internal/buildinfo.Commit=$commit" \
		-o "$out/hy2route-core" ./cmd/hy2route-core
	GOTOOLCHAIN="$toolchain" go run ./cmd/hy2route-data \
		--domains data/china-domains.txt \
		--ipv4 data/china4.txt \
		--output "$out/hy2route-data.bin"
)
sha256sum "$out/hy2route-core" "$out/hy2route-data.bin"
```

Set `PKG_VERSION:=0.2.0`, `PKG_RELEASE:=1`, remove `PKGARCH:=all`, install both artifacts, and change package dependencies to:

```make
DEPENDS:=+nftables-json +kmod-nft-tproxy +ip-full +ucode +ucode-mod-uci +dnsmasq-full +ca-bundle +luci-base
```

Replace the empty compile hook with:

```make
define Build/Compile
	$(CURDIR)/tools/build-core.sh
endef
```

The release environment must pre-download Go 1.25.12 and modules, run `go mod verify`, and build with `GOTOOLCHAIN=local` when network-isolated reproducibility is required.

- [ ] **Step 4: Verify artifact, module and size**

Run:

```bash
./tools/build-core.sh
./tests/test_core_binary_contract.sh
GOTOOLCHAIN=go1.25.12 go mod verify
du -h build/hy2route-core build/hy2route-data.bin
```

Expected: static aarch64 binary, valid data, contract PASS. Record artifact sizes in the release notes; investigate if the stripped binary exceeds 20 MiB.

- [ ] **Step 5: Commit**

```bash
git add tools/build-core.sh tests/test_core_binary_contract.sh Makefile .gitignore
git commit -m "build: package static hy2route core"
```

Do not commit files under `build/`.

---

### Task 2: Root-only core config, control socket and CLI

**Files:**
- Modify: `files/etc/config/hy2route`
- Modify: `files/usr/libexec/hy2route/generate.uc`
- Modify: `files/usr/bin/hy2route`
- Create: `internal/control/server.go`
- Create: `internal/control/client.go`
- Create: `internal/control/control_test.go`
- Modify: `cmd/hy2route-core/main.go`
- Create: `tests/test_core_generator_contract.sh`

**Interfaces:**
- Generator produces `/tmp/hy2route/core.json`, nftables config and dnsmasq snippet.
- Core exposes root-only Unix socket `/var/run/hy2route-core.sock`.
- CLI exposes `check`, `status`, `test`, and `reload`.

- [ ] **Step 1: Write failing generator/control contracts**

```sh
#!/bin/sh
set -eu
json="$(mktemp)"
trap 'rm -f "$json"' EXIT
./tests/fixtures/run-generator.sh core > "$json"
grep -Fq '"trusted_dns": "8.8.8.8:53"' "$json"
grep -Fq '"fail_open": true' "$json"
grep -Fq '"table": "hy2route"' "$json"
grep -Fq '"type": "socks5"' "$json"
! grep -Eq 'vless|xray|chinadns|smart_dns_port' "$json"
echo 'core generator contract passed'
```

```go
func TestControlSocketIs0600AndNeverReturnsSecrets(t *testing.T) {
	s := startControlServer(t, Snapshot{Mode: "proxy", HY2Connected: true})
	info, err := os.Stat(s.Path())
	if err != nil { t.Fatal(err) }
	if info.Mode().Perm() != 0o600 { t.Fatalf("mode = %o", info.Mode().Perm()) }
	raw := requestRaw(t, s.Path(), `{"op":"status"}`)
	if bytes.Contains(raw, []byte("auth")) || bytes.Contains(raw, []byte("password")) {
		t.Fatalf("secret field in status: %s", raw)
	}
}
```

- [ ] **Step 2: Run and verify legacy-output failures**

Run:

```bash
chmod +x tests/test_core_generator_contract.sh
./tests/test_core_generator_contract.sh
GOTOOLCHAIN=go1.25.12 go test ./internal/control -v
```

Expected: FAIL because generator still emits Xray/chinadns and control package is absent.

- [ ] **Step 3: Define the final UCI surface and generator output**

Retain named sections `main`, `relay`, `landing`; remove the `tcp_relay` section from the default file and ignore it with a one-time warning during migration.

UCI additions:

```uci
config main 'main'
	option fail_open '1'
	option dns_cache_entries '4096'
	option learned_ip_entries '16384'
	option udp_sessions '2048'
	option sniff_bytes '8192'
	option sniff_timeout_ms '250'
	option nft_table 'hy2route'

config landing 'landing'
	option type 'direct'
```

`generate.uc core` emits all Phase 1 JSON fields, writes duration strings such as `"60s"`, and never prints a secret to stderr. It defaults missing legacy landing type to `socks5`, while a fresh install defaults to `direct`.

Core control protocol is one JSON request and response per Unix connection:

```json
{"op":"status"}
{"ok":true,"mode":"proxy","hy2_connected":true,"dns_cache":12,"learned_ips":8,"udp_sessions":2,"active_tcp":4,"rss_bytes":25165824}
```

`hy2route test` calls `hy2route-core probe --config /tmp/hy2route/core.json` and prints separate domestic DNS, trusted DNS, HY2 TCP, configured landing and HY2 UDP results.

- [ ] **Step 4: Verify config, control and CLI**

Run:

```bash
./tests/test_core_generator_contract.sh
GOTOOLCHAIN=go1.25.12 go test -race ./internal/control -count=20
sh -n files/usr/bin/hy2route
ucode -c files/usr/libexec/hy2route/generate.uc
git diff --check
```

Expected: all commands PASS and generated JSON mode is 0600 when installed by the init script.

- [ ] **Step 5: Commit**

```bash
git add files/etc/config/hy2route files/usr/libexec/hy2route/generate.uc \
	files/usr/bin/hy2route internal/control cmd/hy2route-core/main.go \
	tests/test_core_generator_contract.sh
git commit -m "feat: expose hy2route core configuration and status"
```

---

### Task 3: Single-process procd lifecycle with rollback-safe ordering

**Files:**
- Modify: `files/etc/init.d/hy2route`
- Delete after final soak only: `files/usr/libexec/hy2route/run-xray.sh`
- Create: `tests/test_core_lifecycle_contract.sh`
- Modify: `tests/test_package_contract.sh`

**Interfaces:**
- procd instance name is `core`.
- Runtime files are `/tmp/hy2route/core.json`, `/tmp/hy2route/nft.conf`, `/tmp/dnsmasq.d/hy2route.conf`.

- [ ] **Step 1: Write a failing single-process lifecycle contract**

```sh
#!/bin/sh
set -eu
init=files/etc/init.d/hy2route
grep -Fq 'PROG=/usr/bin/hy2route-core' "$init"
grep -Fq 'procd_open_instance core' "$init"
grep -Fq 'procd_set_param command "$PROG" serve --config "$RUNDIR/core.json"' "$init"
grep -Fq 'procd_set_param limits nofile=' "$init"
grep -Fq '"$PROG" check --config "$RUNDIR/core.json"' "$init"
! grep -Eq 'XRAY|CHINADNS|run-xray|xray.json|chinadns.conf' "$init"
echo 'core lifecycle contract passed'
```

- [ ] **Step 2: Run and verify legacy lifecycle failure**

Run:

```bash
chmod +x tests/test_core_lifecycle_contract.sh
./tests/test_core_lifecycle_contract.sh
```

Expected: FAIL because the init script still starts Xray and chinadns-ng.

- [ ] **Step 3: Implement preflight-first lifecycle**

`prepare_config`:

1. Create `/tmp/hy2route` mode 0700.
2. Generate `core.json`, `nft.conf` and `dnsmasq.conf` into `.new` files.
3. Set core JSON mode 0600.
4. Run `hy2route-core check --config core.json.new`.
5. Run `nft -c -f nft.conf.new`.
6. Run `hy2route-core probe --config core.json.new --startup`, which checks HY2 and configured landing but does not bind production ports; fail-open allows the probe to report degraded as success with a warning.
7. Rename all three files atomically.

`start_service` keeps the Passwall2 guard, installs policy routing and nftables, switches the dnsmasq snippet only after preflight, and registers:

```sh
procd_open_instance core
procd_set_param command "$PROG" serve --config "$RUNDIR/core.json"
procd_set_param respawn 3600 5 5
procd_set_param limits nofile='65535 65535'
procd_set_param env GOMEMLIMIT=64MiB GOGC=50
procd_set_param stdout 0
procd_set_param stderr 1
procd_close_instance
```

The dnsmasq snippet uses the core first and the domestic resolver second with strict ordering, so a dead core does not leave clients without DNS. The core's nft heartbeat enables the active TPROXY chain only after DNS, TCP and UDP listeners are bound.

`stop_service` restores dnsmasq before deleting the nft table and route rule. It removes only known runtime files and the control socket.

- [ ] **Step 4: Run shell and contract tests**

Run:

```bash
sh -n files/etc/init.d/hy2route
./tests/test_core_lifecycle_contract.sh
./tests/test_package_contract.sh
GOTOOLCHAIN=go1.25.12 go test ./...
```

Expected: PASS; package contract includes the core binary/data and no Xray/chinadns runtime command.

- [ ] **Step 5: Commit**

```bash
git add files/etc/init.d/hy2route tests/test_core_lifecycle_contract.sh \
	tests/test_package_contract.sh
git commit -m "feat: supervise one hy2route core process"
```

Keep `run-xray.sh` in the source and backup payload until Task 6 authorizes legacy removal.

---

### Task 4: Simplified LuCI management

**Files:**
- Modify: `files/www/luci-static/resources/view/hy2route/main.js`
- Modify: `files/usr/share/rpcd/acl.d/luci-app-hy2route.json`
- Create: `tests/test_core_luci_contract.sh`

**Interfaces:**
- LuCI writes only the final UCI fields from Task 2.

- [ ] **Step 1: Write a failing LuCI scope contract**

```sh
#!/bin/sh
set -eu
view=files/www/luci-static/resources/view/hy2route/main.js
grep -Fq "o.value('direct'" "$view"
grep -Fq "o.value('http'" "$view"
grep -Fq "o.value('socks5'" "$view"
grep -Fq "'fail_open'" "$view"
grep -Fq "'dns_cache_entries'" "$view"
grep -Fq "'learned_ip_entries'" "$view"
! grep -Eq 'VLESS|REALITY|tcp_relay|smart_dns_port|chinadns|Xray' "$view"
echo 'core LuCI contract passed'
```

- [ ] **Step 2: Run and verify old VLESS/Xray UI failure**

Run:

```bash
chmod +x tests/test_core_luci_contract.sh
./tests/test_core_luci_contract.sh
```

Expected: FAIL because legacy fields are still visible.

- [ ] **Step 3: Implement the approved compact form**

General tab:

- enable;
- domestic DNS, trusted DNS;
- fail-open warning and toggle;
- log level.

HY2 section:

- server, port, auth, SNI, certificate verification/pin;
- max idle and keepalive under advanced settings.

TCP egress section:

- `direct`, `http`, `socks5`;
- landing fields depend on `http` or `socks5`;
- username/password fields depend on selected landing.

Rules grid:

- direct/proxy;
- domain or IPv4/CIDR;
- existing validators remain.

Advanced tab:

- ports, mark/table, cache/session/sniff limits;
- each field uses the exact bounds from `config.Validate`.

Add help text that UDP never uses landing and that fail-open exposes the real WAN address during HY2 failure.

- [ ] **Step 4: Run JS and contract checks**

Run:

```bash
node --check files/www/luci-static/resources/view/hy2route/main.js
./tests/test_core_luci_contract.sh
git diff --check
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add files/www/luci-static/resources/view/hy2route/main.js \
	files/usr/share/rpcd/acl.d/luci-app-hy2route.json \
	tests/test_core_luci_contract.sh
git commit -m "feat: simplify LuCI for hy2route core"
```

---

### Task 5: Alternate-port and one-client router canary

**Files:**
- Create: `tools/deploy-core-canary.sh`
- Create: `tools/rollback-core.sh`
- Create: `docs/hy2route-core-canary.md`

**Interfaces:**
- Uses canary table `inet hy2route_canary`, ports 22345/2053 and one explicit client IPv4.
- Does not replace production files before all canary checks pass.

- [ ] **Step 1: Add backup/rollback dry-run tests**

```sh
shellcheck tools/deploy-core-canary.sh tools/rollback-core.sh
tools/deploy-core-canary.sh --dry-run --router 192.168.80.1 \
	--client 192.168.80.20 | grep -Fq 'hy2route_canary'
tools/rollback-core.sh --dry-run \
	--backup /root/hy2route-backup-20000101-000000-core |
	grep -Fq 'restore-or-remove run-xray.sh'
```

- [ ] **Step 2: Run and verify missing scripts**

Run:

```bash
shellcheck tools/deploy-core-canary.sh tools/rollback-core.sh
```

Expected: FAIL because scripts do not exist.

- [ ] **Step 3: Implement exact-target backup and canary scripts**

Backup records files and absence markers for:

```text
/etc/config/hy2route
/etc/init.d/hy2route
/usr/bin/hy2route
/usr/bin/hy2route-core
/usr/libexec/hy2route/generate.uc
/usr/libexec/hy2route/run-xray.sh
/www/luci-static/resources/view/hy2route/main.js
/usr/share/hy2route/routing.bin
/tmp/dnsmasq.d/hy2route.conf
```

The canary:

1. Verifies local and remote SHA-256 for every staged file.
2. Creates a canary JSON config using ports 22345/2053 and table `hy2route_canary`.
3. Runs `hy2route-core check` and `probe`.
4. Starts the staged binary without replacing production files.
5. Installs source-IP-specific DNS redirect and TCP/UDP TPROXY rules for only the nominated client, before the production chain.
6. Verifies China DNS answers, overseas DNS, HY2 direct TCP, configured landing TCP, China UDP direct and overseas UDP HY2.
7. Removes canary rules/process on exit even when a check fails.

Rollback stops the new core, restores each backed-up file or removes it when its `.absent` marker exists, restores UCI/dnsmasq/nftables/policy routing, then checks old `hy2route status` and HTTP 204.

- [ ] **Step 4: Execute canary without production replacement**

Run from the workstation:

```bash
./tools/build-core.sh
./tools/deploy-core-canary.sh \
	--router 192.168.80.1 \
	--client 192.168.80.20 \
	--binary build/hy2route-core \
	--data build/hy2route-data.bin
```

Expected:

- legacy `hy2route status` remains running throughout;
- `wechat.com` and `weixin.qq.com` return domestic A records for the canary;
- Google resolves through trusted DNS;
- canary direct/HY2/landing/UDP probes pass;
- no client other than the nominated IPv4 matches `hy2route_canary`;
- after script exit, no canary process/table/route remains.

- [ ] **Step 5: Commit scripts and canary record**

```bash
git add tools/deploy-core-canary.sh tools/rollback-core.sh docs/hy2route-core-canary.md
git commit -m "ops: add reversible hy2route core canary"
```

Do not continue to all-network cutover until the user confirms the recorded canary results.

---

### Task 6: Production cutover, performance and 72-hour soak

**Files:**
- Create: `tools/verify-core-router.sh`
- Create: `docs/hy2route-core-benchmark.md`
- Modify after soak: `Makefile`
- Delete after soak: `files/usr/libexec/hy2route/run-xray.sh`
- Remove after soak: legacy Xray/chinadns generator modes and tests
- Modify: `README.md`

**Interfaces:**
- Produces the final release evidence and retained backup path.

- [ ] **Step 1: Write the verification script before cutover**

`tools/verify-core-router.sh` must fail unless all checks pass:

```text
hy2route-core binary/data hashes match release artifacts
hy2route check succeeds
procd reports exactly one hy2route core instance running
Xray and chinadns-ng are not part of the new service instance
dnsmasq upstream is 127.0.0.1#1053
force_proxy4 < force_direct4 < inspect4 < direct4 < china4
AAAA query returns NODATA
wechat.com and weixin.qq.com classify direct
www.google.com classifies proxy while HY2 is healthy
HY2 outage changes status to fail-open and Google still connects directly
HY2 recovery returns status to proxy after configured hysteresis
UDP test proves landing credentials are never used
RSS and session/cache counts are within configured limits
no procd crash loop or repeated startup failure exists
stopping the core for 11 seconds expires core_state and sends new traffic direct
dnsmasq resolves through the domestic fallback while the core is stopped
```

- [ ] **Step 2: Run pre-cutover verification and confirm it fails safely**

Run:

```bash
./tools/verify-core-router.sh --router 192.168.80.1 --expect legacy
```

Expected: legacy health checks PASS and new-core checks report `not cut over`; no mutation occurs.

- [ ] **Step 3: Perform atomic cutover with automatic rollback**

Use the exact backup produced in Task 5 or create a newer complete backup. Stage all files, verify hashes, run `check` and `probe`, then:

1. Stop the legacy service.
2. Install the complete new payload and UCI.
3. Start the new service.
4. Wait up to 15 seconds for control-socket healthy status.
5. Run `verify-core-router.sh`.
6. On any failure, immediately run `rollback-core.sh` and verify legacy HTTP 204.

Report the exact backup path and do not delete it.

- [ ] **Step 4: Measure the approved performance envelope**

Record in `docs/hy2route-core-benchmark.md`:

```text
router model/firmware/kernel
binary/data SHA-256
idle RSS after 10 minutes
RSS and CPU with 20-device-equivalent connection load
peak RSS and value 10 minutes after load
domestic direct iperf3 baseline and hy2route result
overseas HY2 baseline and hy2route result
HY2+HTTP and HY2+SOCKS5 throughput
DNS cached and uncached p50/p95 latency
UDP packet loss and session churn
```

Acceptance:

- idle RSS <= 24 MB;
- normal RSS <= 40 MB;
- peak RSS <= 64 MB and falls afterward;
- domestic regression <= 10%;
- overseas HY2 >= 280 Mbps when baseline >= 300 Mbps.

If a limit fails, capture CPU and heap profiles, restore legacy service if user-facing performance regresses, and return to the failing implementation phase.

- [ ] **Step 5: Complete soak and remove runtime dependency declarations**

For 72 hours, sample every five minutes:

```text
procd instance state
core RSS
active TCP and UDP sessions
DNS cache and learned-IP counts
HY2/fail-open state transitions
kernel OOM and procd crash logs
client DNS and HTTP health
```

After the complete window passes:

```bash
./tools/verify-core-router.sh --router 192.168.80.1 --expect core
GOTOOLCHAIN=go1.25.12 go test -race ./...
./tests/test_core_binary_contract.sh
./tests/test_core_lifecycle_contract.sh
./tests/test_core_luci_contract.sh
git diff --check
```

Then remove the Xray supervisor and legacy generator/test branches from the package source. Do not uninstall the router's Xray/chinadns-ng packages until the user explicitly authorizes package removal; an installed but unused package is not a runtime dependency.

- [ ] **Step 6: Commit final cleanup and documentation**

```bash
git add Makefile files tests tools README.md docs/hy2route-core-benchmark.md
git commit -m "feat: release single-process hy2route core"
```

Phase 4 is complete only after the 72-hour report, all acceptance thresholds, rollback evidence and user confirmation are present.
