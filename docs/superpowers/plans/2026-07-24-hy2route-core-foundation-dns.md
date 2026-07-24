# hy2route-core Foundation and Smart DNS Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a host-runnable, IPv4-only `hy2route-core` that validates its complete configuration and serves bounded-memory smart DNS using China domain and IPv4 data.

**Architecture:** The daemon is split into configuration, dataset, policy and DNS packages. DNS transports and learned-policy writes are interfaces so Phase 2 can replace the trusted resolver with HY2 and Phase 3 can replace the in-memory learner with nftables without changing DNS behavior.

**Tech Stack:** Go 1.25.12, miekg/dns v1.1.72, standard-library `net/netip`, Python 3 build-time data generators, Go tests/fuzz/race detector.

## Global Constraints

- Module path is `github.com/xmdragon/hy2route`.
- Target runtime is Linux arm64 on OpenWrt 23.05.0; build with `CGO_ENABLED=0`.
- Only A queries return addresses; AAAA returns successful NODATA.
- Every cache and learned-IP table has a configured hard capacity.
- Proxy wins unresolved direct/proxy conflicts.
- No router files or production ports are changed in this phase.

---

### Task 1: Go module, command boundary and reproducible checks

**Files:**
- Create: `go.mod`
- Create when the first dependency is imported: `go.sum`
- Create: `cmd/hy2route-core/main.go`
- Create: `internal/buildinfo/buildinfo.go`
- Create: `internal/buildinfo/buildinfo_test.go`
- Create: `tests/test_go_core_contract.sh`

**Interfaces:**
- Produces: `buildinfo.Version`, `buildinfo.Commit`, and command `hy2route-core version`.
- Produces: the module pins consumed by every later task.

- [ ] **Step 1: Write the failing build-info and package contract tests**

```go
package buildinfo

import "testing"

func TestStringContainsVersionAndCommit(t *testing.T) {
	Version, Commit = "0.2.0-dev", "abc123"
	if got := String(); got != "hy2route-core 0.2.0-dev (abc123)" {
		t.Fatalf("String() = %q", got)
	}
}
```

```sh
#!/bin/sh
set -eu
grep -Fq 'go 1.25.0' go.mod
grep -Fq 'toolchain go1.25.12' go.mod
grep -Fq 'github.com/apernet/hysteria/core/v2 v2.10.0' go.mod
grep -Fq 'github.com/miekg/dns v1.1.72' go.mod
grep -Fq 'github.com/google/nftables v0.3.0' go.mod
GOTOOLCHAIN=go1.25.12 go test ./internal/buildinfo
echo 'Go core contract tests passed'
```

- [ ] **Step 2: Run the tests and verify the missing module/package failure**

Run:

```bash
chmod +x tests/test_go_core_contract.sh
./tests/test_go_core_contract.sh
```

Expected: FAIL because `go.mod` and `internal/buildinfo` do not exist.

- [ ] **Step 3: Add the pinned module and minimal command**

```go
module github.com/xmdragon/hy2route

go 1.25.0

toolchain go1.25.12

require (
	github.com/apernet/hysteria/core/v2 v2.10.0
	github.com/google/nftables v0.3.0
	github.com/miekg/dns v1.1.72
	golang.org/x/sys v0.47.0
)
```

```go
package buildinfo

import "fmt"

var (
	Version = "0.2.0-dev"
	Commit  = "unknown"
)

func String() string {
	return fmt.Sprintf("hy2route-core %s (%s)", Version, Commit)
}
```

```go
package main

import (
	"fmt"
	"os"

	"github.com/xmdragon/hy2route/internal/buildinfo"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "version" {
		fmt.Println(buildinfo.String())
		return
	}
	fmt.Fprintln(os.Stderr, "usage: hy2route-core <version|check|serve>")
	os.Exit(2)
}
```

Run:

```bash
GOTOOLCHAIN=go1.25.12 go mod tidy
```

- [ ] **Step 4: Verify host and static arm64 builds**

Run:

```bash
./tests/test_go_core_contract.sh
GOTOOLCHAIN=go1.25.12 go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOARM64=v8.0 \
	GOTOOLCHAIN=go1.25.12 go build -trimpath -o /tmp/hy2route-core-arm64 ./cmd/hy2route-core
file /tmp/hy2route-core-arm64
```

