# UDP via HY2 Relay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep proxied TCP on the `HY2 relay -> SOCKS5/HTTP landing` chain while sending proxied UDP directly through the HY2 relay so ICE/STUN/TURN traffic no longer depends on SOCKS5 UDP support.

**Architecture:** nftables continues to bypass private and mainland-China IPv4 destinations, sends explicit proxy UDP to TPROXY regardless of the global policy, and applies `udp_policy` to ordinary UDP. Xray routing becomes protocol-aware: direct rules still win where intended, explicit proxy rules select the landing chain for TCP and the HY2 relay for UDP, and the default proxied egress is split the same way. The router deployment is staged with a backup and validated before and after restart.

**Tech Stack:** OpenWrt 23.05, ucode, Xray routing, nftables TPROXY, POSIX shell, LuCI JavaScript.

## Global Constraints

- `udp_policy=proxy` must route proxied UDP through `hy2-relay`, never through the SOCKS5/HTTP landing.
- Proxied TCP must continue to use the `chain` outbound and preserve the configured landing exit.
- Explicit direct IP/domain rules and mainland-China bypass behavior must remain unchanged.
- Explicit proxy IP/domain rules must select `hy2-relay` for UDP and `chain` for TCP.
- `block_ipv6` behavior is unchanged by this fix.
- Never print relay or landing credentials.
- Back up the live generator and generated configuration before deployment; restore the backup if validation, restart, TCP, or UDP verification fails.

---

### Task 1: Add the routing-contract regression test

**Files:**
- Create: `tests/test_udp_route_contract.sh`
- Test: `tests/test_udp_route_contract.sh`

**Interfaces:**
- Consumes: `files/usr/libexec/hy2route/generate.uc` as source of the Xray routing contract.
- Produces: an executable regression check that fails while `udp-tproxy` is grouped with the landing-chain TCP inbounds and passes only when UDP and TCP have distinct egress rules.

- [ ] **Step 1: Write the failing test**

```sh
#!/bin/sh
set -eu

generator="${1:-files/usr/libexec/hy2route/generate.uc}"

require_literal() {
	if ! grep -Fq "$1" "$generator"; then
		echo "missing routing contract: $1" >&2
		exit 1
	fi
}

reject_literal() {
	if grep -Fq "$1" "$generator"; then
		echo "obsolete routing contract remains: $1" >&2
		exit 1
	fi
}

require_literal "push(route_rules, { domain: [ 'domain:' + domain ], network: 'udp', outboundTag: 'hy2-relay' });"
require_literal "push(route_rules, { domain: [ 'domain:' + domain ], network: 'tcp', outboundTag: 'chain' });"
require_literal "push(route_rules, { ip: [ ip ], network: 'udp', outboundTag: 'hy2-relay' });"
require_literal "push(route_rules, { ip: [ ip ], network: 'tcp', outboundTag: 'chain' });"
require_literal "inboundTag: [ 'udp-tproxy', 'test-socks' ],"
require_literal "network: 'udp',"
require_literal "outboundTag: 'hy2-relay'"
require_literal "inboundTag: [ 'tcp-redirect', 'test-socks' ],"
require_literal "network: 'tcp',"
require_literal "outboundTag: 'chain'"
require_literal "settings: { auth: 'noauth', udp: true }"
require_literal "ip daddr @force_proxy4 meta l4proto udp tproxy"
reject_literal "inboundTag: [ 'tcp-redirect', 'udp-tproxy', 'test-socks' ]"
reject_literal "HTTP landing cannot carry UDP"
```

- [ ] **Step 2: Run the test against the old implementation**

Run: `sh tests/test_udp_route_contract.sh`

Expected: non-zero exit reporting a missing protocol-aware routing contract.

### Task 2: Split TCP and UDP egress in the generator and UI contract

**Files:**
- Modify: `files/usr/libexec/hy2route/generate.uc:102-111,210-229,258-279`
- Modify: `files/www/luci-static/resources/view/hy2route/main.js:39-55,163-177`
- Modify: `README.md:3-50,78-88`
- Modify: `Makefile:5`
- Test: `tests/test_udp_route_contract.sh`

**Interfaces:**
- Consumes: Xray `RuleObject.network` values `tcp` and `udp` and existing outbound tags `chain`, `hy2-relay`, and `direct`.
- Produces: generated Xray rules where proxy matches and default matches select protocol-appropriate egress.

- [ ] **Step 1: Implement protocol-aware explicit proxy rules**

For every proxy domain and proxy IP, emit the UDP rule first and the TCP rule second:

