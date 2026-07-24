package landing

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"testing"
)

func TestSOCKS5UsernamePasswordAndIPv4Connect(t *testing.T) {
	upstream, peer := net.Pipe()
	defer peer.Close()
	base := &fakeDialer{conn: upstream}
	dialer := newSOCKS5(base, "landing.example:1080", "alice", "secret")
	errs := make(chan error, 1)
	go func() {
		if got := readN(peer, 4); !bytes.Equal(got, []byte{5, 2, 0, 2}) {
			errs <- fmt.Errorf("greeting %v", got)
			return
		}
		_, _ = peer.Write([]byte{5, 2})
		wantAuth := append([]byte{1, 5}, []byte("alice")...)
		wantAuth = append(wantAuth, 6)
		wantAuth = append(wantAuth, []byte("secret")...)
		if got := readN(peer, len(wantAuth)); !bytes.Equal(got, wantAuth) {
			errs <- fmt.Errorf("auth %v", got)
			return
		}
		_, _ = peer.Write([]byte{1, 0})
		wantConnect := []byte{5, 1, 0, 1, 203, 0, 113, 8, 1, 187}
		if got := readN(peer, len(wantConnect)); !bytes.Equal(got, wantConnect) {
			errs <- fmt.Errorf("connect %v", got)
			return
		}
		_, _ = peer.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
		errs <- nil
	}()
	conn, err := dialer.Dial(context.Background(), "203.0.113.8:443")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
}

type fakeDialer struct {
	conn net.Conn
	err  error
}

func (dialer *fakeDialer) Dial(context.Context, string) (net.Conn, error) {
	return dialer.conn, dialer.err
}
func readN(reader io.Reader, count int) []byte {
	data := make([]byte, count)
	_, _ = io.ReadFull(reader, data)
	return data
}
