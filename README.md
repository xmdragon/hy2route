# hy2route

`hy2route` is a deliberately small OpenWrt service for this split topology:

```text
TCP (default): LAN client -> HY2 relay -> SOCKS5 or HTTP landing -> Internet
TCP (optional): LAN client -> VLESS Reality relay -> SOCKS5 or HTTP landing -> Internet
UDP: LAN client -> HY2 relay -> Internet
DNS (smart): dnsmasq -> ChinaDNS-NG
  mainland candidate -> bootstrap DNS directly
  trusted candidate  -> Xray DNS inbound -> remote DNS through relay
```

Mainland China IPv4 destinations bypass Xray in nftables. Other destinations
are transparently proxied. Explicit IP and domain rules can force either path.
Proxied TCP exits from the landing, while proxied UDP exits from the HY2 relay.

## Design goals

- One Xray proxy core plus a lightweight POSIX shell supervisor.
- China IP bypass happens in the kernel before traffic reaches Xray.
- Atomic configuration validation before traffic rules are installed.
- `procd` supervises Xray and restarts it after crashes.
- `procd` also supervises ChinaDNS-NG when smart DNS is enabled.
- The package is disabled by default and refuses to start while Passwall2 is
  running.
- When the optional VLESS TCP relay is enabled, proxied TCP and remote DNS use
  VLESS Reality to reach the relay while ordinary UDP continues through HY2.
  This keeps long-lived TCP sessions independent from the HY2/QUIC path without
  changing their configured SOCKS5 or HTTP landing exit.
- Smart DNS sends its trusted query over TCP to Xray's DNS inbound, so the
  query can use HY2 or the optional VLESS relay without depending on landing
  proxy DNS support. The direct candidate always uses `bootstrap_dns`.
- Smart DNS startup fails closed if ChinaDNS-NG is missing or cannot open its
  generated configuration; hy2route never silently selects a different DNS
  path.
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

## Smart DNS

`smart_dns=1` is the default. dnsmasq sends ordinary queries to the local
ChinaDNS-NG listener. ChinaDNS-NG queries `bootstrap_dns` directly and sends a
second trusted query over TCP to Xray's DNS inbound, which targets
`remote_dns`. For domains without an explicit rule, it accepts the direct
answer only when its A records are in hy2route's `china4` nftables set;
otherwise it returns the trusted answer. This keeps DNS-based CDN selection
aligned with the destination-IP routing decision.

Explicit direct domains still use `bootstrap_dns` and populate
`force_direct4`. Explicit proxy domains still populate `force_proxy4`, which
is evaluated before `china4`. Smart mode filters AAAA answers because this
release routes forwarded clients by IPv4 and blocks LAN IPv6 forwarding by
default.

Set `smart_dns=0` to restore release 12 DNS behavior. This is an explicit
compatibility option, not an automatic fallback.

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
the service policy, smart DNS, HY2 relay, SOCKS/HTTP landing, advanced ports
and an add/remove/sort table for explicit IP, CIDR and domain routing rules.
Password fields are masked in the browser. Saving and applying the form
commits the UCI configuration and triggers a service reload.

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

Smart mode requires `/usr/bin/chinadns-ng` with the tested 2025.08.09 command
interface. The binary comes from a third-party OpenWrt feed and is not vendored
or declared as an unresolved dependency in this package's stock SDK build.
`hy2route check` reports an error when smart mode is enabled and the executable
is unavailable.

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
