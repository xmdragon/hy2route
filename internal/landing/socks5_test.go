package landing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSOCKS5UsernamePasswordAndIPv4Connect(t *testing.T) {
	upstream, peer := net.Pipe()
	defer peer.Close()
	base := &fakeDialer{conn: upstream}
	dialer := newSOCKS5(base, "landing.example:1080", "alice", "secret")
	errs := make(chan error, 1)
	go func() {
		if got := readN(peer, 3); !bytes.Equal(got, []byte{5, 1, 2}) {
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

type socksReplyConn struct {
	net.Conn
	reply   *bytes.Reader
	readErr error
	closed  bool
	writes  bytes.Buffer
}

func (c *socksReplyConn) Read(b []byte) (int, error) {
	if c.reply.Len() == 0 && c.readErr != nil {
		return 0, c.readErr
	}
	return c.reply.Read(b)
}
func (c *socksReplyConn) Write(b []byte) (int, error) { return c.writes.Write(b) }
func (c *socksReplyConn) SetDeadline(time.Time) error { return nil }
func (c *socksReplyConn) Close() error                { c.closed = true; return nil }

func TestSOCKS5DiagnosticsDistinguishTransportAndProtocolFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		reply   []byte
		readErr error
		want    string
		cause   error
	}{
		{"greeting EOF", nil, nil, "SOCKS5 greeting read: EOF", io.EOF},
		{"greeting partial", []byte{5}, nil, "SOCKS5 greeting read: unexpected EOF", io.ErrUnexpectedEOF},
		{"greeting timeout", nil, os.ErrDeadlineExceeded, "SOCKS5 greeting read:", os.ErrDeadlineExceeded},
		{"invalid version", []byte{4, 2}, nil, "invalid version 0x04", nil},
		{"method refused", []byte{5, 255}, nil, "SOCKS5 method rejected", nil},
		{"unoffered method", []byte{5, 0}, nil, "unoffered method 0x00", nil},
		{"auth EOF", []byte{5, 2}, nil, "SOCKS5 authentication read: EOF", io.EOF},
		{"auth timeout", []byte{5, 2}, os.ErrDeadlineExceeded, "SOCKS5 authentication read:", os.ErrDeadlineExceeded},
		{"auth invalid version", []byte{5, 2, 5, 0}, nil, "authentication: invalid version", nil},
		{"auth rejected", []byte{5, 2, 1, 1}, nil, "SOCKS5 authentication rejected (status 0x01)", nil},
		{"connect EOF", []byte{5, 2, 1, 0}, nil, "SOCKS5 connect read: EOF", io.EOF},
		{"connect timeout", []byte{5, 2, 1, 0}, os.ErrDeadlineExceeded, "SOCKS5 connect read:", os.ErrDeadlineExceeded},
		{"connect rejected", []byte{5, 2, 1, 0, 5, 5, 0, 1}, nil, "SOCKS5 connect rejected (status 0x05)", nil},
		{"connect invalid header", []byte{5, 2, 1, 0, 4, 0, 0, 1}, nil, "invalid reply header", nil},
		{"connect truncated address", []byte{5, 2, 1, 0, 5, 0, 0, 1, 0}, nil, "SOCKS5 connect address read: unexpected EOF", io.ErrUnexpectedEOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			peer := &socksReplyConn{reply: bytes.NewReader(tc.reply), readErr: tc.readErr}
			dialer := newSOCKS5(&fakeDialer{conn: peer}, "landing:443", "private-user", "private-password")
			conn, err := dialer.Dial(context.Background(), "203.0.113.8:443")
			if conn != nil || err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("dial = %v, %v", conn, err)
			}
			if tc.cause != nil && !errors.Is(err, tc.cause) {
				t.Fatalf("lost error cause: %v", err)
			}
			if !peer.closed || strings.Contains(err.Error(), "private-") {
				t.Fatalf("connection or credential leak: %v", err)
			}
		})
	}
}

func TestSOCKS5PreservesUpstreamFailure(t *testing.T) {
	cause := errors.New("tls: certificate has expired")
	dialer := newSOCKS5(&fakeDialer{err: cause}, "landing:443", "", "")
	_, err := dialer.Dial(context.Background(), "example.com:443")
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "SOCKS5 upstream dial:") {
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
