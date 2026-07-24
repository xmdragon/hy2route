package landing

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"testing"
)

func TestHTTPConnectOverHY2(t *testing.T) {
	upstream, peer := net.Pipe()
	defer peer.Close()
	base := &fakeDialer{conn: upstream}
	dialer := newHTTP(base, "landing.example:8080", "alice", "secret")
	errs := make(chan error, 1)
	go func() {
		reader := bufio.NewReader(peer)
		line, err := reader.ReadString('\n')
		if err != nil || line != "CONNECT 203.0.113.8:443 HTTP/1.1\r\n" {
			errs <- errOr(t, err, line)
			return
		}
		headers, err := textproto.NewReader(reader).ReadMIMEHeader()
		if err != nil || headers.Get("Host") != "203.0.113.8:443" || headers.Get("Proxy-Authorization") != "Basic YWxpY2U6c2VjcmV0" {
			errs <- errOr(t, err, headers.Get("Proxy-Authorization"))
			return
		}
		_, err = io.WriteString(peer, "HTTP/1.1 200 Connection established\r\n\r\nEARLY")
		errs <- err
	}()
	conn, err := dialer.Dial(context.Background(), "203.0.113.8:443")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	buffer := make([]byte, 5)
	if _, err := io.ReadFull(conn, buffer); err != nil || string(buffer) != "EARLY" {
		t.Fatalf("early data = %q, %v", buffer, err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
}

func errOr(t *testing.T, err error, value any) error {
	t.Helper()
	if err != nil {
		return err
	}
	return fmt.Errorf("unexpected value: %v", value)
}
