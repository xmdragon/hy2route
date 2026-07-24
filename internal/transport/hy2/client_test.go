package hy2

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	coreclient "github.com/apernet/hysteria/core/v2/client"
	"github.com/miekg/dns"
	"github.com/xmdragon/hy2route/internal/config"
)

type fakeCoreClient struct {
	tcpConn   net.Conn
	tcpTarget string
	udpConn   coreclient.HyUDPConn
}

func (fake *fakeCoreClient) TCP(target string) (net.Conn, error) {
	fake.tcpTarget = target
	return fake.tcpConn, nil
}

func (fake *fakeCoreClient) UDP() (coreclient.HyUDPConn, error) { return fake.udpConn, nil }
func (fake *fakeCoreClient) Close() error                       { return nil }

type fakeUDP struct{}

func (*fakeUDP) Send([]byte, string) error        { return nil }
func (*fakeUDP) Receive() ([]byte, string, error) { return nil, "", nil }
func (*fakeUDP) Close() error                     { return nil }

type fakeBootstrap struct {
	host string
	ip   netip.Addr
}

func (bootstrap *fakeBootstrap) LookupIPv4(_ context.Context, host string) (netip.Addr, error) {
	bootstrap.host = host
	return bootstrap.ip, nil
}

func TestAdapterDelegatesTCPAndUDP(t *testing.T) {
	clientSide, peer := net.Pipe()
	defer peer.Close()
	fake := &fakeCoreClient{tcpConn: clientSide, udpConn: &fakeUDP{}}
	adapter := newWithCoreClient(fake, 1)
	conn, err := adapter.Dial(context.Background(), "203.0.113.8:443")
	if err != nil || conn != fake.tcpConn {
		t.Fatalf("dial = %v, %v", conn, err)
	}
	if fake.tcpTarget != "203.0.113.8:443" {
		t.Fatal(fake.tcpTarget)
	}
	packet, err := adapter.OpenPacket(context.Background())
	if err != nil || packet != fake.udpConn {
		t.Fatalf("udp = %v, %v", packet, err)
	}
}

func TestBuildCoreConfigPinsCertificate(t *testing.T) {
	cfg := testHY2Config()
	cfg.PinnedCertSHA256 = strings.Repeat("ab", 32)
	got, err := buildCoreConfig(context.Background(), cfg, &fakeBootstrap{ip: netip.MustParseAddr("198.51.100.10")})
	if err != nil {
		t.Fatal(err)
	}
	err = got.TLSConfig.VerifyPeerCertificate([][]byte{[]byte("wrong")}, nil)
	if err == nil {
		t.Fatal("wrong certificate accepted")
	}
}

func TestServerDomainUsesBootstrapResolverOnly(t *testing.T) {
	bootstrap := &fakeBootstrap{ip: netip.MustParseAddr("198.51.100.10")}
	cfg := testHY2Config()
	cfg.Server = "relay.example:443"
	got, err := buildCoreConfig(context.Background(), cfg, bootstrap)
	if err != nil {
		t.Fatal(err)
	}
	if got.ServerAddr.String() != "198.51.100.10:443" {
		t.Fatal(got.ServerAddr)
	}
	if bootstrap.host != "relay.example" {
		t.Fatal(bootstrap.host)
	}
}

type fakeDNSExchanger struct {
	calls int
	reply *dns.Msg
}

func (fake *fakeDNSExchanger) Exchange(_ context.Context, request *dns.Msg) (*dns.Msg, error) {
	fake.calls++
	response := fake.reply.Copy()
	response.Id = request.Id
	return response, nil
}

func TestBootstrapResolverUsesDomesticDNS(t *testing.T) {
	request := new(dns.Msg)
	request.SetQuestion("relay.example.", dns.TypeA)
	response := new(dns.Msg)
	response.SetReply(request)
	response.Answer = append(response.Answer, &dns.A{
		Hdr: dns.RR_Header{Name: "relay.example.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
		A:   netip.MustParseAddr("198.51.100.10").AsSlice(),
	})
	exchanger := &fakeDNSExchanger{reply: response}
	resolver := NewBootstrapResolver(exchanger)
	ip, err := resolver.LookupIPv4(context.Background(), "relay.example")
	if err != nil || ip.String() != "198.51.100.10" || exchanger.calls != 1 {
		t.Fatalf("ip=%v err=%v calls=%d", ip, err, exchanger.calls)
	}
}

func testHY2Config() config.HY2Config {
	return config.HY2Config{
		Server:                  "198.51.100.10:443",
		Auth:                    "test-auth",
		SNI:                     "relay.example",
		MaxIdle:                 config.Duration(30 * time.Second),
		KeepAlive:               config.Duration(10 * time.Second),
		InitialStreamWindow:     1 << 20,
		MaxStreamWindow:         4 << 20,
		InitialConnectionWindow: 4 << 20,
		MaxConnectionWindow:     8 << 20,
		MaxConcurrentDials:      2,
	}
}
