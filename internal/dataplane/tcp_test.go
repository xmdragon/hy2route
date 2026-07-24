package dataplane

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/xmdragon/hy2route/internal/config"
	"github.com/xmdragon/hy2route/internal/dataset"
	"github.com/xmdragon/hy2route/internal/policy"
	"github.com/xmdragon/hy2route/internal/sniff"
	"github.com/xmdragon/hy2route/internal/transport"
)

func TestTCPChoosesDomainOverLearnedIPAndReplaysPeekedBytes(t *testing.T) {
	client, inbound := net.Pipe()
	defer client.Close()
	direct := newRecordingDialer()
	proxy := newRecordingDialer()
	learned := policy.NewLearningTable(8)
	targetIP := netip.MustParseAddr("203.0.113.8")
	expires := time.Now().Add(time.Minute)
	if err := learned.Observe(policy.Observation{Domain: "world.example", Action: policy.Proxy, IPs: []netip.Addr{targetIP}, Expires: expires}); err != nil {
		t.Fatal(err)
	}
	if err := learned.Observe(policy.Observation{Domain: "cn.example", Action: policy.Direct, IPs: []netip.Addr{targetIP}, Expires: expires}); err != nil {
		t.Fatal(err)
	}
	server := newTCPServerForTest(t, testClassifier(t), learned, direct, proxy)
	done := make(chan error, 1)
	go func() {
		done <- server.handle(context.Background(), wrapAddrConn(inbound, "192.168.80.20:50000", "203.0.113.8:443"))
	}()
	hello := buildClientHello("wechat.com")
	go func() { _, _ = client.Write(hello) }()
	conn := direct.wait(t)
	if conn.target != "203.0.113.8:443" {
		t.Fatal(conn.target)
	}
	if got := readExactly(t, conn.peer, len(hello)); !bytes.Equal(got, hello) {
		t.Fatal("peeked bytes were not replayed")
	}
	if proxy.calls() != 0 {
		t.Fatal("proxy dialed")
	}
	_ = client.Close()
	_ = conn.peer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("TCP handler did not return")
	}
}

func TestRelayClosesWriteSideAfterEOF(t *testing.T) {
	client, inbound := tcpPair(t)
	defer client.Close()
	defer inbound.Close()
	outbound, target := tcpPair(t)
	defer outbound.Close()
	defer target.Close()
	done := make(chan error, 1)
	go func() { done <- relay(inbound, outbound) }()
	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	if err := client.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if got := string(readExactly(t, target, 7)); got != "request" {
		t.Fatal(got)
	}
	if _, err := target.Write([]byte("final")); err != nil {
		t.Fatal(err)
	}
	if err := target.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if got := string(readExactly(t, client, 5)); got != "final" {
		t.Fatal(got)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func testClassifier(t *testing.T) *policy.Classifier {
	t.Helper()
	classifier, err := policy.New(dataset.Data{Domains: []dataset.Domain{{Name: "wechat.com"}}}, []config.RuleConfig{{Action: "proxy", Type: "domain", Value: "proxy.example"}})
	if err != nil {
		t.Fatal(err)
	}
	return classifier
}

func newTCPServerForTest(t *testing.T, classifier *policy.Classifier, learned *policy.LearningTable, direct, proxy transport.StreamDialer) *TCPServer {
	t.Helper()
	return &TCPServer{Classifier: classifier, Learned: learned, Direct: direct, Proxy: proxy, Sniff: sniff.Limits{Bytes: 4096, Timeout: time.Second}, MaxActive: 4}
}

type recordingDialer struct {
	entries chan dialEntry
}

type dialEntry struct {
	target string
	peer   net.Conn
}

func newRecordingDialer() *recordingDialer { return &recordingDialer{entries: make(chan dialEntry, 2)} }

func (d *recordingDialer) Dial(_ context.Context, target string) (net.Conn, error) {
	conn, peer := net.Pipe()
	d.entries <- dialEntry{target: target, peer: peer}
	return conn, nil
}
func (d *recordingDialer) wait(t *testing.T) dialEntry {
	t.Helper()
	select {
	case entry := <-d.entries:
		return entry
	case <-time.After(time.Second):
		t.Fatal("dial not recorded")
		return dialEntry{}
	}
}
func (d *recordingDialer) calls() int { return len(d.entries) }

type addrConn struct {
	net.Conn
	remote net.Addr
	local  net.Addr
}

func wrapAddrConn(conn net.Conn, remote, local string) net.Conn {
	return &addrConn{Conn: conn, remote: tcpAddr(remote), local: tcpAddr(local)}
}
func (c *addrConn) RemoteAddr() net.Addr { return c.remote }
func (c *addrConn) LocalAddr() net.Addr  { return c.local }

type tcpAddr string

func (a tcpAddr) Network() string { return "tcp" }
func (a tcpAddr) String() string  { return string(a) }

func readExactly(t *testing.T, reader io.Reader, length int) []byte {
	t.Helper()
	buf := make([]byte, length)
	if _, err := io.ReadFull(reader, buf); err != nil {
		t.Fatal(err)
	}
	return buf
}

func tcpPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	client, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server := <-accepted
	_ = listener.Close()
	return client, server
}

func buildClientHello(host string) []byte {
	name := []byte(host)
	serverName := append([]byte{0, byte(len(name) >> 8), byte(len(name))}, name...)
	serverName = append([]byte{byte(len(serverName) >> 8), byte(len(serverName))}, serverName...)
	extension := append([]byte{0, 0, byte(len(serverName) >> 8), byte(len(serverName))}, serverName...)
	body := append([]byte{3, 3}, make([]byte, 32)...)
	body = append(body, 0, 0, 2, 0, 47, 1, 0, byte(len(extension)>>8), byte(len(extension)))
	body = append(body, extension...)
	handshake := append([]byte{1, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}, body...)
	return append([]byte{22, 3, 3, byte(len(handshake) >> 8), byte(len(handshake))}, handshake...)
}
