# hy2route-core HY2 Transport and Landing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a reusable Hysteria2 client, direct/HTTP/SOCKS5 TCP egress, HY2 UDP, trusted DNS over HY2, and availability-first direct fallback.

**Architecture:** All data paths depend on small local interfaces rather than Hysteria concrete types. Landing handshakes run inside an HY2 TCP stream. A hysteretic health controller changes routing for new operations only; direct fallback is allowed only before application payload has been forwarded.

**Tech Stack:** Go 1.25.12, `github.com/apernet/hysteria/core/v2/client` v2.10.0, standard-library TLS/net/http primitives, fake transports and official Hysteria 2.9.2 interop test binary.

## Global Constraints

- Consume `config.Config`, `dnsproxy.Exchanger` and `policy.Observation` from Phase 1 without renaming fields or methods.
- Do not import Hysteria `app/internal` packages or copy its application modes.
- Do not implement QUIC, TLS or HY2 wire framing.
- TCP landing supports HTTP CONNECT Basic auth or SOCKS5 username/password only.
- UDP never enters the landing dialer.
- Fail-open retries direct only when HY2 dial or landing handshake fails before client payload forwarding.
- Hysteria client receive windows start at 1 MiB/4 MiB and grow no higher than configured 4 MiB/8 MiB defaults; profile before increasing.
- No production router deployment in this phase.

---

### Task 1: Transport interfaces and Hysteria client adapter

**Files:**
- Create: `internal/transport/interfaces.go`
- Create: `internal/transport/hy2/client.go`
- Create: `internal/transport/hy2/resolver.go`
- Create: `internal/transport/hy2/client_test.go`
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Produces: `transport.StreamDialer`, `transport.PacketDialer`, `transport.PacketSession`.
- Produces: `hy2.New(config.HY2Config, BootstrapResolver, EventSink) (*Client, error)`.

- [ ] **Step 1: Write adapter tests against a fake Hysteria client**

```go
package transport

type StreamDialer interface {
	Dial(context.Context, string) (net.Conn, error)
}

type PacketDialer interface {
	OpenPacket(context.Context) (PacketSession, error)
}

type PacketSession interface {
	Send([]byte, string) error
	Receive() ([]byte, string, error)
	Close() error
}

type Event struct {
	Stage  string
	Reason string
}

type EventSink interface {
	Emit(Event)
}
```

```go
func TestAdapterDelegatesTCPAndUDP(t *testing.T) {
	fake := &fakeCoreClient{tcpConn: newMemConn(), udpConn: &fakeUDP{}}
	c := newWithCoreClient(fake)
	conn, err := c.Dial(context.Background(), "203.0.113.8:443")
	if err != nil || conn != fake.tcpConn { t.Fatalf("dial = %v, %v", conn, err) }
	if fake.tcpTarget != "203.0.113.8:443" { t.Fatal(fake.tcpTarget) }
	p, err := c.OpenPacket(context.Background())
	if err != nil || p != fake.udpConn { t.Fatalf("udp = %v, %v", p, err) }
}

func TestBuildCoreConfigPinsCertificate(t *testing.T) {
	cfg := testHY2Config()
	cfg.PinnedCertSHA256 = strings.Repeat("ab", 32)
	got, err := buildCoreConfig(context.Background(), cfg,
		&fakeBootstrap{ip: netip.MustParseAddr("198.51.100.10")})
	if err != nil { t.Fatal(err) }
	err = got.TLSConfig.VerifyPeerCertificate([][]byte{[]byte("wrong")}, nil)
	if err == nil { t.Fatal("wrong certificate accepted") }
}

func TestServerDomainUsesBootstrapResolverOnly(t *testing.T) {
	bootstrap := &fakeBootstrap{ip: netip.MustParseAddr("198.51.100.10")}
	cfg := testHY2Config()
	cfg.Server = "relay.example:443"
	got, err := buildCoreConfig(context.Background(), cfg, bootstrap)
	if err != nil { t.Fatal(err) }
	if got.ServerAddr.String() != "198.51.100.10:443" { t.Fatal(got.ServerAddr) }
	if bootstrap.host != "relay.example" { t.Fatal(bootstrap.host) }
}
```

- [ ] **Step 2: Run and confirm missing interface/adapter failures**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test ./internal/transport/... -v
```

Expected: FAIL because transport packages do not exist.

- [ ] **Step 3: Implement the adapter around the pinned client API**

```go
type coreClient interface {
	TCP(string) (net.Conn, error)
	UDP() (client.HyUDPConn, error)
	Close() error
}

