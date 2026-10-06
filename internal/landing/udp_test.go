package landing

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/xmdragon/hy2route/internal/config"
	"github.com/xmdragon/hy2route/internal/transport"
)

type packetResult struct {
	payload []byte
	source  string
}
type testPacket struct {
	mu      sync.Mutex
	sent    []byte
	target  string
	replies chan packetResult
	closed  chan struct{}
	once    sync.Once
}

func (p *testPacket) Send(b []byte, target string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append([]byte(nil), b...)
	p.target = target
	return nil
}
func (p *testPacket) Receive() ([]byte, string, error) {
	select {
	case r := <-p.replies:
		return r.payload, r.source, nil
	case <-p.closed:
		return nil, "", net.ErrClosed
	}
}
func (p *testPacket) Close() error { p.once.Do(func() { close(p.closed) }); return nil }

type testPacketDialer struct {
	packet *testPacket
	calls  int
}

func (d *testPacketDialer) OpenPacket(context.Context) (transport.PacketSession, error) {
	d.calls++
	return d.packet, nil
}

func udpFixture(t *testing.T, replyCode byte) (transport.PacketDialer, *testPacketDialer, chan struct{}) {
	t.Helper()
	upstream, peer := net.Pipe()
	done := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			close(done)
		}
		peer.Close()
		upstream.Close()
	})
	packet := &testPacket{replies: make(chan packetResult, 4), closed: make(chan struct{})}
	pd := &testPacketDialer{packet: packet}
	go func() {
		defer peer.Close()
		var g [3]byte
		if _, err := io.ReadFull(peer, g[:]); err != nil {
			return
		}
		if !bytes.Equal(g[:], []byte{5, 1, 0}) {
			t.Error("unexpected greeting")
			return
		}
		peer.Write([]byte{5, 0})
		var request [10]byte
		if _, err := io.ReadFull(peer, request[:]); err != nil {
			return
		}
		if !bytes.Equal(request[:], []byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}) {
			t.Error("did not request UDP ASSOCIATE")
			return
		}
		peer.Write([]byte{5, replyCode, 0, 1, 0, 0, 0, 0, 0xd4, 0x31})
		<-done
	}()
	d, err := NewPacket(config.LandingConfig{Type: "socks5", Server: "192.0.2.5:1080", MaxUDPPayload: 2038}, &fakeDialer{conn: upstream}, pd)
	if err != nil {
		t.Fatal(err)
	}
	return d, pd, done
}
func TestUDPLandingWrapsDatagramsAndKeepsDestination(t *testing.T) {
	d, pd, _ := udpFixture(t, 0)
	session, err := d.OpenPacket(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err = session.Send([]byte("dns"), "203.0.113.8:53"); err != nil {
		t.Fatal(err)
	}
	expected := []byte{0, 0, 0, 1, 203, 0, 113, 8, 0, 53, 'd', 'n', 's'}
	if !bytes.Equal(pd.packet.sent, expected) || pd.packet.target != "192.0.2.5:54321" {
		t.Fatalf("landing envelope or relay wrong: %v %s", pd.packet.sent, pd.packet.target)
	}
	if err = session.Send([]byte("other"), "203.0.113.9:53"); err == nil {
		t.Fatal("reused pinned association for another destination")
	}
	if err = session.Send(make([]byte, 2039), "203.0.113.8:53"); err == nil {
		t.Fatal("oversize datagram allowed to truncate")
	}
	if !bytes.Equal(pd.packet.sent, expected) {
		t.Fatal("rejected datagram was transmitted")
	}
	// Invalid fragments and a mismatched source must never be forwarded.
	pd.packet.replies <- packetResult{expected, "192.0.2.9:54321"}
	fragment := append([]byte(nil), expected...)
	fragment[2] = 1
	pd.packet.replies <- packetResult{fragment, "192.0.2.5:54321"}
	pd.packet.replies <- packetResult{expected, "192.0.2.5:54321"}
	b, target, err := session.Receive()
	if err != nil || string(b) != "dns" || target != "203.0.113.8:53" {
		t.Fatalf("decoded reply = %q %s %v", b, target, err)
	}
}
func TestUDPLandingRejectionDoesNotSkipLanding(t *testing.T) {
	d, pd, _ := udpFixture(t, 9)
	_, err := d.OpenPacket(context.Background())
	if err == nil || pd.calls != 0 {
		t.Fatalf("rejected association fell through: %v calls=%d", err, pd.calls)
	}
}
func TestUDPLandingControlCloseClosesPacketSession(t *testing.T) {
	d, pd, _ := udpFixture(t, 0)
	session, err := d.OpenPacket(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err = session.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
	select {
	case <-pd.packet.closed:
	case <-time.After(time.Second):
		t.Fatal("packet session leaked")
	}
}
func TestUDPAddressEncodingSupportsIPv4IPv6AndDomains(t *testing.T) {
	for _, target := range []string{"203.0.113.8:53", "[2001:db8::1]:53", "dns.example:53"} {
		b, err := encodeUDP([]byte("payload"), target)
		if err != nil {
			t.Fatal(err)
		}
		reader := bytes.NewReader(b[4:])
		address, err := readAddress(reader, b[3])
		if err != nil || address != target || string(b[len(b)-reader.Len():]) != "payload" {
			t.Fatalf("roundtrip %s: %s %v", target, address, err)
		}
	}
}

func TestUDPLandingRemoteControlEOFInvalidatesAssociation(t *testing.T) {
	d, pd, done := udpFixture(t, 0)
	session, err := d.OpenPacket(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	close(done)
	select {
	case <-pd.packet.closed:
	case <-time.After(time.Second):
		t.Fatal("UDP survived control-stream EOF")
	}
	if err = session.Send([]byte("data"), "203.0.113.8:53"); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("send after control EOF: %v", err)
	}
}
