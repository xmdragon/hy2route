package transport

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

type relayTestDialer struct {
	calls  int
	target string
	fail   bool
}

func (d *relayTestDialer) Dial(ctx context.Context, target string) (net.Conn, error) {
	d.calls++
	d.target = target
	if d.fail {
		return nil, errors.New("relay down")
	}
	a, b := net.Pipe()
	b.Close()
	return a, nil
}
func TestRelayFallbackPreservesLandingAndRecovers(t *testing.T) {
	primary, backup := &relayTestDialer{fail: true}, &relayTestDialer{}
	d := NewRelayFallback(primary, backup, "reality", "hy2", time.Hour)
	for range 2 {
		conn, err := d.Dial(context.Background(), "landing.example:443")
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}
	if primary.calls != 1 || backup.calls != 2 || backup.target != "landing.example:443" || d.Active() != "hy2" {
		t.Fatal("fallback did not preserve target or cooldown")
	}
	primary.fail = false
	d.mu.Lock()
	d.retryAt = time.Time{}
	d.mu.Unlock()
	conn, err := d.Dial(context.Background(), "landing.example:443")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if d.Active() != "reality" || backup.calls != 2 {
		t.Fatal("primary did not recover")
	}
}
func TestRelayFallbackDoesNotRetryCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	primary, backup := &relayTestDialer{fail: true}, &relayTestDialer{}
	d := NewRelayFallback(primary, backup, "reality", "hy2", time.Minute)
	_, err := d.Dial(ctx, "landing.example:443")
	if !errors.Is(err, context.Canceled) || backup.calls != 0 {
		t.Fatal("cancellation retried")
	}
}

type relayTestPacketDialer struct {
	calls   int
	fail    bool
	session *relayTestPacket
}

func (d *relayTestPacketDialer) OpenPacket(context.Context) (PacketSession, error) {
	d.calls++
	if d.fail {
		return nil, errors.New("UDP relay down")
	}
	return d.session, nil
}

type relayTestPacket struct {
	fail   bool
	target string
	sent   []byte
	closed chan struct{}
}

func (s *relayTestPacket) Send(b []byte, target string) error {
	if s.fail {
		return errors.New("send failed")
	}
	s.sent = append([]byte(nil), b...)
	s.target = target
	return nil
}
func (s *relayTestPacket) Receive() ([]byte, string, error) {
	<-s.closed
	return nil, "", net.ErrClosed
}
func (s *relayTestPacket) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return nil
}
func TestPacketRelayFallbackSkipsLandingAndRetriesFailedDatagram(t *testing.T) {
	p := &relayTestPacket{fail: true, closed: make(chan struct{})}
	b := &relayTestPacket{closed: make(chan struct{})}
	primary, backup := &relayTestPacketDialer{session: p}, &relayTestPacketDialer{session: b}
	d := NewPacketRelayFallback(primary, backup, time.Hour)
	session, err := d.OpenPacket(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err = session.Send([]byte("dns"), "1.1.1.1:53"); err != nil {
		t.Fatal(err)
	}
	if b.target != "1.1.1.1:53" || string(b.sent) != "dns" || d.Active() != "reality" {
		t.Fatal("failed UDP datagram was not retried through relay")
	}
	if _, err = d.OpenPacket(context.Background()); err != nil {
		t.Fatal(err)
	}
	if primary.calls != 1 || backup.calls != 2 {
		t.Fatal("UDP cooldown ignored")
	}
}
func TestPacketRelayFallbackBothRelaysDownReturnsError(t *testing.T) {
	d := NewPacketRelayFallback(&relayTestPacketDialer{fail: true}, &relayTestPacketDialer{fail: true}, time.Minute)
	if _, err := d.OpenPacket(context.Background()); err == nil {
		t.Fatal("failed relays silently bypassed")
	}
}

type waitingRelay struct{}

func (waitingRelay) Dial(ctx context.Context, _ string) (net.Conn, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestRelayFallbackReservesRequestBudgetForBackup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	backup := &relayTestDialer{}
	d := NewRelayFallback(waitingRelay{}, backup, "reality", "hy2", time.Minute)
	conn, err := d.Dial(ctx, "landing.example:443")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if backup.calls != 1 || ctx.Err() != nil {
		t.Fatal("primary exhausted fallback budget")
	}
}

func TestClosingPacketSessionDoesNotFailPrimary(t *testing.T) {
	primary := &relayTestPacketDialer{session: &relayTestPacket{closed: make(chan struct{})}}
	d := NewPacketRelayFallback(primary, &relayTestPacketDialer{fail: true}, time.Minute)
	session, err := d.OpenPacket(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	session.Close()
	session.Receive()
	d.mu.Lock()
	cooldown := !d.retryAt.IsZero()
	d.mu.Unlock()
	if cooldown {
		t.Fatal("normal session close triggered relay failure")
	}
}
