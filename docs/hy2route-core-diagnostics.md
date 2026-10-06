# HY2 and SOCKS5 connection diagnostics

Starting with package `0.2.0-3`, `hy2route status` includes
`hy2_last_error_stage` and `hy2_last_error_reason` alongside the existing
`hy2_last_error` timestamp. These describe the last HY2 transport failure,
including TLS certificate verification errors, even when fail-open routing
has subsequently attempted a direct connection to the landing proxy.

For example, an expired relay certificate reports a reason containing
`tls: failed to verify certificate: x509: certificate has expired`.
The error timestamp and reason remain available after recovery; use
`hy2_state` and `hy2_connected` to determine the current state.

The same reason appears in `logread -e hy2route` as `stage=hy2.tcp` or
`stage=hy2.udp`. The first failure of an outage is logged immediately;
continued failures are logged at most once per minute. Status still updates
on every accepted failure. Errors from older connection generations do not
overwrite a newer successful handshake. `log_level=none` disables these
HY2 log messages without removing status diagnostics.

Configured relay and landing credentials are redacted from HY2 diagnostics.
Messages are flattened to one line and limited to 1,024 characters plus an
ellipsis. No credential configuration fields are added to the control API.

SOCKS5 errors now name the failing operation and preserve underlying read
errors. For example, `SOCKS5 greeting read: i/o timeout` means that a reply
was not received in time; it is not an authentication rejection.
`SOCKS5 method rejected` is reserved for the server's explicit `0xff`
response. Authentication and CONNECT rejections include the returned status
code, while malformed replies and methods that were not offered have their
own errors.

If one client connects while another reports certificate expiry, compare
certificate verification settings before replacing credentials or changing
the transport. Renewing the relay certificate fixes the verification failure.

## DNS policy bypass

Package `0.2.0-4` gives dnsmasq only one default upstream: the core DNS
listener. Explicit direct domains and relay bootstrap domains still have
their domain-specific direct upstreams. Domestic/trusted selection and DNS
fallback remain the core's responsibility.

Previously, dnsmasq also received a default bootstrap DNS server. On the
router this returned direct DNS answers for Google even while the core and
the HY2 trusted DNS path returned working answers. Removing this extra
default upstream and restarting dnsmasq clears the affected cache and keeps
normal DNS requests within the core's routing policy.

## Hybrid relay paths (0.2.0-5)

`tcp_transport` and `udp_transport` report the last selected relay path.
Reality-enabled TCP prefers Reality, and UDP prefers HY2. A failed relay
enters a cooldown; a working backup does not change TCP's SOCKS/HTTP landing.
UDP uses HY2 or Reality XUDP without the landing. Transport selection applies
to new connections; an existing TCP connection must reconnect after failure.

Use `hy2route-core probe --network tcp|udp --transport auto|reality|hy2` to
exercise the configured path, or `--config` for a staged configuration.
TCP verifies a real HTTP request through the landing. UDP verifies a complete
DNS round trip rather than relying on `UDPEnabled` during HY2 authentication.
Keep credentials in the configuration file; probe arguments contain no secrets.

With `main.fail_open=0`, both relays being unavailable fails proxied requests
instead of selecting the local network as a third path. Domestic and explicit
direct rules remain direct. `udp_policy=direct|block` is now also honored by
the custom core.
