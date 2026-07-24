package sniff

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestTLSClientHelloSNIAcrossFragments(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	raw := buildClientHello("cdn.wechat.com")
	go func() {
		for _, size := range []int{1, 2, 5, 13} {
			if len(raw) == 0 {
				return
			}
			if size > len(raw) {
				size = len(raw)
			}
			_, _ = server.Write(raw[:size])
			raw = raw[size:]
		}
		_, _ = server.Write(raw)
	}()
	result, _, err := Peek(context.Background(), client, Limits{Bytes: 4096, Timeout: time.Second})
	if err != nil || result.Domain != "cdn.wechat.com" || result.Protocol != "tls" || !result.Complete {
		t.Fatalf("result=%#v error=%v", result, err)
	}
}

func TestHTTPHostIsNormalized(t *testing.T) {
	result := Parse([]byte("GET / HTTP/1.1\r\nHost: WWW.Example.COM:443\r\n\r\n"))
	if result.Domain != "www.example.com" || result.Protocol != "http" || !result.Complete {
		t.Fatalf("result=%#v", result)
	}
}

func TestOversizedInputStopsAtLimitAndPreservesBytes(t *testing.T) {
	raw := bytes.Repeat([]byte{'x'}, 4097)
	conn := &countingConn{Reader: bytes.NewReader(raw)}
	result, reader, err := Peek(context.Background(), conn, Limits{Bytes: 4096, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if result.Domain != "" || conn.BytesRead() != 4096 {
		t.Fatalf("result=%#v bytes=%d", result, conn.BytesRead())
	}
	replayed, err := io.ReadAll(io.LimitReader(reader, 4096))
	if err != nil || !bytes.Equal(replayed, raw[:4096]) {
		t.Fatalf("replayed=%d error=%v", len(replayed), err)
	}
}

func TestParsersRejectMalformedInput(t *testing.T) {
	for _, raw := range [][]byte{
		{22, 3, 3, 0, 4, 1, 0, 0, 255},
		[]byte("GET / HTTP/1.1\r\nHost: bad host\r\n\r\n"),
		[]byte("GET / HTTP/1.1\r\nHost: one.example\r\nHost: two.example\r\n\r\n"),
	} {
		if result := Parse(raw); result.Domain != "" {
			t.Fatalf("malformed result=%#v", result)
		}
	}
}

type countingConn struct {
	*bytes.Reader
	reads atomic.Int64
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Reader.Read(p)
	c.reads.Add(int64(n))
	return n, err
}
func (c *countingConn) Write([]byte) (int, error)        { return 0, io.ErrClosedPipe }
func (c *countingConn) Close() error                     { return nil }
func (c *countingConn) LocalAddr() net.Addr              { return testAddr("local") }
func (c *countingConn) RemoteAddr() net.Addr             { return testAddr("remote") }
func (c *countingConn) SetDeadline(time.Time) error      { return nil }
func (c *countingConn) SetReadDeadline(time.Time) error  { return nil }
func (c *countingConn) SetWriteDeadline(time.Time) error { return nil }
func (c *countingConn) BytesRead() int                   { return int(c.reads.Load()) }

type testAddr string

func (a testAddr) Network() string { return "test" }
func (a testAddr) String() string  { return string(a) }

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
