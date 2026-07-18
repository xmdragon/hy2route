# hy2route

`hy2route` is a deliberately small OpenWrt service for this topology:

```text
LAN client -> HY2 relay -> SOCKS5 or HTTP landing -> Internet
```

Mainland China IPv4 destinations bypass Xray in nftables. Other destinations
are transparently proxied. Explicit IP and domain rules can force either path.

## Design goals

- One persistent process: the existing Xray core.
- China IP bypass happens in the kernel before traffic reaches Xray.
- Atomic configuration validation before traffic rules are installed.
- `procd` supervises Xray and restarts it after crashes.
- The package is disabled by default and refuses to start while Passwall2 is
  running.
- With a SOCKS5 landing, DNS goes through the HY2 relay but deliberately exits
  before the landing proxy. This keeps DNS independent of SOCKS5 UDP support.
  HTTP landing mode uses the configured bootstrap DNS directly because an HTTP
  proxy cannot transport dnsmasq's UDP upstream queries.
- LAN IPv6 forwarding is blocked by default so an unproxied IPv6 route cannot
  bypass the IPv4 policy. Router-local IPv6 services remain reachable.
- HY2 uses BBR with periodic QUIC keepalives by default. The package raises the
  UDP socket buffer ceiling to 4 MiB without changing the default allocation
  for unrelated sockets.

`allow_insecure` is available only for migrating HY2 servers that do not have
a verifiable certificate. Leave it disabled when possible; a configured
`pinned_cert_sha256` takes precedence.

## Landing protocol limits

SOCKS5 can carry TCP and UDP only when the landing server supports SOCKS5 UDP
ASSOCIATE. HTTP proxy landing servers carry TCP only. With an HTTP landing,
set `udp_policy` to `block` (recommended) or `direct` (leaks UDP outside the
proxy). `proxy` is rejected for HTTP landings.

## Rule precedence

1. Relay, landing and private addresses are always direct.
2. Explicit proxy IP/domain rules.
3. Explicit direct IP/domain rules.
4. Mainland China IPv4 addresses are direct.
5. Everything else uses the chain.

Proxy wins when the same value appears in both explicit actions. Domain rules
are also installed as dnsmasq nft sets, so they do not depend on TLS sniffing.

## LuCI configuration

The package installs a native LuCI form at **Services > HY2Route**. It exposes
the service policy, HY2 relay, SOCKS/HTTP landing, advanced ports and an
add/remove/sort table for explicit IP, CIDR and domain routing rules. Password
fields are masked in the browser. Saving and applying the form commits the UCI
configuration and triggers a service reload.

## Build

Copy this directory into `package/hy2route` in an OpenWrt 23.05 SDK matching the
router target, refresh the China snapshot, then build the package:

```sh
python3 tools/update_china4.py
make package/hy2route/compile V=s
```

GitHub Actions refreshes the APNIC mainland China IPv4 snapshot, builds the
package with the verified OpenWrt 23.05.0 `mediatek/filogic` SDK and publishes
the `.ipk` as a workflow artifact. It runs for changes, manual requests and a
weekly schedule.

The target router used during development is `mediatek/filogic`, ARM64,
OpenWrt 23.05.0.

## Configure and test

Edit `/etc/config/hy2route`, then run:

```sh
hy2route check
/etc/init.d/hy2route enable
/etc/init.d/hy2route start
hy2route status
hy2route test
```

Passwall2 must be stopped before `hy2route` starts. The service never stops or
changes Passwall2 automatically.