type BootstrapResolver interface {
	LookupIPv4(context.Context, string) (netip.Addr, error)
}

type Client struct {
	core coreClient
	sem  chan struct{}
}
```

Build the upstream config with:

```go
&client.Config{
	ServerAddr: udpAddr,
	Auth: cfg.Auth,
	TLSConfig: client.TLSConfig{
		ServerName:         cfg.SNI,
		InsecureSkipVerify: cfg.Insecure,
		VerifyPeerCertificate: pinVerifier(cfg.PinnedCertSHA256),
	},
	QUICConfig: client.QUICConfig{
		InitialStreamReceiveWindow:     cfg.InitialStreamWindow,
		MaxStreamReceiveWindow:         cfg.MaxStreamWindow,
		InitialConnectionReceiveWindow: cfg.InitialConnectionWindow,
		MaxConnectionReceiveWindow:     cfg.MaxConnectionWindow,
		MaxIdleTimeout:                 cfg.MaxIdle.Value(),
		KeepAlivePeriod:                cfg.KeepAlive.Value(),
	},
	CongestionConfig: client.CongestionConfig{
		Type:       "bbr",
		BBRProfile: "standard",
	},
}
```

Resolve an HY2 server domain through `BootstrapResolver`, whose implementation sends an A query directly to `Config.DomesticDNS`; never call the process-wide resolver because dnsmasq forwards back to this daemon. Cache the successful server IPv4 for its DNS TTL, with a five-minute minimum and one-hour maximum.

Create the client with `client.NewReconnectableClient(configFunc, connectedCallback, true)`. Limit concurrent blocking `TCP` calls with a configured semaphore. If context cancellation wins, close a connection returned later and release the semaphore; never deliver it to the caller.

Certificate pinning hashes the leaf DER with SHA-256 and uses `subtle.ConstantTimeCompare`.

- [ ] **Step 4: Verify adapter behavior and dependency reachability**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test -race ./internal/transport/... -count=20
GOTOOLCHAIN=go1.25.12 go mod verify
```

Expected: PASS; canceled dials do not leak semaphore slots.

- [ ] **Step 5: Commit**

```bash
git add internal/transport internal/config go.mod go.sum
git commit -m "feat: add reusable Hysteria transport"
```

---

### Task 2: HTTP CONNECT and SOCKS5 landing dialers

**Files:**
- Create: `internal/landing/http.go`
- Create: `internal/landing/http_test.go`
- Create: `internal/landing/socks5.go`
- Create: `internal/landing/socks5_test.go`
- Create: `internal/landing/prefixed_conn.go`

**Interfaces:**
- Produces: `landing.New(config.LandingConfig, transport.StreamDialer) (transport.StreamDialer, error)`.

- [ ] **Step 1: Write byte-exact handshake tests**

```go
func TestHTTPConnectOverHY2(t *testing.T) {
	upstream, peer := net.Pipe()
	base := &fakeDialer{conn: upstream}
	d := newHTTP(base, "landing.example:8080", "alice", "secret")
	go func() {
		br := bufio.NewReader(peer)
		line, _ := br.ReadString('\n')
		if line != "CONNECT 203.0.113.8:443 HTTP/1.1\r\n" { t.Errorf("line %q", line) }
		headers, _ := textproto.NewReader(br).ReadMIMEHeader()
		if headers.Get("Host") != "203.0.113.8:443" { t.Error(headers) }
		if headers.Get("Proxy-Authorization") != "Basic YWxpY2U6c2VjcmV0" { t.Error(headers) }
		io.WriteString(peer, "HTTP/1.1 200 Connection established\r\n\r\nEARLY")
	}()
	conn, err := d.Dial(context.Background(), "203.0.113.8:443")
	if err != nil { t.Fatal(err) }
	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "EARLY" {
		t.Fatalf("early data = %q, %v", buf, err)
	}
}

func TestSOCKS5UsernamePasswordAndIPv4Connect(t *testing.T) {
	upstream, peer := net.Pipe()
	base := &fakeDialer{conn: upstream}
	d := newSOCKS5(base, "landing.example:1080", "alice", "secret")
	errs := make(chan error, 1)
	go func() {
		defer close(errs)
		if got := readN(t, peer, 4); !bytes.Equal(got, []byte{5, 2, 0, 2}) {
			errs <- fmt.Errorf("greeting %v", got); return
		}
		peer.Write([]byte{5, 2})
		wantAuth := append([]byte{1, 5}, []byte("alice")...)
		wantAuth = append(wantAuth, 6)
		wantAuth = append(wantAuth, []byte("secret")...)
		if got := readN(t, peer, len(wantAuth)); !bytes.Equal(got, wantAuth) {
			errs <- fmt.Errorf("auth %v", got); return
		}
		peer.Write([]byte{1, 0})
		wantConnect := []byte{5, 1, 0, 1, 203, 0, 113, 8, 1, 187}
		if got := readN(t, peer, len(wantConnect)); !bytes.Equal(got, wantConnect) {
			errs <- fmt.Errorf("connect %v", got); return
		}
		peer.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	}()
	conn, err := d.Dial(context.Background(), "203.0.113.8:443")
	if err != nil { t.Fatal(err) }
	defer conn.Close()
	if err := <-errs; err != nil { t.Fatal(err) }
}
```

