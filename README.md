# hy2route

`hy2route` is a deliberately small OpenWrt service for this split topology:

```text
TCP (default): LAN client -> HY2 relay -> SOCKS5 or HTTP landing -> Internet
TCP (optional): LAN client -> VLESS Reality relay -> SOCKS5 or HTTP landing -> Internet
UDP: LAN client -> HY2 relay -> Internet
```

Mainland China IPv4 destinations bypass Xray in nftables. Other destinations
are transparently proxied. Explicit IP and domain rules can force either path.
Proxied TCP exits from the landing, while proxied UDP exits from the HY2 relay.

## Design goals

- One Xray proxy core plus a lightweight POSIX shell supervisor.
- China IP bypass happens in the kernel before traffic reaches Xray.
- Atomic configuration validation before traffic rules are installed.
- `procd` supervises Xray and restarts it after crashes.
- The package is disabled by default and refuses to start while Passwall2 is
  running.
- When the optional VLESS TCP relay is enabled, proxied TCP and remote DNS use
  VLESS Reality to reach the relay while ordinary UDP continues through HY2.
  This keeps long-lived TCP sessions independent from the HY2/QUIC path without
  changing their configured SOCKS5 or HTTP landing exit.
- Without the optional VLESS relay, a SOCKS5 landing sends DNS through HY2 but
  exits before the landing proxy. HTTP landing mode uses the configured
  bootstrap DNS directly because an HTTP proxy cannot transport dnsmasq's UDP
  upstream queries.
- LAN IPv6 forwarding is blocked by default so an unproxied IPv6 route cannot
  bypass the IPv4 policy. Router-local IPv6 services remain reachable.
- HY2 uses BBR and allows idle QUIC connections to close by default. The
  package raises the UDP socket buffer ceiling to 4 MiB without changing the
  default allocation for unrelated sockets.
- Xray runs with `GOMEMLIMIT=80MiB`. The supervisor samples RSS every 30
  seconds and restarts Xray after 3 consecutive samples above 110 MiB.
- End-to-end health checks use two independent HTTP 204 targets. Three rounds
  in which both targets fail may restart Xray once. Further health-triggered
  restarts remain suppressed until the 15-minute cooldown has elapsed and the
  chain has completed 3 consecutive successful rounds.

`allow_insecure` is available only for migrating HY2 servers that do not have
a verifiable certificate. Leave it disabled when possible; a configured
`pinned_cert_sha256` takes precedence.

## Supervisor recovery policy

The supervisor keeps three failure classes separate:

1. If Xray exits, the supervisor returns its status and procd applies the
   configured crash-respawn policy.
2. If Xray RSS exceeds 110 MiB for 3 consecutive 30-second samples, the
   supervisor restarts the child to avoid the router's previously observed
   out-of-memory failure.
3. If both end-to-end health targets fail for 3 consecutive rounds, the
   supervisor performs one health recovery restart for that outage. It does
   not rearm until the 15-minute cooldown has elapsed and the chain has passed
   3 consecutive health rounds, so a persistent relay, landing, or Internet
   outage cannot cause periodic Xray restarts.

Health recovery is deliberately weaker than crash and memory recovery because
an end-to-end timeout does not prove that the local Xray process is unhealthy.

## Protocol split

The landing proxy carries TCP only. TCP reaches it through HY2 by default, or
through the optional VLESS Reality relay when `tcp_relay.enabled=1`. Remote DNS
also uses the VLESS relay in hybrid mode. `udp_policy=proxy` sends ordinary UDP
through HY2 without involving the SOCKS5 or HTTP landing, so it does not depend
on SOCKS5 UDP ASSOCIATE support. `udp_policy=direct` bypasses the proxy for UDP,
and `udp_policy=block` drops non-bypassed UDP.

The VLESS section is optional so an upgraded release 11 configuration remains
valid and keeps its original HY2-only transport until the operator explicitly
enables the new relay.

## Rule precedence

1. Relay, landing and private addresses are always direct.
2. Explicit proxy IP/domain rules.
3. Explicit direct IP/domain rules.
4. Mainland China IPv4 addresses are direct.
5. Everything else uses the protocol split: TCP uses the landing chain over
   VLESS when enabled (otherwise HY2); UDP follows `udp_policy` (`proxy` uses
   the HY2 relay, `direct` bypasses it, and `block` drops it).

Proxy wins when the same value appears in both explicit actions. Domain rules
are also installed as dnsmasq nft sets, so they do not depend on TLS sniffing.

Latency-sensitive UDP services may perform poorly when the HY2 relay is far
from their STUN/TURN infrastructure. Add explicit direct IP/CIDR rules for the
service's documented UDP ranges when low latency matters; other UDP traffic
continues to follow `udp_policy`.

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
