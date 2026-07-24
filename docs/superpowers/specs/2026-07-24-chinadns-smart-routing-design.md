# ChinaDNS Smart Routing Design

## Goal

Replace hy2route's single foreign DNS path with an integrated dual-upstream
resolver that selects a mainland answer when the mainland resolver returns
mainland IPv4 addresses, and otherwise selects the trusted answer carried by
Xray. This prevents DNS-based load balancing from turning direct-capable
services such as WeChat into proxy traffic.

## Confirmed Root Cause

The live router currently forwards ordinary DNS to `8.8.8.8` through Xray.
On 2026-07-24, that path returned `43.163.181.74` and `43.163.178.94` for
`wechat.com`; neither address belongs to hy2route's `china4` set. The direct
resolver at `192.168.1.1` returned `120.233.109.151` and `120.233.109.196`,
both of which belong to `china4`. The IP routing rules therefore behaved as
configured, but the foreign resolver selected a foreign CDN before routing
made its decision.

An isolated live test proved that the router's installed
`chinadns-ng 2025.08.09-1` can query both paths and use
`inet@hy2route@china4` to accept the mainland answers for both `wechat.com`
and `weixin.qq.com`.

## Chosen Approach

Run `chinadns-ng` as a second procd-managed process owned by hy2route:

```text
LAN client
    |
    v
dnsmasq :53
    |
    v
chinadns-ng 127.0.0.1:65353
    |                                |
    | mainland upstream             | trusted upstream
    v                                v
192.168.1.1 direct          TCP 127.0.0.1:1053
                                      |
                                      v
                               Xray -> 8.8.8.8
```

For an unclassified domain, ChinaDNS sends the query to both upstream groups.
It accepts the mainland answer only when its A records belong to
`inet@hy2route@china4`; otherwise it accepts the trusted answer. This keeps the
existing destination-IP routing model aligned with the DNS answer that
clients receive.

The rejected alternatives are:

- A growing direct-domain allowlist: useful for overrides but incomplete for
  services whose domains change.
- ECS injection into foreign DNS: dependent on a stable public prefix and
  authoritative-server support, and it discloses client location.
- Sending every query to a mainland resolver: restores local CDN answers but
  loses the trusted foreign resolution path.

## Configuration

Add two options to the `main` UCI section and LuCI:

```text
option smart_dns '1'
option smart_dns_port '65353'
```

`smart_dns` is enabled by default. LuCI exposes it under normal DNS settings
and exposes `smart_dns_port` under advanced ports.

When enabled:

- `chinadns-ng` is required at `/usr/bin/chinadns-ng`.
- `bootstrap_dns` is the ChinaDNS mainland upstream.
- `remote_dns` remains the destination of Xray's DNS inbound.
- ChinaDNS's trusted upstream is `tcp://127.0.0.1#<dns_port>`.
- dnsmasq's ordinary upstream is
  `127.0.0.1#<smart_dns_port>`.

When disabled, hy2route preserves release 12 behavior exactly. This opt-out is
an emergency compatibility mechanism, not an automatic fallback.

The two ports must be different. Both must be valid TCP/UDP port numbers.

## Generated ChinaDNS Configuration

Add a `chinadns` generator mode that emits a runtime configuration containing:

```text
bind-addr 127.0.0.1
bind-port <smart_dns_port>
china-dns <bootstrap_dns>
trust-dns tcp://127.0.0.1#<dns_port>
ipset-name4 inet@hy2route@china4
ipset-name6 inet@hy2route@china6
no-ipv6
cache 1024
verdict-cache 1024
```

The cache sizes are fixed rather than exposed as tuning options. They are large
enough for a home LAN and bounded for this memory-constrained router.

hy2route remains IPv4-only for forwarded client traffic. ChinaDNS filters AAAA
answers, consistent with the package's existing default of blocking LAN IPv6
forwarding. The generated nftables table gains an empty `china6` IPv6-address
set solely to provide a correctly typed set to ChinaDNS; no IPv6 route
classification is introduced in this release.

No `chnlist` or `gfwlist` is required. Unclassified-domain dual resolution is
the mechanism that fixes DNS load-balancing selection without maintaining
another domain database.

## Existing Rule Semantics

Existing explicit rules retain their current precedence:

1. Relay, landing, and private addresses.
2. Explicit proxy IP/domain rules.
3. Explicit direct IP/domain rules.
4. Mainland IPv4 addresses.
5. Default proxy policy.

Explicit direct domains continue to:

- use `bootstrap_dns` directly through dnsmasq; and
- add their A records to `force_direct4`.

Explicit proxy domains continue to add their final A records to
`force_proxy4`. Since `force_proxy4` is evaluated before `china4`, an explicit
proxy rule still wins even if ChinaDNS returns a mainland address.

## Lifecycle and Failure Handling

The init script generates four runtime artifacts:

- `xray.json`
- `nft.conf`
- `dnsmasq.conf`
- `chinadns.conf` when smart DNS is enabled

Before modifying live DNS, startup validates the Xray and nftables
configurations and verifies that the ChinaDNS binary is executable. After the
new nftables table is installed, the init script performs a short ChinaDNS
startup probe against the generated configuration because ChinaDNS must open
the referenced nftables sets.

Only after the probe succeeds does the script install the dnsmasq snippet.
ChinaDNS is then registered as a named procd instance with respawn enabled,
alongside the existing Xray supervisor instance.

If the binary is missing or the startup probe fails:

- hy2route startup fails;
- its newly installed policy and nftables rules are removed;
- the existing dnsmasq configuration is left unchanged.

If installing the generated dnsmasq snippet fails:

- the snippet is removed;
- dnsmasq is restarted with its original configuration;
- hy2route's network rules are removed.

There is no silent fallback from smart DNS to the legacy foreign-only or
mainland-only DNS path. A runtime ChinaDNS crash is handled by procd respawn;
while it is unavailable, DNS fails visibly instead of returning answers that
silently take the wrong route.

Stopping hy2route removes its dnsmasq snippet, restarts dnsmasq, stops both
managed instances through procd, removes policy routing and nftables state,
and deletes generated runtime files.

`hy2route status` requires both the Xray process and the ChinaDNS process when
smart DNS is enabled.

## Packaging

The repository does not vendor an architecture-specific ChinaDNS binary.
Release documentation states that smart mode requires `chinadns-ng` compatible
with the tested 2025.08.09 command interface. The target router already has
that package installed.

Because `chinadns-ng` is supplied by a third-party OpenWrt feed rather than the
stock OpenWrt 23.05 SDK feed used by this repository's CI, this change does not
add an unresolved hard package dependency to `Makefile`. Runtime validation
provides the agreed fail-closed behavior. A future packaging change may add a
dependency only after the build workflow imports and pins a compatible
ChinaDNS package recipe.

The hy2route package release increments from 12 to 13.

## Tests

Add a smart-DNS contract test that fails before implementation and verifies:

- UCI and LuCI defaults;
- port validation and port-collision rejection;
- the exact generated ChinaDNS upstream and nftset wiring;
- dnsmasq selects ChinaDNS only when enabled;
- the empty, correctly typed `china6` set is generated;
- the init script checks the binary, performs the startup probe, registers a
  named procd instance, and rolls back on failure;
- status checks ChinaDNS only in smart mode;
- the package release is 13.

Run all existing shell contract and supervisor tests to protect release 12
routing, VLESS, UDP, and recovery behavior.

On the router, validate in this order:

1. Back up the live generator, init script, CLI, LuCI view, UCI configuration,
   and generated runtime files into a timestamped root-only directory.
2. Run offline `hy2route check` and dnsmasq/nftables syntax validation.
3. Restart hy2route and verify both procd-managed processes remain running.
4. Query `wechat.com` and `weixin.qq.com` through `192.168.80.1` and confirm the
   returned A records belong to `china4`.
5. Query a foreign domain and confirm DNS succeeds through the trusted path.
6. Run `hy2route test`, verify nftables and policy routing, and observe process
   stability before declaring deployment complete.

## Deployment and Rollback

Deployment preserves `/etc/config/hy2route`; it adds the new UCI options with
`uci set` rather than replacing the file containing live credentials.

If any live verification fails, restore the backed-up runtime scripts and LuCI
view, restore the saved UCI configuration, run the old `hy2route check`, and
restart the previous service. The backup path and validation output are
reported to the operator.
