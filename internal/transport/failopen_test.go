package transport

import (
	"context"
	"errors"
	"net"
	"testing"

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
