# hy2route-core Transparent Dataplane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add bounded TLS-SNI/HTTP-Host inspection, TCP/UDP TPROXY, dynamic nftables learning sets and an in-kernel China fast path.

**Architecture:** nftables bypasses explicit and unambiguous domestic IPv4 before TPROXY. `inspect4`, explicit proxy and non-China destinations enter one Go daemon. TCP classification may use a bounded initial read; UDP conflicts default to proxy. The daemon uses the small `apernet/go-tproxy` socket helper and google/nftables netlink API, both statically linked.

**Tech Stack:** Go 1.25.12, apernet/go-tproxy pinned pseudo-version, google/nftables v0.3.0, Linux 5.15 TPROXY, network namespaces, nftables and iproute2.

## Global Constraints

- Consume Phase 1 `policy.Classifier`, `policy.LearningTable`, `dnsproxy.Learner` and Phase 2 transport interfaces unchanged.
- Inspect at most configured `sniff_bytes` and wait at most configured `sniff_timeout`.
- Never decrypt TLS or parse HTTP bodies.
- `force_proxy4` precedes `force_direct4`; `inspect4` precedes `direct4` and `china4`.
- Local, loopback, multicast and LAN-service destinations never enter TPROXY.
- UDP session count and per-session buffers are hard-bounded.
- Do not change production nftables or ports in this phase.

---

### Task 1: Bounded TLS SNI and HTTP Host inspection

**Files:**
- Create: `internal/sniff/result.go`
- Create: `internal/sniff/tls.go`
- Create: `internal/sniff/http.go`
- Create: `internal/sniff/sniff.go`
- Create: `internal/sniff/sniff_test.go`
- Create: `internal/sniff/fuzz_test.go`

**Interfaces:**
- Produces: `sniff.Peek(context.Context, net.Conn, Limits) (Result, *bufio.Reader, error)`.
- Produces: `Result{Domain string, Protocol string, Complete bool}`.

- [ ] **Step 1: Write fragmented, oversized and malformed input tests**

```go
func TestTLSClientHelloSNIAcrossFragments(t *testing.T) {
	raw := buildClientHello(t, "cdn.wechat.com")
	result := parseInChunks(t, raw, []int{1, 2, 5, 13})
	if result.Domain != "cdn.wechat.com" || result.Protocol != "tls" {
		t.Fatalf("result = %#v", result)
	}
}

func TestHTTPHostIsNormalized(t *testing.T) {
	raw := []byte("GET / HTTP/1.1\r\nHost: WWW.Example.COM:443\r\n\r\n")
	result := parseOnce(t, raw)
	if result.Domain != "www.example.com" || result.Protocol != "http" {
		t.Fatalf("result = %#v", result)
	}
}

func TestOversizedInputStopsAtLimitAndPreservesBytes(t *testing.T) {
	raw := bytes.Repeat([]byte{'x'}, 4097)
	conn := newCountingConn(raw)
	result, reader, err := Peek(context.Background(), conn, Limits{Bytes: 4096, Timeout: time.Second})
	if err != nil { t.Fatal(err) }
	if result.Domain != "" { t.Fatalf("result = %#v", result) }
	if conn.BytesRead() != 4096 { t.Fatalf("read %d bytes", conn.BytesRead()) }
	replayed, err := io.ReadAll(io.LimitReader(reader, 4096))
	if err != nil { t.Fatal(err) }
	if !bytes.Equal(replayed, raw[:4096]) { t.Fatal("replay differs") }
}
```

- [ ] **Step 2: Run and verify missing parser failure**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test ./internal/sniff -v
```

Expected: FAIL because the package does not exist.

- [ ] **Step 3: Implement defensive parsers**

```go
type Limits struct {
	Bytes   int
	Timeout time.Duration
}

type Result struct {
	Domain   string
	Protocol string
	Complete bool
}
```

TLS parser requirements:

- Require record type 22 and TLS record header length within `Limits.Bytes`.
- Parse ClientHello vectors with checked integer addition before every slice.
- Accept SNI host_name only; normalize through `policy.NormalizeDomain`.
- Stop after the first complete ClientHello or limit.

HTTP parser requirements:

- Recognize only an uppercase/lowercase HTTP method token followed by space.
- Read through `\r\n\r\n` within the limit.
- Accept exactly one Host header; remove an optional numeric port.
- Reject control characters, whitespace inside host and IPv6 literals.

`Peek` returns the same `bufio.Reader` containing every byte read so the relay can replay them once and only once.

- [ ] **Step 4: Add fuzzing and verify bounded behavior**

```go
func FuzzTLSAndHTTPParsers(f *testing.F) {
	f.Add(buildSeedClientHello("wechat.com"))
	f.Add([]byte("GET / HTTP/1.1\r\nHost: wechat.com\r\n\r\n"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 16384 { b = b[:16384] }
		_, _ = Parse(b)
	})
}
```

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test -race ./internal/sniff -count=20
GOTOOLCHAIN=go1.25.12 go test ./internal/sniff -run '^$' \
	-fuzz FuzzTLSAndHTTPParsers -fuzztime 30s
```

