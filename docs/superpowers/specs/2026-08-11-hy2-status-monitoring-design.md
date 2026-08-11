# HY2 Status Monitoring Design

## Goal

Replace the permanently false `hy2_connected` status with a backward-compatible,
event-driven view of the HY2 transport's last known health. Add enough context to
distinguish a client that has not connected yet from one that connected successfully
or most recently encountered a transport error.

## Status Contract

The control snapshot keeps the existing `hy2_connected` boolean and adds three
fields:

- `hy2_state`: one of `idle`, `connected`, or `degraded`.
- `hy2_last_success`: the UTC RFC3339Nano timestamp of the latest successful HY2
  handshake, omitted until the first success.
- `hy2_last_error`: the UTC RFC3339Nano timestamp of the latest HY2 TCP or UDP
  transport error, omitted until the first error.

The fields describe the last state observed by the reconnectable HY2 client. They do
not claim that an idle QUIC socket remains open. This distinction matters because
hy2route intentionally permits idle HY2 sessions to close and reconnect on demand.

The state transitions are:

- Process start: `idle`, `hy2_connected=false`, with both timestamps absent.
- Successful HY2 handshake: `connected`, `hy2_connected=true`, and update
  `hy2_last_success`.
- HY2 TCP or UDP transport error: `degraded`, `hy2_connected=false`, and update
  `hy2_last_error`.
- Successful reconnect handshake: return to `connected` and update
  `hy2_last_success`; retain the previous error timestamp for diagnostic context.

## Architecture

Add an application-owned, concurrency-safe HY2 status tracker in
`cmd/hy2route-core`. The tracker implements `transport.EventSink`, accepts the
existing `hy2.connected`, `hy2.tcp`, and `hy2.udp` events, and exposes an immutable
snapshot for the control response.

`newApplication` constructs one tracker, passes it to `hy2.New`, and stores it on the
application. The control snapshot reads the tracker instead of initializing
`hy2_connected` to false. The tracker owns its clock so unit tests can assert exact
timestamps without sleeping.

No periodic probe is added. Active probes would create continuous traffic and could
misclassify a landing proxy or probe target failure as an HY2 transport failure. The
existing data path and reconnect behavior remain unchanged.

## Error Handling and Security

Only event stage and occurrence time affect the public status. Error reasons,
addresses, authentication values, and other transport details are not added to the
control response. Existing socket permissions remain `0600`, and the existing status
contract continues to contain no secrets.

Unrelated event stages are ignored. The tracker uses a mutex so handshake callbacks,
TCP dials, UDP sessions, and control requests may run concurrently without races.

## Testing

Tests cover:

- The initial idle snapshot and omitted timestamps.
- A successful handshake transition to connected.
- TCP and UDP error transitions to degraded.
- Recovery after a later successful handshake while retaining the last error time.
- Ignoring unrelated transport events.
- JSON/control-socket compatibility, including the existing boolean and the absence
  of secret fields.
- Race-safe behavior through the repository's Go race tests.

The implementation follows test-driven development: each behavior is first expressed
as a failing test, the failure is observed, and then the minimum production change is
added.

## Deployment and Acceptance

Build the ARM64 OpenWrt package with the repository's existing build tooling. Before
installing it, preserve the router's current package and `/etc/config/hy2route` using
the established deployment workflow. Install the new package, restart only
`hy2route`, and verify:

1. The service, control socket, nftables heartbeat, policy route, and DNS listeners
   are active.
2. Status starts with a valid `hy2_state` and never reports the old permanently false
   value after a successful HY2 handshake.
3. A LAN-bound HTTPS request exits through `64.32.180.57`.
4. `hy2_state=connected`, `hy2_connected=true`, and `hy2_last_success` is populated
   after the request.
5. No new authentication, SOCKS5, OOM, or service crash errors appear.

After deployment verification, commit all implementation and plan changes and push
the branch to the configured remote without force-pushing.
