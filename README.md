# hy2route

A small OpenWrt transparent proxy with protocol-specific relay paths:

```text
TCP: LAN -> VLESS Reality relay -> SOCKS5/HTTP landing -> Internet
     fallback: LAN -> HY2 relay -> the same landing -> Internet
UDP: LAN -> HY2 relay -> optional SOCKS5 UDP landing -> Internet
     fallback: LAN -> VLESS Reality (XUDP) relay -> the same UDP landing -> Internet
DNS: domestic DNS directly; trusted DNS over the relay, without the landing
```

The optional `tcp_relay` is disabled by default. Without it, TCP continues to
use HY2 followed by the landing. Set `landing.udp=1` to send proxied UDP
through the SOCKS5 landing too. This requires UDP ASSOCIATE support; rejected
associations fail instead of silently using relay-only egress. HTTP CONNECT
landings cannot carry this UDP path. The default `landing.udp=0` preserves
relay-only UDP egress for existing configurations.

Sessions are keyed by both source and destination, so each target has its own
UDP association. This also handles landings that pin an association to its
first destination. `landing.udp_max_payload` rejects oversized payloads before
sending them; zero disables the limit. Choose the limit from an actual echo
test: some landings truncate packets because their receive buffer includes the
SOCKS header. With IPv4, a 2,048-byte buffer leaves 2,038 bytes for the payload.

## Routing and DNS

Private addresses, the relay and landing endpoints bypass interception.
Explicit proxy rules win over explicit direct rules, followed by mainland
China IP/domain rules, then the default proxy path. DNS learns the same domain
policy into nftables sets. LAN IPv6 forwarding is blocked by default until a
matching IPv6 proxy policy is configured.

`udp_policy=proxy` uses the UDP relay path; `direct` uses the local network;
`block` rejects proxied UDP. Existing domestic/explicit direct routes remain
in effect for all three policies.

Trusted DNS uses TCP through Reality (HY2 as backup) when `tcp_relay` is
enabled. It never depends on UDP support or DNS handling at the landing.
Domestic DNS continues to use `bootstrap_dns` directly.

## Failover

Relay failures apply a cooldown to new connections; the primary is retried
when that cooldown ends. TCP fallback changes the relay transport while
preserving the landing destination and credentials. It does not replay bytes
from an established TCP connection, so interrupted connections must reconnect.
UDP can retry a failed send on the backup; failed receive sessions are removed
so subsequent datagrams can create a new session. Normal session expiry does
not mark the relay unhealthy.

Set `main.fail_open=0` to keep proxied traffic on the two configured relays.
When both fail, requests fail instead of using local direct egress. The default
`fail_open=1` retains the earlier local fallback behavior for compatibility.
TCP and UDP relay cooldowns are independent, so a HY2 outage does not disable
a working Reality TCP path.

Datagram loss without a reported transport error is not proof of a failed
relay. The core does not automatically change paths for individual lost
packets; use the UDP probe to verify actual round trips when diagnosing a
partially broken relay.

## Configure

Use LuCI **Services → hy2route**, or edit `/etc/config/hy2route`:

- `relay`: HY2 server, port, authentication and certificate SNI.
- `tcp_relay`: optional VLESS Reality server, UUID (`id`), server name,
  X25519 public key (`reality_password`), short ID, fingerprint and Vision flow.
- `landing`: SOCKS5 or HTTP server, credentials, optional UDP egress and payload limit.
- `main`: UDP policy, DNS, bypass marks, resource limits and fail-open behavior.
- `rule`: explicit direct/proxy IPv4 CIDR or domain rules.

Keep `allow_insecure=0` for a relay with a verifiable certificate. Node secrets
belong in the router's root-readable configuration, not source control.

```sh
hy2route check
/etc/init.d/hy2route enable
/etc/init.d/hy2route start
hy2route status
```

The service refuses to start while Passwall2 is running. Its generated
configuration is validated before installing firewall rules. `procd`
supervises one `hy2route-core` process, with `GOMEMLIMIT=64MiB` and `GOGC=50`.
No external Xray or sing-box daemon is required; Reality TLS and Vision reuse
upstream sing-box/sing-vmess libraries inside the existing process.

## Probe actual egress

Probes do not start transparent listeners or change firewall rules:

```sh
hy2route-core probe --network tcp
hy2route-core probe --network udp
hy2route-core probe --network tcp --transport reality
hy2route-core probe --network tcp --transport hy2
hy2route-core probe --network udp --transport hy2 --target 8.8.8.8:53
hy2route-core probe --network udp --transport reality
```

TCP probes request `https://api.ipify.org` through the landing; `--url` can
select another HTTP(S) target. UDP probes query `example.com` through the relay,
validate the DNS transaction ID and response code, and report the answers.
`--config` accepts a staged configuration for checks before a cutover or fault
injection. `status` reports the last selected `tcp_transport` and
`udp_transport`, plus the existing HY2 connection diagnostics.

## Build and test

Go 1.25.12 and an OpenWrt 23.05 SDK targeting `mediatek/filogic` are used.
Reality requires the `with_utls` build tag, included by `tools/build-core.sh`.

```sh
go test -race -tags with_utls ./...
tools/build-core.sh
for test in tests/test_*contract.sh; do sh "$test" || exit; done
```

Copy the repository into `package/hy2route` in the matching OpenWrt SDK, then
run `make package/hy2route/compile V=s`. The package contains the statically
linked ARM64 core, routing data, configuration generator, service and LuCI UI.

`third_party/hysteria-core` pins HY2 core v2.10.0 with a single QUIC Datagram
negotiation compatibility patch. See its `HY2ROUTE-PATCH.md` for upstream
provenance, the reproduction and the cross-implementation test requirement.

## Deployment

Back up the router configuration and installed artifacts before switching.
First probe a staged config with both transports. Verify TCP exits from the
landing and UDP works through each relay and the selected landing policy. Inject unavailable relay addresses
only into staged configs to check both fallback paths and the both-down case.

For a migration, switch HY2 to the new relay first, then enable `tcp_relay`.
Verify DNS, domestic bypass, explicit direct rules, IPv6 policy, resource usage
and actual client traffic after each step. Roll back configuration and core
together if verification fails. Retain the backup on the router.

## Windows clients

When the router owns proxy routing, a separate Windows TUN client can override
it. `tools/configure-windows-router.ps1` runs in administrator PowerShell,
backs up adapter metrics, default routes, DNS and the selected v2rayN config,
disables that application's TUN mode, and prefers the router's adapter.
Specify `-InterfaceIndex` when multiple adapters use the same router gateway.
`-V2rayConfig` is optional and must identify the active installation; other VPN
applications are not stopped. Wi-Fi remains available as a lower-priority
connection. The script prints the backup directory for rollback.

If a separate LAN VPN client sends HY2 to a relay on UDP/443, an existing
OpenWrt `Block-LAN-QUIC-UDP443` forwarding rule can reject those packets even
when router-originated HY2 works. Place a narrow LAN-to-WAN UDP/443 allow rule
for the configured relay addresses before that QUIC rule. Keep this distinct
from transparent application UDP, which is handled by the core.

For proxy TLS connections, `main.tcp_domain_dial=1` sends the sniffed SNI and
original port to the landing for resolution. The default is disabled. This
changes the connection destination, not the ClientHello or application bytes.
Direct routes, explicit IP rules, absent/incomplete SNI and ECH retain the
original IP. HTTP Host and UDP targets are not rewritten. It can help when
client-selected IPs are unreachable from the landing, but is unsuitable for
connections that require a fixed destination IP. It does not retry or replay
an application request after TLS failure.