Expected: PASS with no panic, oversized allocation or byte loss.

- [ ] **Step 5: Commit**

```bash
git add internal/sniff
git commit -m "feat: inspect bounded TLS SNI and HTTP Host"
```

---

### Task 2: Transparent TCP listener and classified relay

**Files:**
- Create: `internal/dataplane/tcp.go`
- Create: `internal/dataplane/tcp_test.go`
- Create: `internal/dataplane/relay.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Produces: `dataplane.TCPServer.Run(context.Context) error`.
- Consumes: `policy.Classifier`, `policy.LearningTable`, direct `StreamDialer`, fail-open proxy `StreamDialer`.

- [ ] **Step 1: Write original-destination, replay and half-close tests**

```go
func TestTCPChoosesDomainOverLearnedIPAndReplaysPeekedBytes(t *testing.T) {
	client, inbound := net.Pipe()
	direct := &recordingDialer{}
	proxy := &recordingDialer{}
	s := newTCPHandler(testClassifier(), direct, proxy)
	go s.handle(context.Background(), wrapAddrConn(inbound,
		"192.168.80.20:50000", "203.0.113.8:443"))
	hello := buildClientHello(t, "wechat.com")
	go client.Write(hello)
	conn := direct.wait(t)
	if conn.target != "203.0.113.8:443" { t.Fatal(conn.target) }
	if got := readExactly(t, conn.peer, len(hello)); !bytes.Equal(got, hello) {
		t.Fatal("peeked bytes were not replayed")
	}
	if proxy.calls() != 0 { t.Fatal("proxy dialed") }
}

func TestRelayClosesWriteSideAfterEOF(t *testing.T) {
	client, inbound := tcpPair(t)
	outbound, target := tcpPair(t)
	done := make(chan error, 1)
	go func() { done <- relay(inbound, outbound, newBufferPool(32<<10, 2)) }()
	if _, err := client.Write([]byte("request")); err != nil { t.Fatal(err) }
	if err := client.CloseWrite(); err != nil { t.Fatal(err) }
	if got := string(readN(t, target, 7)); got != "request" { t.Fatal(got) }
	if _, err := target.Write([]byte("final")); err != nil { t.Fatal(err) }
	if err := target.CloseWrite(); err != nil { t.Fatal(err) }
	if got := string(readN(t, client, 5)); got != "final" { t.Fatal(got) }
	if err := <-done; err != nil { t.Fatal(err) }
}
```

- [ ] **Step 2: Run and verify missing TCP server**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test ./internal/dataplane -run TCP -v
```

Expected: FAIL because the dataplane package does not exist.

- [ ] **Step 3: Implement the Linux TPROXY server**

Pin:

```go
github.com/apernet/go-tproxy v0.0.0-20230809025308-8f4723fd742f
```

Server shape:

```go
type TCPServer struct {
	ListenAddr string
	Classifier *policy.Classifier
	Learned    *policy.LearningTable
	Direct     transport.StreamDialer
	Proxy      transport.StreamDialer
	Sniff      sniff.Limits
	MaxActive  int
}
```

Use `tproxy.ListenTCP("tcp4", addr)`. For every accepted connection:

1. Acquire the active-connection semaphore or close immediately when the hard limit is reached.
2. Read original target from `conn.LocalAddr()` and client from `conn.RemoteAddr()`.
3. If the learned target is ambiguous/inspect, call `sniff.Peek`.
4. Apply explicit domain, SNI/Host, learned IP, China IPv4, default-proxy order.
5. Dial direct or fail-open proxy before replaying the buffered bytes.
6. Copy both directions with one pooled 32 KiB buffer each.
7. Preserve TCP half-close using `CloseWrite` where supported.

- [ ] **Step 4: Run race tests and leak checks**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test -race ./internal/dataplane -run TCP -count=30
GOTOOLCHAIN=go1.25.12 go test ./internal/dataplane -run TCP -count=100
```

Expected: PASS; active count returns to zero and goroutine count stabilizes after connections close.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/dataplane
git commit -m "feat: relay classified transparent TCP"
```