Expected: tests PASS; `file` reports an ARM aarch64 statically linked executable.

- [ ] **Step 5: Commit**

```bash
git add go.mod cmd/hy2route-core internal/buildinfo tests/test_go_core_contract.sh
git commit -m "feat: bootstrap hy2route core module"
```

---

### Task 2: Stable JSON configuration and validation

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/load.go`
- Create: `internal/config/config_test.go`
- Modify: `cmd/hy2route-core/main.go`

**Interfaces:**
- Produces: `config.Config`, `config.Load(path string) (Config, error)`, and `Config.Validate() error`.
- Consumes later: Phase 2 transport and Phase 4 UCI generator use the exact JSON names defined here.

- [ ] **Step 1: Write table tests for valid, invalid and secret-safe configuration**

```go
func TestValidateCompleteConfig(t *testing.T) {
	cfg := validConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsIPv6AndPortCollisions(t *testing.T) {
	cfg := validConfig()
	cfg.DomesticDNS = "[2001:db8::1]:53"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "domestic_dns") {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg = validConfig()
	cfg.Listen.DNS = cfg.Listen.TCP
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "listen ports") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestErrorsNeverContainCredentials(t *testing.T) {
	raw := []byte(`{"hy2":{"auth":"TOP-SECRET"}}`)
	_, err := Decode(raw)
	if err == nil || strings.Contains(err.Error(), "TOP-SECRET") {
		t.Fatalf("secret leaked in error: %v", err)
	}
}
```

- [ ] **Step 2: Run the focused tests and confirm undefined symbols**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test ./internal/config -run 'TestValidate|TestErrors' -v
```

Expected: FAIL because `Config`, `Decode` and validation do not exist.

- [ ] **Step 3: Implement the complete schema and bounded defaults**

```go
type Config struct {
	Listen      ListenConfig   `json:"listen"`
	DomesticDNS string         `json:"domestic_dns"`
	TrustedDNS  string         `json:"trusted_dns"`
	HY2         HY2Config      `json:"hy2"`
	Landing     LandingConfig  `json:"landing"`
	Limits      LimitsConfig   `json:"limits"`
	Health      HealthConfig   `json:"health"`
	Firewall    FirewallConfig `json:"firewall"`
	Rules       []RuleConfig   `json:"rules"`
	Data        DataConfig     `json:"data"`
	LogLevel    string         `json:"log_level"`
	FailOpen    bool           `json:"fail_open"`
}

type ListenConfig struct {
	DNS string `json:"dns"`
	TCP string `json:"tcp"`
	UDP string `json:"udp"`
}

type HY2Config struct {
	Server                  string        `json:"server"`
	Auth                    string        `json:"auth"`
	SNI                     string        `json:"sni"`
	Insecure                bool          `json:"insecure"`
	PinnedCertSHA256        string        `json:"pinned_cert_sha256"`
	MaxIdle                 Duration      `json:"max_idle"`
	KeepAlive               Duration      `json:"keep_alive"`
	InitialStreamWindow     uint64        `json:"initial_stream_window"`
	MaxStreamWindow         uint64        `json:"max_stream_window"`
	InitialConnectionWindow uint64        `json:"initial_connection_window"`
	MaxConnectionWindow     uint64        `json:"max_connection_window"`
}

type LandingConfig struct {
	Type     string `json:"type"`
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type LimitsConfig struct {
	DNSCacheEntries  int           `json:"dns_cache_entries"`
	LearnedIPEntries int           `json:"learned_ip_entries"`
	UDPSessions      int           `json:"udp_sessions"`
	UDPIdle          Duration      `json:"udp_idle"`
	SniffBytes       int           `json:"sniff_bytes"`
	SniffTimeout     Duration      `json:"sniff_timeout"`
}

type HealthConfig struct {
	FailureThreshold int           `json:"failure_threshold"`
	SuccessThreshold int           `json:"success_threshold"`
	Cooldown         Duration      `json:"cooldown"`
}

type FirewallConfig struct {
	Table        string `json:"table"`
	LANInterface string `json:"lan_interface"`
	Mark         uint32 `json:"mark"`
	RouteTable   uint32 `json:"route_table"`
	CanarySource string `json:"canary_source,omitempty"`
}

type RuleConfig struct {
	Action string `json:"action"`
	Type   string `json:"type"`
	Value  string `json:"value"`
}

type DataConfig struct {
	Domains string `json:"domains"`
	IPv4    string `json:"ipv4"`
}
```

Define `type Duration time.Duration` with JSON strings such as `"250ms"` and `"60s"`, reject numeric JSON durations, and expose `func (d Duration) Value() time.Duration`.

Validation requirements:

```go
var validLanding = map[string]bool{"direct": true, "http": true, "socks5": true}

// Validate must additionally enforce:
// - listen addresses are IPv4 and use three distinct ports;
// - table/interface names contain only [A-Za-z0-9_.-], mark/table are non-zero;
// - canary_source is empty or one IPv4 host address;
// - DNS endpoints are IPv4:port; HY2/landing endpoints are host:port and
//   contain no IPv6 literal;
// - auth is non-empty, SNI is a valid DNS name, and pin is empty or 64 hex characters;
// - HY2 max idle is 4s..120s and keepalive is 2s..60s;
// - stream windows default to 1 MiB/4 MiB and connection windows to
//   4 MiB/8 MiB; initial values never exceed maximum values;
// - limits are: DNS 64..65536, learned IP 64..131072, UDP 64..65536,
//   UDP idle 2s..600s, sniff bytes 1024..16384, sniff timeout 10ms..2s;
// - health thresholds are 1..10 and cooldown is 5s..15m;
// - landing credentials are permitted only for http/socks5;
// - rules are direct/proxy plus domain or IPv4/CIDR.
```

`Decode` must use `json.Decoder.DisallowUnknownFields()` and return field-oriented errors without echoing source JSON.

- [ ] **Step 4: Add `check --config` and verify redacted behavior**

```go
case "check":
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	path := fs.String("config", "/tmp/hy2route/core.json", "configuration path")
	_ = fs.Parse(os.Args[2:])
	if _, err := config.Load(*path); err != nil {
		fmt.Fprintf(os.Stderr, "configuration invalid: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("hy2route-core configuration is valid")
```

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test ./internal/config -v
GOTOOLCHAIN=go1.25.12 go test ./...
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config cmd/hy2route-core/main.go
git commit -m "feat: validate hy2route core configuration"
```

---

### Task 3: Compact China datasets and deterministic policy

**Files:**
- Create: `internal/dataset/format.go`
- Create: `internal/dataset/format_test.go`
- Create: `internal/policy/types.go`
- Create: `internal/policy/domains.go`
- Create: `internal/policy/ipv4.go`
- Create: `internal/policy/classifier.go`
- Create: `internal/policy/classifier_test.go`
- Create: `cmd/hy2route-data/main.go`
- Create: `tools/update_china_domains.py`
- Modify: `tools/update_china4.py`
- Create: `data/china-domains.txt`
- Create: `data/china4.txt`

**Interfaces:**
- Produces: `dataset.Load(path string) (dataset.Data, error)`.
- Produces: `policy.New(dataset.Data, []config.RuleConfig) (*policy.Classifier, error)`.
- Produces: `policy.NormalizeDomain(string) (string, error)`, `Classifier.Domain(string) Decision`, `Classifier.IP(netip.Addr) Decision`.

- [ ] **Step 1: Write binary round-trip, suffix and prefix tests**

```go
func TestDatasetRoundTrip(t *testing.T) {
	want := Data{
		Domains: []Domain{{Name: "wechat.com", Exact: false}, {Name: "wx.qq.com", Exact: true}},
		Prefixes: []netip.Prefix{netip.MustParsePrefix("120.232.0.0/12")},
	}
	var buf bytes.Buffer
	if err := Write(&buf, want); err != nil { t.Fatal(err) }
	got, err := Read(&buf)
	if err != nil { t.Fatal(err) }
	if diff := cmp.Diff(want, got); diff != "" { t.Fatal(diff) }
}

func TestClassifierPrecedence(t *testing.T) {
	c := testClassifier(t,
		[]string{"wechat.com"},
		[]string{"120.232.0.0/12"},
		[]config.RuleConfig{
			{Action: "proxy", Type: "domain", Value: "pay.wechat.com"},
			{Action: "proxy", Type: "ip", Value: "120.233.109.151/32"},
		},
	)
	assertDecision(t, c.Domain("img.wechat.com"), Direct, SourceChinaDomain)
	assertDecision(t, c.Domain("pay.wechat.com"), Proxy, SourceExplicitDomain)
	assertDecision(t, c.IP(netip.MustParseAddr("120.233.109.151")), Proxy, SourceExplicitIP)
	assertDecision(t, c.IP(netip.MustParseAddr("120.233.109.196")), Direct, SourceChinaIP)
}
```

- [ ] **Step 2: Run and confirm the missing packages**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test ./internal/dataset ./internal/policy -v
```

Expected: FAIL because the packages do not exist.

- [ ] **Step 3: Implement the stable binary format and classifiers**

Use this on-disk format:

```text
bytes[8]  magic "H2RDATA1"
uint32    domain count, big endian
repeat:   uint8 kind (0 suffix, 1 exact), uint16 byte length, lower-case ASCII bytes
uint32    IPv4 prefix count
repeat:   uint32 network in network byte order, uint8 prefix bits
bytes[32] SHA-256 of every preceding byte
```

Use these policy types:

```go
type Action uint8
const (
	Unknown Action = iota
	Direct
	Proxy
)

type Source string
const (
	SourceExplicitDomain Source = "explicit-domain"
	SourceExplicitIP     Source = "explicit-ip"
	SourceChinaDomain    Source = "china-domain"
	SourceChinaIP        Source = "china-ip"
	SourceDefault        Source = "default"
)

type Decision struct {
	Action Action
	Source Source
	Domain string
}
```

Domain matching must normalize lower-case names, remove the terminal dot, match on label boundaries, and reject empty labels. IPv4 matching must use a longest-prefix radix tree and reject IPv4-mapped IPv6.

- [ ] **Step 4: Add deterministic source-data generators**

`tools/update_china_domains.py` downloads:

```python
SOURCE = (
    "https://raw.githubusercontent.com/felixonmars/"
    "dnsmasq-china-list/master/accelerated-domains.china.conf"
)
```

For each non-comment line matching `server=/DOMAIN/ADDRESS`, extract `DOMAIN`, lower-case it, reject wildcard/empty/non-ASCII labels, sort uniquely, and write `data/china-domains.txt`. Print source URL, UTC retrieval date, SHA-256 and domain count in the first four comment lines.

Modify `tools/update_china4.py` so the same collapsed APNIC list is also written one CIDR per line to `data/china4.txt`; retain the existing nft output unchanged.

Run:

```bash
python3 tools/update_china_domains.py
python3 tools/update_china4.py
GOTOOLCHAIN=go1.25.12 go run ./cmd/hy2route-data \
	--domains data/china-domains.txt \
	--ipv4 data/china4.txt \
	--output /tmp/hy2route-data.bin
GOTOOLCHAIN=go1.25.12 go test ./internal/dataset ./internal/policy -v
```

Expected: generators report non-zero counts; the binary loader and classifier tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dataset internal/policy cmd/hy2route-data tools \
	data/china-domains.txt data/china4.txt files/usr/share/hy2route/china4.nft
git commit -m "feat: compile compact China routing data"
```

---

### Task 4: Bounded learned-IP table and conflict semantics

**Files:**
- Create: `internal/policy/learning.go`
- Create: `internal/policy/learning_test.go`

**Interfaces:**
- Produces: `Observation`, `LearningTable.Observe`, `Lookup`, `Expire`, and `Snapshot`.
- Phase 3 implements the same observation stream in nftables.

- [ ] **Step 1: Write TTL, conflict and capacity tests with a fake clock**

```go
func TestLearningConflictUsesInspectProxy(t *testing.T) {
	now := time.Unix(100, 0)
	table := NewLearningTable(8)
	ip := netip.MustParseAddr("203.0.113.8")
	table.Observe(Observation{Domain: "cn.example", Action: Direct, IPs: []netip.Addr{ip}, Expires: now.Add(time.Minute)})
	table.Observe(Observation{Domain: "world.example", Action: Proxy, IPs: []netip.Addr{ip}, Expires: now.Add(time.Minute)})
	got := table.Lookup(ip, now)
	if !got.Direct || !got.Proxy || got.Action != Proxy || !got.Inspect {
		t.Fatalf("conflict result = %#v", got)
	}
}

func TestLearningExpiresAndEvictsOldest(t *testing.T) {
	table := NewLearningTable(2)
	now := time.Unix(100, 0)
	ip1 := netip.MustParseAddr("192.0.2.1")
	ip2 := netip.MustParseAddr("192.0.2.2")
	ip3 := netip.MustParseAddr("192.0.2.3")
	table.Observe(Observation{Domain: "one.example", Action: Direct, IPs: []netip.Addr{ip1}, Expires: now.Add(time.Minute)})
	table.Observe(Observation{Domain: "two.example", Action: Direct, IPs: []netip.Addr{ip2}, Expires: now.Add(time.Minute)})
	table.Observe(Observation{Domain: "three.example", Action: Direct, IPs: []netip.Addr{ip3}, Expires: now.Add(time.Minute)})
	if got := table.Lookup(ip1, now); got.Action != Unknown {
		t.Fatalf("oldest entry was not evicted: %#v", got)
	}
	if got := table.Lookup(ip3, now.Add(61*time.Second)); got.Action != Unknown {
		t.Fatalf("expired entry remains: %#v", got)
	}
}
```

- [ ] **Step 2: Run and verify undefined learning API**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test ./internal/policy -run Learning -v
```

Expected: FAIL because `LearningTable` is undefined.

- [ ] **Step 3: Implement bounded indexes**

```go
type Observation struct {
	Domain  string
	Action  Action
	IPs     []netip.Addr
	Expires time.Time
}

type LearnedDecision struct {
	Action  Action
	Direct  bool
	Proxy   bool
	Inspect bool
}
```

Maintain:

- observation key `(normalized domain, action, IPv4)`;
- reverse index IPv4 to live observation keys;
- minimum-expiry heap;
- least-recently-observed list for hard-cap eviction.

`Lookup` returns proxy+inspect when both actions are live. Removing or expiring the last proxy observation changes a still-live direct address back to direct-only.

- [ ] **Step 4: Run race and capacity tests**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test -race ./internal/policy -run Learning -count=20
```

Expected: PASS with no race report and table length never exceeding its configured capacity.

- [ ] **Step 5: Commit**

```bash
git add internal/policy/learning.go internal/policy/learning_test.go
git commit -m "feat: track bounded DNS routing observations"
```

---

### Task 5: Smart DNS resolver, cache and AAAA filtering

**Files:**
- Create: `internal/dnsproxy/interfaces.go`
- Create: `internal/dnsproxy/cache.go`
- Create: `internal/dnsproxy/resolver.go`
- Create: `internal/dnsproxy/resolver_test.go`
- Create: `internal/dnsproxy/fuzz_test.go`

**Interfaces:**
- Produces: `Exchanger.Exchange(context.Context, *dns.Msg) (*dns.Msg, error)`.
- Produces: `Learner.Observe(context.Context, policy.Observation) error`.
- Produces: `Resolver.Resolve(context.Context, *dns.Msg) (*dns.Msg, error)`.

- [ ] **Step 1: Write resolver behavior tests**

```go
type Exchanger interface {
	Exchange(context.Context, *dns.Msg) (*dns.Msg, error)
}

type Learner interface {
	Observe(context.Context, policy.Observation) error
}

func TestAAAAIsNODATAWithoutUpstream(t *testing.T) {
	r := newTestResolver(t)
	req := new(dns.Msg)
	req.SetQuestion("wechat.com.", dns.TypeAAAA)
	resp, err := r.Resolve(context.Background(), req)
	if err != nil { t.Fatal(err) }
	if resp.Rcode != dns.RcodeSuccess || len(resp.Answer) != 0 {
		t.Fatalf("response = %#v", resp)
	}
	if r.domestic.calls+r.trusted.calls != 0 { t.Fatal("AAAA reached upstream") }
}

func TestUnknownUsesDomesticOnlyWhenEveryAIsChina(t *testing.T) {
	r, domestic, trusted, learner := resolverFixture(t)
	domestic.reply = aReply("unknown.example.", "120.233.109.151", "120.233.109.196")
	trusted.reply = aReply("unknown.example.", "203.0.113.8")
	resp, err := r.Resolve(context.Background(), aQuestion("unknown.example."))
	if err != nil { t.Fatal(err) }
	if got := allA(resp); !slices.Equal(got, []string{"120.233.109.151", "120.233.109.196"}) {
		t.Fatalf("domestic answer = %v", got)
	}
	if learner.last.Action != policy.Direct { t.Fatalf("observation = %#v", learner.last) }

	r, domestic, trusted, learner = resolverFixture(t)
	domestic.reply = aReply("mixed.example.", "120.233.109.151", "203.0.113.9")
	trusted.reply = aReply("mixed.example.", "203.0.113.8")
	resp, err = r.Resolve(context.Background(), aQuestion("mixed.example."))
	if err != nil { t.Fatal(err) }
	if got := allA(resp); !slices.Equal(got, []string{"203.0.113.8"}) {
		t.Fatalf("trusted answer = %v", got)
	}
	if learner.last.Action != policy.Proxy { t.Fatalf("observation = %#v", learner.last) }
}

func TestChinaDomainUsesDomesticEvenForNonChinaAddress(t *testing.T) {
	r, domestic, trusted, learner := resolverFixture(t)
	domestic.reply = aReply("wechat.com.", "203.0.113.8")
	resp, err := r.Resolve(context.Background(), aQuestion("wechat.com."))
	if err != nil { t.Fatal(err) }
	if got := allA(resp); !slices.Equal(got, []string{"203.0.113.8"}) {
		t.Fatalf("answer = %v", got)
	}
	if trusted.calls != 0 { t.Fatalf("trusted calls = %d", trusted.calls) }
	if learner.last.Action != policy.Direct { t.Fatalf("observation = %#v", learner.last) }
}
```

- [ ] **Step 2: Run the focused tests and verify failure**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test ./internal/dnsproxy -run 'TestAAAA|TestUnknown|TestChina' -v
```

Expected: FAIL because the DNS package does not exist.

- [ ] **Step 3: Implement resolver rules and bounded cache**

```go
type Resolver struct {
	classifier *policy.Classifier
	domestic   Exchanger
	trusted    Exchanger
	learner    Learner
	cache      *Cache
	now        func() time.Time
	timeout    time.Duration
}
```

Rules:

- Reject malformed requests, more than one question, non-IN class, and non-A/AAAA with `RcodeNotImplemented`.
- AAAA returns NODATA with SOA-free zero answers.
- Explicit/China direct domains query domestic only.
- Explicit proxy domains query trusted only.
- Unknown domains query both concurrently with one child context.
- Accept domestic only when it has at least one A and every A is in China IPv4; otherwise use a successful trusted answer.
- Preserve CNAMEs but learn only A records, using the minimum positive TTL clamped to 30 seconds through 24 hours.
- Negative responses cache for at most 60 seconds.
- Cache key is lower-case qname plus qtype; hard-cap eviction is LRU.
- Copy `dns.Msg` values on cache insertion and return to prevent mutation races.

- [ ] **Step 4: Add fuzz seeds and run race/fuzz smoke tests**

```go
func FuzzResolverNeverPanics(f *testing.F) {
	f.Add([]byte{0, 1, 0, 0})
	f.Add(mustPackQuestion("wechat.com.", dns.TypeA))
	f.Fuzz(func(t *testing.T, raw []byte) {
		var msg dns.Msg
		if msg.Unpack(raw) != nil { return }
		r := newTestResolver(t)
		_, _ = r.Resolve(context.Background(), &msg)
	})
}
```

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test -race ./internal/dnsproxy -count=10
GOTOOLCHAIN=go1.25.12 go test ./internal/dnsproxy -run '^$' -fuzz FuzzResolverNeverPanics -fuzztime 20s
```

Expected: PASS, no panic, race or unbounded cache length.

- [ ] **Step 5: Commit**

```bash
git add internal/dnsproxy
git commit -m "feat: resolve IPv4 with bounded smart DNS"
```

---

### Task 6: DNS server command and end-to-end host test

**Files:**
- Create: `internal/dnsproxy/server.go`
- Create: `internal/dnsproxy/server_test.go`
- Create: `internal/dnsproxy/upstream.go`
- Modify: `cmd/hy2route-core/main.go`
- Create: `tests/test_core_dns_integration.sh`

**Interfaces:**
- Produces: `dnsproxy.Server.Run(context.Context) error`.
- Produces: `hy2route-core serve --config PATH --dns-only`.

- [ ] **Step 1: Write UDP/TCP server shutdown and integration tests**

```go
func TestServerAnswersUDPAndTCPAndStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := newLoopbackTestServer(t)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	waitReady(t, s.Addr())
	assertAQuery(t, "udp", s.Addr(), "wechat.com.")
	assertAQuery(t, "tcp", s.Addr(), "wechat.com.")
	cancel()
	if err := <-done; err != nil { t.Fatal(err) }
}
```

```sh
#!/bin/sh
set -eu
bin="${1:-/tmp/hy2route-core}"
config="${2:-/tmp/hy2route-core-test.json}"
"$bin" check --config "$config"
"$bin" serve --config "$config" --dns-only > /tmp/hy2route-core-dns.log 2>&1 &
pid=$!
trap 'kill "$pid" 2>/dev/null || true; wait "$pid" 2>/dev/null || true' EXIT
sleep 1
dig @127.0.0.1 -p 2053 wechat.com A +short | grep -Eq '^[0-9]+\.'
test -z "$(dig @127.0.0.1 -p 2053 wechat.com AAAA +short)"
kill "$pid"
wait "$pid"
trap - EXIT
echo 'core DNS integration passed'
```

- [ ] **Step 2: Run and verify the absent server command**

Run:

```bash
GOTOOLCHAIN=go1.25.12 go test ./internal/dnsproxy -run TestServer -v
```

Expected: FAIL because `Server` is undefined.

- [ ] **Step 3: Implement bounded UDP/TCP listeners and signal shutdown**

`Server.Run` starts miekg/dns UDP and TCP servers on the same loopback address, uses the `Resolver` as handler, and on context cancellation calls `ShutdownContext` for both with a two-second deadline. Set UDP read size to 1232 bytes and reject larger responses with truncation rather than allocating from the packet length.

Add `serve` to `main.go`:

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
cfg, err := config.Load(*path)
if err != nil { fatal("configuration invalid", err) }
app, err := newApplication(cfg, *dnsOnly)
if err != nil { fatal("startup failed", err) }
if err := app.Run(ctx); err != nil { fatal("runtime failed", err) }
```