- [ ] **Step 2: Run and verify undefined dialers**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test ./internal/landing -v
```

Expected: FAIL because the landing package does not exist.

- [ ] **Step 3: Implement minimal handshakes with deadlines**

HTTP requirements:

```text
CONNECT <target> HTTP/1.1\r\n
Host: <target>\r\n
Proxy-Authorization: Basic <base64 user:password>\r\n  # only when configured
\r\n
```

Accept status 200 only. Bound the status line to 1024 bytes, total headers to 8192 bytes, and handshake time to five seconds. Return a `prefixedConn` so bytes already buffered after the headers are not lost.

SOCKS5 requirements:

- Offer no-auth and username/password when credentials exist; no-auth only otherwise.
- Username and password are each at most 255 bytes.
- Send CONNECT with ATYP IPv4 for IPv4 targets and ATYP domain for domain targets.
- Parse and bound every variable-length reply field.
- Treat all non-zero reply codes as handshake failure.
- Apply the same five-second handshake deadline, then clear it.

`landing.New` returns the supplied HY2 stream dialer unchanged when type is `direct`; `direct` here means HY2 connects to the final target without an additional landing proxy. It never selects the operating-system direct dialer.

- [ ] **Step 4: Run tests including malformed and oversized replies**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test -race ./internal/landing -count=20
```

Expected: PASS for success, authentication rejection, oversized headers, truncated SOCKS reply and preserved early data.

- [ ] **Step 5: Commit**

```bash
git add internal/landing
git commit -m "feat: chain TCP through HTTP and SOCKS5 landing"
```

---

### Task 3: Hysteretic fail-open controller

**Files:**
- Create: `internal/failover/controller.go`
- Create: `internal/failover/controller_test.go`
- Create: `internal/transport/failopen.go`
- Create: `internal/transport/failopen_test.go`

**Interfaces:**
- Produces: `failover.Controller.RecordFailure`, `RecordSuccess`, `Mode`, and `Snapshot`.
- Produces: `transport.NewFailOpen(proxy, direct, controller, EventSink) StreamDialer`.

- [ ] **Step 1: Write deterministic state-transition and retry tests**

```go
func TestControllerUsesThresholdCooldownAndRecovery(t *testing.T) {
	clock := newFakeClock(time.Unix(100, 0))
	c := New(Config{Failures: 3, Successes: 2, Cooldown: 30 * time.Second}, clock.Now)
	c.RecordFailure(); c.RecordFailure()
	if c.Mode() != Proxy { t.Fatal(c.Mode()) }
	c.RecordFailure()
	if c.Mode() != Direct { t.Fatal(c.Mode()) }
	clock.Add(30 * time.Second)
	c.RecordSuccess()
	if c.Mode() != Direct { t.Fatal(c.Mode()) }
	c.RecordSuccess()
	if c.Mode() != Proxy { t.Fatal(c.Mode()) }
}

func TestFailOpenRetriesDirectOnProxyDialFailure(t *testing.T) {
	proxy := &fakeDialer{err: errors.New("hy2 down")}
	direct := &fakeDialer{conn: newMemConn()}
	d := NewFailOpen(proxy, direct, degradedController(), discardEvents{})
	conn, err := d.Dial(context.Background(), "203.0.113.8:443")
	if err != nil || conn != direct.conn { t.Fatalf("dial = %v, %v", conn, err) }
}
```

