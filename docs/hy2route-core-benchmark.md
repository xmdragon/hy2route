# hy2route-core deployment evidence

## Environment

- Router: Xiaomi WR30U, OpenWrt 23.05, Linux 5.15.134, ARM Cortex-A53, IPv4 only.
- Core artifact SHA-256: `dc7b74412940eae407637edb3b209dd3359959151fcdbafa238c6bf01de733bb`.
- Routing data SHA-256: `984ce6ba5974a3e1c0d7e10bad452cefa6b0965d44c0684167463adaebb31240`.
- Runtime guard: `GOMEMLIMIT=64MiB`, `GOGC=50`.

## Cutover checks

- Core control socket, nft `core_state`, and dnsmasq upstream `127.0.0.1#1053`: passed.
- `wechat.com` DNS returned `120.233.109.151` and `120.233.109.196`.
- `www.google.com` DNS returned IPv4 answers through trusted DNS.
- Google `generate_204`: HTTP 204 in 1.315 s.
- WeChat HTTPS: HTTP 302 in 0.425 s.
- UDP DNS check: `wechat.com` 28 ms and `www.google.com` 440 ms, both answered over UDP by `192.168.80.1:53`.
- Legacy Xray was stopped only after the core checks passed; complete backup remains at `/root/hy2route-backup-20260724-071816-core`.
- First post-cutover soak sample: RSS `24,432 kB`, high-water `27,036 kB`; nft heartbeat had `8.68 s` remaining from a 10-second lease.
- Second five-minute sample: RSS `23,796 kB`, high-water unchanged at `27,036 kB`; this meets the 24 MiB idle-RSS target.
- A 20-request concurrent HTTPS smoke workload (10 Google, 10 WeChat) completed without client errors; RSS and high-water remained `23,796/27,036 kB`.

## Pending gates

- Measured iperf3 domestic/overseas throughput and UDP loss/session churn.
- 72-hour five-minute health/RSS/oom/crash observation.