For Phase 1, `newApplication` uses `dnsproxy.NetworkExchanger` for both upstreams and `policy.LearningTable` through an adapter. Phase 2 replaces only the trusted exchanger.

- [ ] **Step 4: Run the complete Phase 1 gate**

Run:

```bash
GOTOOLCHAIN=go1.25.12 gofmt -w cmd internal
GOTOOLCHAIN=go1.25.12 go vet ./...
GOTOOLCHAIN=go1.25.12 go test -race ./...
GOTOOLCHAIN=go1.25.12 go test ./internal/dnsproxy -run '^$' \
	-fuzz FuzzResolverNeverPanics -fuzztime 20s
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOARM64=v8.0 \
	GOTOOLCHAIN=go1.25.12 go build -trimpath \
	-ldflags="-s -w -X github.com/xmdragon/hy2route/internal/buildinfo.Commit=$(git rev-parse --short HEAD)" \
	-o /tmp/hy2route-core-arm64 ./cmd/hy2route-core
./tests/test_go_core_contract.sh
git diff --check
```

Expected: every command exits 0. Record `/usr/bin/time -v` idle host RSS as a baseline, but do not compare it to the router acceptance limit.

- [ ] **Step 5: Commit**

```bash
git add cmd/hy2route-core internal/dnsproxy tests/test_core_dns_integration.sh
git commit -m "feat: serve smart DNS from hy2route core"
```

Phase 1 is complete only after a reviewer verifies the public interfaces listed above and the entire gate passes.
