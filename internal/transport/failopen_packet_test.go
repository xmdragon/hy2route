package transport

import (
	"context"
	"errors"
	"testing"

	"github.com/xmdragon/hy2route/internal/failover"
)

func TestFailOpenPacketUsesDirectWhenHY2PacketOpenFails(t *testing.T) {
	proxy := &packetDialerStub{err: errors.New("HY2 UDP unavailable")}
	direct := &packetDialerStub{session: &packetSessionStub{}}
	controller := failover.New(failover.Config{Failures: 1, Successes: 1}, nil)

	session, err := NewFailOpenPacket(proxy, direct, controller, noEvents{}).OpenPacket(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if session != direct.session || proxy.calls != 1 || direct.calls != 1 || controller.Mode() != failover.DirectRecovery {
		t.Fatalf("session=%T proxy=%d direct=%d mode=%v", session, proxy.calls, direct.calls, controller.Mode())
	}
}

func TestFailOpenPacketMovesFutureDatagramsDirectAfterHY2SendFailure(t *testing.T) {
	proxySession := &packetSessionStub{sendErr: errors.New("HY2 UDP write failed")}
	proxy := &packetDialerStub{session: proxySession}
	directSession := &packetSessionStub{}
	direct := &packetDialerStub{session: directSession}
	controller := failover.New(failover.Config{Failures: 1, Successes: 1}, nil)
	session, err := NewFailOpenPacket(proxy, direct, controller, noEvents{}).OpenPacket(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Send([]byte("first"), "203.0.113.8:443"); err == nil {
		t.Fatal("first failed proxy datagram unexpectedly succeeded")
	}
	if err := session.Send([]byte("second"), "203.0.113.8:443"); err != nil {
		t.Fatal(err)
	}
	if proxySession.sends != 1 || direct.calls != 1 || directSession.sends != 1 {
		t.Fatalf("proxy sends=%d direct opens=%d direct sends=%d", proxySession.sends, direct.calls, directSession.sends)
	}
}

type packetDialerStub struct {
	calls   int
	session PacketSession
	err     error
}

func (d *packetDialerStub) OpenPacket(context.Context) (PacketSession, error) {
	d.calls++
	return d.session, d.err
}

type packetSessionStub struct {
	sends   int
	sendErr error
}

func (s *packetSessionStub) Send([]byte, string) error { s.sends++; return s.sendErr }
func (*packetSessionStub) Receive() ([]byte, string, error) {
	return nil, "", errors.New("not implemented")
}
func (*packetSessionStub) Close() error { return nil }