---

### Task 3: Bounded transparent UDP sessions

**Files:**
- Create: `internal/dataplane/udp.go`
- Create: `internal/dataplane/udp_test.go`
- Create: `internal/dataplane/session_table.go`
- Create: `internal/dataplane/session_table_test.go`

**Interfaces:**
- Produces: `dataplane.UDPServer.Run(context.Context) error`.
- Consumes: direct and fail-open `transport.PacketDialer`; never consumes a landing dialer.

- [ ] **Step 1: Write tuple, expiry, capacity and response tests**

```go
func TestUDPSessionKeyIncludesSourceAndDestination(t *testing.T) {
	a := sessionKey{Source: mustAddrPort("192.168.80.20:40000"), Target: mustAddrPort("8.8.8.8:53")}
	b := sessionKey{Source: mustAddrPort("192.168.80.20:40000"), Target: mustAddrPort("1.1.1.1:53")}
	if a == b { t.Fatal("destinations collapsed") }
}

func TestSessionTableEvictsLRUAtCapacity(t *testing.T) {
	table := newSessionTable(2, time.Minute, fakeClock())
	first := table.add(key(1), fakeSession())
	table.add(key(2), fakeSession())
	table.add(key(3), fakeSession())
	if !first.closed.Load() || table.len() != 2 { t.Fatal("LRU not evicted") }
}

func TestUDPConflictUsesHY2AndNotLanding(t *testing.T) {
	learned := policy.NewLearningTable(8)
	target := netip.MustParseAddr("203.0.113.8")
	expires := time.Now().Add(time.Minute)
	learned.Observe(policy.Observation{Domain: "cn.example", Action: policy.Direct, IPs: []netip.Addr{target}, Expires: expires})
	learned.Observe(policy.Observation{Domain: "world.example", Action: policy.Proxy, IPs: []netip.Addr{target}, Expires: expires})
	hy2 := &fakePacketDialer{session: &fakePacketSession{}}
	direct := &fakePacketDialer{session: &fakePacketSession{}}
	s := newUDPServerForTest(learned, direct, hy2)
	err := s.handleFirst(context.Background(),
		mustAddrPort("192.168.80.20:40000"),
		mustAddrPort("203.0.113.8:443"), []byte("payload"))
	if err != nil { t.Fatal(err) }
	if hy2.opens != 1 || direct.opens != 0 {
		t.Fatalf("HY2 opens=%d direct opens=%d", hy2.opens, direct.opens)
	}
}
```

- [ ] **Step 2: Run and verify missing UDP implementation**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test ./internal/dataplane -run 'UDP|Session' -v
```

Expected: FAIL because UDP server/session table are undefined.

- [ ] **Step 3: Implement per-tuple transparent sockets**

Use:

- `tproxy.ListenUDP("udp4", listenAddr)` for first packets.
- `tproxy.ReadFromUDP` to obtain client and original target.
- `tproxy.DialUDP("udp4", target, client)` so later packets for that tuple move to the connected transparent socket and replies preserve the original source.

For each new tuple:

- Copy the first datagram before returning the shared receive buffer.
- Choose direct only for an unambiguous direct decision; conflict/unknown uses HY2.
- Open exactly one `PacketSession`.
- Start two forwarding loops and refresh one inactivity deadline.
- Allocate fixed 64 KiB buffers from a bounded pool.
- Enforce `limits.udp_sessions`; evict least-recently-used session and close both sides.
- Treat idle timeout as normal closure.

- [ ] **Step 4: Race-test churn and assert memory bounds**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test -race ./internal/dataplane \
	-run 'UDP|Session' -count=30
GOTOOLCHAIN=go1.25.12 go test ./internal/dataplane \
	-run TestSessionTableEvictsLRUAtCapacity -count=1000
```

Expected: PASS; the session table and buffer pool never exceed configured capacity.

- [ ] **Step 5: Commit**

```bash
git add internal/dataplane/udp.go internal/dataplane/udp_test.go \
	internal/dataplane/session_table.go internal/dataplane/session_table_test.go
git commit -m "feat: relay bounded transparent UDP sessions"
```

---

### Task 4: Dynamic direct4/inspect4 nftables reconciler

**Files:**
- Create: `internal/firewall/client.go`
- Create: `internal/firewall/learner.go`
- Create: `internal/firewall/learner_test.go`
- Create: `internal/firewall/nft_linux.go`
- Create: `internal/firewall/nft_linux_test.go`

