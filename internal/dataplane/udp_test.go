package dataplane

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/xmdragon/hy2route/internal/policy"
	"github.com/xmdragon/hy2route/internal/transport"
)

func TestUDPConflictUsesHY2AndNotDirect(t *testing.T) {
	learned := policy.NewLearningTable(8)
	target := netip.MustParseAddr("203.0.113.8")
	expires := time.Now().Add(time.Minute)
	if err := learned.Observe(policy.Observation{Domain: "cn.example", Action: policy.Direct, IPs: []netip.Addr{target}, Expires: expires}); err != nil {
		t.Fatal(err)
	}
	if err := learned.Observe(policy.Observation{Domain: "world.example", Action: policy.Proxy, IPs: []netip.Addr{target}, Expires: expires}); err != nil {
		t.Fatal(err)
	}
	hy2 := &udpPacketDialer{session: &udpPacketSession{}}
	direct := &udpPacketDialer{session: &udpPacketSession{}}
	server := newUDPServerForTest(learned, direct, hy2)
	if err := server.handleFirst(context.Background(), netip.MustParseAddrPort("192.168.80.20:40000"), netip.MustParseAddrPort("203.0.113.8:443"), []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if hy2.calls != 1 || direct.calls != 0 {
		t.Fatalf("hy2=%d direct=%d", hy2.calls, direct.calls)
	}
}

type udpPacketDialer struct {
	calls   int
	session transport.PacketSession
}

func (d *udpPacketDialer) OpenPacket(context.Context) (transport.PacketSession, error) {
	d.calls++
	return d.session, nil
}

type udpPacketSession struct{}

func (*udpPacketSession) Send([]byte, string) error { return nil }
func (*udpPacketSession) Receive() ([]byte, string, error) {
	return nil, "", errors.New("not implemented")
}
func (*udpPacketSession) Close() error { return nil }

var _ transport.PacketDialer = (*udpPacketDialer)(nil)
