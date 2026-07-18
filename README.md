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
- With a SOCKS5 landing, DNS goes through the chain to avoid polluted direct
  DNS. HTTP landing mode uses the configured bootstrap DNS directly because
  an HTTP proxy cannot transport dnsmasq's UDP upstream queries.

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

## Build

Copy this directory into `package/hy2route` in an OpenWrt 23.05 SDK matching the
router target, refresh the China snapshot, then build the package:

```sh
python3 tools/update_china4.py
make package/hy2route/compile V=s
```

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