```ucode
push(route_rules, { domain: [ 'domain:' + domain ], network: 'udp', outboundTag: 'hy2-relay' });
push(route_rules, { domain: [ 'domain:' + domain ], network: 'tcp', outboundTag: 'chain' });
```

Use the equivalent `ip` rules for proxy IP/CIDR entries. Keep direct rules protocol-agnostic.

- [ ] **Step 2: Implement protocol-aware default routes**

Replace the combined inbound rule with:

```ucode
push(route_rules, {
	inboundTag: [ 'udp-tproxy', 'test-socks' ],
	network: 'udp',
	outboundTag: 'hy2-relay'
});
push(route_rules, {
	inboundTag: [ 'tcp-redirect', 'test-socks' ],
	network: 'tcp',
	outboundTag: 'chain'
});
```

- [ ] **Step 3: Make the local SOCKS test inbound accept UDP independently of landing type**

Set `settings: { auth: 'noauth', udp: true }`. Remove the generator rejection and LuCI validation that prohibit `udp_policy=proxy` with an HTTP landing, because UDP now exits from HY2 before the landing.

- [ ] **Step 4: Update documentation and LuCI copy**

Document the split topology explicitly:

```text
TCP: LAN client -> HY2 relay -> SOCKS5 or HTTP landing -> Internet
UDP: LAN client -> HY2 relay -> Internet
```

State that `udp_policy=proxy` is independent of landing UDP support and that UDP has the relay's exit address while TCP has the landing's exit address.

- [ ] **Step 5: Run the routing-contract test**

Run: `sh tests/test_udp_route_contract.sh`

Expected: exit 0 with no output.

### Task 3: Validate and deploy the live generator

**Files:**
- Deploy: `/usr/libexec/hy2route/generate.uc`
- Deploy: `/www/luci-static/resources/view/hy2route/main.js`
- Backup: `/root/hy2route-before-udp-relay-<timestamp>/`

**Interfaces:**
- Consumes: the current router UCI configuration without changing credentials or selected relay/landing.
- Produces: a validated running Xray configuration with `udp -> hy2-relay` and `tcp -> chain`.

- [ ] **Step 1: Back up live files and generated state**

Create a timestamped root-only directory containing the live generator, LuCI form, `/etc/config/hy2route`, and `/tmp/hy2route/xray.json` when present.

- [ ] **Step 2: Copy files and validate before restart**

Run `hy2route check`, then inspect `/tmp/hy2route/xray.check.json` with `jsonfilter` to confirm one UDP catch-all rule targets `hy2-relay` and one TCP catch-all rule targets `chain`. Run `nft -c -f /tmp/hy2route/nft.check.conf`.

- [ ] **Step 3: Restart and verify service health**

Run `/etc/init.d/hy2route restart`, `hy2route status`, `hy2route test`, verify the nftables table and policy route, and confirm the TCP exit still matches the configured landing.

- [ ] **Step 4: Verify the original symptom**

Start one UU Remote connection attempt. Confirm the fresh connection log no longer records repeated STUN/TURN `No route to host`, obtains a selected ICE candidate pair, and remains connected beyond the previous 12-second failure window.

- [ ] **Step 5: Roll back on any failed gate**

Restore both backed-up files, run `hy2route check`, restart, and report the exact failed gate without making a success claim.

### Task 4: Commit the verified repository change

**Files:**
- Commit: `files/usr/libexec/hy2route/generate.uc`
- Commit: `files/www/luci-static/resources/view/hy2route/main.js`
- Commit: `Makefile`
- Commit: `README.md`
- Commit: `tests/test_udp_route_contract.sh`
- Commit: `docs/superpowers/plans/2026-07-19-udp-via-hy2-relay.md`

**Interfaces:**
- Consumes: successful local contract test and live router validation.
- Produces: one reviewable branch commit; no push or pull request without a separate request.

- [ ] **Step 1: Review the final diff and repository status**

Run: `git diff --check && git diff --stat && git status --short`

Expected: no whitespace errors and only the planned files changed.

- [ ] **Step 2: Re-run the local regression test**

Run: `sh tests/test_udp_route_contract.sh`

Expected: exit 0 with no output.

- [ ] **Step 3: Commit**

```sh
git add Makefile README.md docs/superpowers/plans/2026-07-19-udp-via-hy2-relay.md \
	files/usr/libexec/hy2route/generate.uc \
	files/www/luci-static/resources/view/hy2route/main.js \
	tests/test_udp_route_contract.sh
git commit -m "fix: route proxied UDP through HY2 relay"
```