- [ ] **Step 2: Run and verify missing state machine**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test ./internal/failover ./internal/transport -run 'Controller|FailOpen' -v
```

Expected: FAIL because the state machine is undefined.

- [ ] **Step 3: Implement transition-safe, rate-limited fail-open**

States are `Proxy`, `DirectCooldown`, and `DirectRecovery`. Only transitions emit events. A success before cooldown expiry does not recover; failures during recovery reset the consecutive success count.

`FailOpen.Dial` behavior:

- In `Proxy`, attempt proxy first; on dial/landing error record failure and retry direct.
- In either direct state, dial direct immediately and launch at most one background HY2 probe per health interval.
- A successfully returned proxy connection records success.
- Do not retry direct after returning a proxy connection; midstream failures cannot be replayed safely.

- [ ] **Step 4: Race-test transitions and single-flight probes**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test -race ./internal/failover ./internal/transport \
	-run 'Controller|FailOpen' -count=50
```

Expected: PASS; one transition event per change and no concurrent probe burst.

- [ ] **Step 5: Commit**

```bash
git add internal/failover internal/transport/failopen.go internal/transport/failopen_test.go
git commit -m "feat: degrade unavailable HY2 traffic to direct"
```

---

### Task 4: Trusted DNS over HY2 and direct HY2 UDP sessions

**Files:**
- Create: `internal/transport/dnstcp.go`
- Create: `internal/transport/dnstcp_test.go`
- Create: `internal/transport/udp.go`
- Create: `internal/transport/udp_test.go`
- Modify: `internal/dnsproxy/resolver.go`
- Modify: `internal/dnsproxy/resolver_test.go`
- Modify: `cmd/hy2route-core/main.go`

**Interfaces:**
- Produces: `transport.NewDNSOverStream(dialer, endpoint, timeout) dnsproxy.Exchanger`.
- Produces: `transport.NewFailOpenPacket(proxy, direct, controller) transport.PacketDialer`.

- [ ] **Step 1: Write DNS framing, HY2 UDP and fallback tests**

```go
func TestDNSOverStreamUsesRFC1035TCPFraming(t *testing.T) {
	client, server := net.Pipe()
	d := &fakeDialer{conn: client}
	ex := NewDNSOverStream(d, "8.8.8.8:53", time.Second)
	go serveOneFramedDNS(t, server, "www.google.com.", netip.MustParseAddr("142.250.1.1"))
	req := new(dns.Msg)
	req.SetQuestion("www.google.com.", dns.TypeA)
	resp, err := ex.Exchange(context.Background(), req)
	if err != nil || firstA(resp) != "142.250.1.1" { t.Fatalf("%v, %v", resp, err) }
	if d.target != "8.8.8.8:53" { t.Fatal(d.target) }
}

func TestPacketSessionNeverCallsLanding(t *testing.T) {
	hy := &fakePacketDialer{session: &fakePacketSession{}}
	session, err := hy.OpenPacket(context.Background())
	if err != nil { t.Fatal(err) }
	if err := session.Send([]byte("x"), "203.0.113.8:443"); err != nil { t.Fatal(err) }
	if hy.session.target != "203.0.113.8:443" { t.Fatal(hy.session.target) }
}
```

- [ ] **Step 2: Run and verify missing transport implementations**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test ./internal/transport ./internal/dnsproxy \
	-run 'DNSOverStream|PacketSession|TrustedFallback' -v
```

Expected: FAIL because stream DNS and packet fallback are undefined.

- [ ] **Step 3: Implement the bounded transports**

DNS over stream:

- Open one HY2 TCP stream to the trusted DNS endpoint per exchange.
- Write a two-byte big-endian payload length followed by `dns.Msg.Pack()`.
- Reject queries or replies over 4096 bytes.
- Read exactly one framed reply, unpack it and restore the request ID.
- On proxy failure, use direct TCP to the same trusted endpoint; if direct trusted DNS also fails, query the configured domestic exchanger.

Packet transport:

- `hy2.Client.OpenPacket` wraps one `client.HyUDPConn`.
- Direct fallback creates a connected IPv4 UDP socket for the session target.
- `Send` rejects payloads over 65507 bytes.
- A failed HY2 send records failure; the first failed datagram may be lost, and subsequent sends use a newly opened direct session.

Wire the Phase 1 application so domestic DNS stays direct and trusted DNS uses `NewDNSOverStream` through raw HY2 with operating-system direct fallback. User TCP uses a separate dialer graph: `landing.New(config.Landing, hy2)` wrapped by fail-open to operating-system direct. Trusted DNS never uses HTTP/SOCKS5 landing credentials.

- [ ] **Step 4: Run transport and resolver race tests**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test -race ./internal/transport ./internal/dnsproxy -count=20
```