**Interfaces:**
- Produces: `firewall.Learner`, which implements `dnsproxy.Learner`.
- Produces: `SetClient.Replace(ctx, ip, state, ttl) error`.
- Produces: `SetClient.Heartbeat(ctx, ttl) error` for crash-safe nft fail-open.

- [ ] **Step 1: Write state-transition and batched-write tests**

```go
func TestLearnerMovesConflictFromDirectToInspectAndBack(t *testing.T) {
	table := policy.NewLearningTable(16)
	sets := &fakeSetClient{}
	l := NewLearner(table, sets, fakeClock())
	ip := netip.MustParseAddr("203.0.113.8")
	l.Observe(ctx, obs("cn.example", policy.Direct, ip, time.Minute))
	assertLastSet(t, sets, ip, SetDirect)
	l.Observe(ctx, obs("world.example", policy.Proxy, ip, 10*time.Second))
	assertLastSet(t, sets, ip, SetInspect)
	fakeClock().Add(11 * time.Second)
	l.Expire(ctx)
	assertLastSet(t, sets, ip, SetDirect)
}

func TestSetWritesAreCoalesced(t *testing.T) {
	table := policy.NewLearningTable(256)
	sets := &fakeSetClient{}
	l := NewLearner(table, sets, fakeClock())
	ips := make([]netip.Addr, 100)
	for i := range ips {
		ips[i] = netip.AddrFrom4([4]byte{198, 51, 100, byte(i + 1)})
	}
	err := l.Observe(ctx, policy.Observation{
		Domain: "bulk.example", Action: policy.Proxy, IPs: ips,
		Expires: time.Now().Add(time.Minute),
	})
	if err != nil { t.Fatal(err) }
	if sets.flushes != 1 || len(sets.updates[0]) != 100 {
		t.Fatalf("flushes=%d updates=%d", sets.flushes, len(sets.updates[0]))
	}
}
```

- [ ] **Step 2: Run and verify absent firewall package**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test ./internal/firewall -v
```

Expected: FAIL because firewall learner does not exist.

- [ ] **Step 3: Implement netlink updates with element TTL**

Use `github.com/google/nftables`:

```go
type SetState uint8
const (
	SetNone SetState = iota
	SetDirect
	SetInspect
)

type SetClient interface {
	Replace(context.Context, []SetUpdate) error
	Heartbeat(context.Context, time.Duration) error
}

type SetUpdate struct {
	IP    netip.Addr
	State SetState
	TTL   time.Duration
}
```

`nft_linux.go` obtains the table named by `config.Config.Firewall.Table`, sets `direct4`, `inspect4`, and verdict map `core_state`. For each update, delete the key from both sets, then add it only to the selected set with `SetElement.Timeout`. Flush one batch. Clamp kernel TTL to one second through 24 hours.

After DNS/TCP/UDP listeners are bound, refresh `core_state` element `1 : jump active` every three seconds with a ten-second timeout. If the process crashes, the element expires and the outer nft chain returns without entering `active`, so new traffic is direct. Never refresh the heartbeat before every listener is ready.

The learner updates its in-memory table first, derives all changed IP states, and performs one batch. On netlink failure it keeps the observations, reports unhealthy status, and retries a full snapshot reconciliation with exponential backoff capped at 30 seconds.

- [ ] **Step 4: Run unit tests and an opt-in kernel smoke test**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test -race ./internal/firewall -count=20
if sudo -n true 2>/dev/null; then
	HY2ROUTE_NFT_TEST=1 GOTOOLCHAIN=go1.25.12 \
		sudo --preserve-env=HY2ROUTE_NFT_TEST,GOTOOLCHAIN \
		go test ./internal/firewall \
		-run 'TestKernel(SetTimeout|VerdictMapTimeout)$' -v
else
	echo 'SKIP: nftables kernel tests require passwordless sudo'
fi
```

Expected: unit tests always PASS. When passwordless test root is available, the kernel tests create only `inet hy2route_test`, verify both ordinary set-element expiry and `1 : jump active` verdict-map expiry, and delete that exact table in cleanup. A running kernel that rejects timed verdict-map elements fails the task; do not weaken the crash-safe gate or continue to Task 5.

- [ ] **Step 5: Commit**

```bash
git add internal/firewall
git commit -m "feat: reconcile learned IPv4 nftables sets"
```

---

### Task 5: Ordered nftables rules and namespace end-to-end gate

