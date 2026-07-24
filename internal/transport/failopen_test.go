package transport

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xmdragon/hy2route/internal/failover"
)

type failDialer struct {
	conn net.Conn
	err  error
}

func (d *failDialer) Dial(context.Context, string) (net.Conn, error) { return d.conn, d.err }

type noEvents struct{}

func (noEvents) Emit(Event) {}

func TestFailOpenRetriesDirectOnProxyDialFailure(t *testing.T) {
	proxy := &failDialer{err: errors.New("hy2 down")}
	directConn, peer := net.Pipe()
	defer peer.Close()
	direct := &failDialer{conn: directConn}
	c := failover.New(failover.Config{Failures: 1, Successes: 1}, nil)
	d := NewFailOpen(proxy, direct, c, noEvents{})
	conn, err := d.Dial(context.Background(), "203.0.113.8:443")
	if err != nil || conn != directConn {
		t.Fatalf("dial=%v %v", conn, err)
	}
}

func TestFailOpenProbesOnceAfterCooldownWhileTrafficStaysDirect(t *testing.T) {
	now := time.Unix(100, 0)
	controller := failover.New(failover.Config{Failures: 1, Successes: 1, Cooldown: time.Second}, func() time.Time { return now })
	controller.RecordFailure()
	now = now.Add(time.Second)

	probeStarted := make(chan struct{}, 1)
	proxyPeer, proxyConn := net.Pipe()
	defer proxyPeer.Close()
	var proxyDials atomic.Int32
	proxy := dialFunc(func(context.Context, string) (net.Conn, error) {
		proxyDials.Add(1)
		probeStarted <- struct{}{}
		return proxyConn, nil
	})
	directConn, directPeer := net.Pipe()
	defer directPeer.Close()
	var directDials atomic.Int32
	direct := dialFunc(func(context.Context, string) (net.Conn, error) {
		directDials.Add(1)
		return directConn, nil
	})

	d := NewFailOpenWithProbe(proxy, direct, controller, noEvents{}, time.Millisecond)
	conn, err := d.Dial(context.Background(), "203.0.113.8:443")
	if err != nil || conn != directConn {
		t.Fatalf("dial=%v %v", conn, err)
	}
	select {
	case <-probeStarted:
	case <-time.After(time.Second):
		t.Fatal("background HY2 probe did not start")
	}
	deadline := time.Now().Add(time.Second)
	for controller.Mode() != failover.Proxy && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if controller.Mode() != failover.Proxy {
		t.Fatal("successful probe did not restore proxy mode")
	}
	if proxyDials.Load() != 1 || directDials.Load() != 1 {
		t.Fatalf("proxy=%d direct=%d", proxyDials.Load(), directDials.Load())
	}
}

type dialFunc func(context.Context, string) (net.Conn, error)

func (fn dialFunc) Dial(ctx context.Context, target string) (net.Conn, error) { return fn(ctx, target) }