Expected: PASS; DNS fallback preserves request IDs and UDP never calls a landing dialer.

- [ ] **Step 5: Commit**

```bash
git add internal/transport internal/dnsproxy cmd/hy2route-core/main.go
git commit -m "feat: carry trusted DNS and UDP through HY2"
```

---

### Task 5: Official HY2 interoperability and security gate

**Files:**
- Create: `tests/integration/hy2/server.yaml`
- Create: `tests/integration/hy2/run.sh`
- Create: `tests/integration/hy2/interop_test.go`
- Create: `tools/build-test-hysteria.sh`
- Modify: `README.md`

**Interfaces:**
- Verifies the production adapter against official Hysteria app v2.9.2.
- Produces no runtime API.

- [ ] **Step 1: Add an initially failing opt-in interop test**

```go
func TestOfficialServerTCPAndUDP(t *testing.T) {
	server := os.Getenv("HY2_INTEROP_SERVER")
	if server == "" { t.Skip("HY2_INTEROP_SERVER is not set") }
	cfg := testConfigFromEnv(server)
	c, err := hy2.New(cfg, testEvents{t})
	if err != nil { t.Fatal(err) }
	defer c.Close()
	assertTCPEcho(t, c)
	assertUDPEcho(t, c)
}
```

`tests/integration/hy2/run.sh` must fail if the test skips:

```sh
output="$(HY2_INTEROP_SERVER=127.0.0.1:38443 \
	HY2_INTEROP_AUTH=interop-secret \
	GOTOOLCHAIN=go1.25.12 go test ./tests/integration/hy2 -run TestOfficialServerTCPAndUDP -v)"
printf '%s\n' "$output"
printf '%s\n' "$output" | grep -Fq -- '--- PASS: TestOfficialServerTCPAndUDP'
```

- [ ] **Step 2: Run and verify the absent test server/tool failure**

Run:

```bash
chmod +x tests/integration/hy2/run.sh
./tests/integration/hy2/run.sh
```

Expected: FAIL because the official test binary/certificate/server orchestration is absent.

- [ ] **Step 3: Add deterministic official-server orchestration**

`tools/build-test-hysteria.sh`:

```sh
#!/bin/sh
set -eu
out="${1:-/tmp/hysteria-2.9.2}"
GOBIN="$(dirname "$out")" GOTOOLCHAIN=go1.25.12 \
	go install github.com/apernet/hysteria/app/v2@v2.9.2
mv "$(dirname "$out")/hysteria" "$out"
```

`run.sh` creates a directory with `mktemp -d /tmp/hy2route-interop.XXXXXX`, creates a localhost certificate with OpenSSL, substitutes the exact certificate paths into the checked-in YAML template, starts TCP and UDP echo helpers, then starts:

```bash
/tmp/hysteria-2.9.2 server -c "$interop_dir/server.yaml"
```

and registers a trap that terminates every child and removes only that exact temporary directory.

`server.yaml`:

```yaml
listen: 127.0.0.1:38443
tls:
  cert: "@CERT@"
  key: "@KEY@"
auth:
  type: password
  password: interop-secret
```

Generate the runtime YAML with:

```sh
sed -e "s|@CERT@|$interop_dir/server.crt|" \
	-e "s|@KEY@|$interop_dir/server.key|" \
	tests/integration/hy2/server.yaml > "$interop_dir/server.yaml"
```

- [ ] **Step 4: Run the Phase 2 gate**

Run:

```bash
./tools/build-test-hysteria.sh /tmp/hysteria-2.9.2
./tests/integration/hy2/run.sh
GOTOOLCHAIN=go1.25.12 gofmt -w cmd internal tests/integration
GOTOOLCHAIN=go1.25.12 go vet ./...
GOTOOLCHAIN=go1.25.12 go test -race ./...
GOTOOLCHAIN=go1.25.12 go mod verify
GOTOOLCHAIN=go1.25.12 go run golang.org/x/vuln/cmd/govulncheck@latest ./...
git diff --check
```

Expected: interop TCP and UDP PASS; all unit/race tests PASS. `govulncheck` must show no reachable vulnerability from the client binary. A module-only advisory without a reachable symbol is recorded in the release notes and does not silently disappear.

- [ ] **Step 5: Commit**

```bash
git add tests/integration/hy2 tools/build-test-hysteria.sh README.md
git commit -m "test: prove official Hysteria interoperability"
```

Phase 2 is complete only after the official-server test passes and a reviewer verifies that UDP has no reference to `internal/landing`.