**Files:**
- Modify: `files/usr/libexec/hy2route/generate.uc`
- Create: `tests/test_core_nft_contract.sh`
- Create: `tests/integration/netns/run.sh`
- Create: `tests/integration/netns/nft_test.go`
- Modify: `cmd/hy2route-core/main.go`

**Interfaces:**
- Produces nftables sets `force_proxy4`, `force_direct4`, `inspect4`, `direct4`, `china4` and verdict map `core_state`.
- Starts Phase 1 DNS plus Phase 2 transport plus Phase 3 TCP/UDP servers in one process.

- [ ] **Step 1: Write a failing rule-order contract**

```sh
#!/bin/sh
set -eu
out="$(mktemp)"
trap 'rm -f "$out"' EXIT
./tests/fixtures/run-generator.sh nft > "$out"
line() { grep -nF "$1" "$out" | head -n1 | cut -d: -f1; }
proxy="$(line 'ip daddr @force_proxy4')"
direct="$(line 'ip daddr @force_direct4 return')"
inspect="$(line 'ip daddr @inspect4')"
learned="$(line 'ip daddr @direct4 return')"
china="$(line 'ip daddr @china4 return')"
test "$proxy" -lt "$direct"
test "$direct" -lt "$inspect"
test "$inspect" -lt "$learned"
test "$learned" -lt "$china"
grep -Fq 'fib daddr type local return' "$out"
grep -Fq '1 vmap @core_state' "$out"
echo 'core nft contract passed'
```

- [ ] **Step 2: Run and confirm missing sets/order**

Run:

```bash
chmod +x tests/test_core_nft_contract.sh
./tests/test_core_nft_contract.sh
```

Expected: FAIL because `inspect4` and `direct4` are absent.

- [ ] **Step 3: Generate the exact fast-path order**

The outer prerouting chain must emit:

```nft
iifname != "br-lan" return
fib daddr type local return
1 vmap @core_state
return
```

The `active` chain must emit:

```nft
ip daddr 127.0.0.0/8 return
ip daddr 224.0.0.0/4 return
ip daddr @force_proxy4 meta l4proto tcp tproxy ip to :12345 meta mark set 102 accept
ip daddr @force_proxy4 meta l4proto udp tproxy ip to :12345 meta mark set 102 accept
ip daddr @force_direct4 return
ip daddr { 10.0.0.0/8, 169.254.0.0/16, 172.16.0.0/12, 192.168.0.0/16 } return
ip daddr @inspect4 meta l4proto tcp tproxy ip to :12345 meta mark set 102 accept
ip daddr @inspect4 meta l4proto udp tproxy ip to :12345 meta mark set 102 accept
ip daddr @direct4 return
ip daddr @china4 return
meta l4proto tcp tproxy ip to :12345 meta mark set 102 accept
meta l4proto udp tproxy ip to :12345 meta mark set 102 accept
```

Create `direct4` and `inspect4` as `ipv4_addr` sets with `flags timeout`; keep `china4` and explicit CIDRs as interval sets. Create `core_state` as an integer-to-verdict map with timeout support and no permanent element.

Wire `main.go` so `serve` starts DNS, TCP and UDP under one cancelable application group. If any listener fails, cancel all listeners and return a stage-labeled error.

- [ ] **Step 4: Add privileged network namespace coverage**

`tests/integration/netns/run.sh` creates uniquely named namespaces from `$$`, registers cleanup before creation, and proves:

- China IPv4 bypass does not reach the core counter.
- non-China TCP reaches TPROXY and preserves original target;
- learned `direct4` bypasses;
- adding the same IP to `inspect4` overrides `direct4` and `china4`;
- UDP receives original source/target and replies with the original target as source;
- a one-second dynamic element expires and returns to proxy behavior.
- stopping the heartbeat for 11 seconds bypasses the complete `active` chain and makes new traffic direct.

Run:

```bash
./tests/test_core_nft_contract.sh
sudo tests/integration/netns/run.sh
GOTOOLCHAIN=go1.25.12 go test -race ./...
GOTOOLCHAIN=go1.25.12 go vet ./...
git diff --check
```

Expected: all paths PASS and the script leaves no `h2r-*` namespace or `hy2route_test` nftables table.

- [ ] **Step 5: Commit**

```bash
git add files/usr/libexec/hy2route/generate.uc tests/test_core_nft_contract.sh \
	tests/integration/netns cmd/hy2route-core/main.go
git commit -m "feat: activate transparent hy2route core dataplane"
```

Phase 3 is complete only after `TestKernelVerdictMapTimeout` and the namespace gate pass on Linux 5.15 or newer, and a reviewer confirms the rule order keeps domestic traffic out of userspace.
