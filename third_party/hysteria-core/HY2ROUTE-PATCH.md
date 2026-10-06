This directory is an unmodified copy of github.com/apernet/hysteria/core/v2
v2.10.0 (upstream commit f2ad1de5da52a1da9622285a1d61553ddaa41f21), except
for one interoperability fix in client/client.go:

- Keep max_datagram_frame_size in the QUIC transport parameters. Upstream
  hardcodes OmitMaxDatagramFrameSize=true. RFC 9221 servers such as sing-box
  consequently cannot send UDP replies, even though HY2 authentication
  reports UDPEnabled=true and TCP works. Advertise datagram support instead.

The regression was reproduced with sing-box 1.14.2: DMIT received the DNS
request and its answer, but the HY2 client timed out; the official HY2 2.7.1
client succeeded. The patched client must pass both TCP and UDP probes.

Preserve upstream licensing. Update this copy deliberately when upgrading
Hysteria, and repeat the cross-implementation UDP probe.

Validation note: the project's race-enabled suite and the upstream client
and protocol tests pass. The upstream full race suite reports a race in
TestClientServerTrafficLoggerTCP: testify reflects a traffic-counter argument
while the server updates its atomic value. That server/mock path is not linked
into hy2route-core and is retained unchanged from upstream.
