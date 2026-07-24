package transport

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestDirectStreamDialerUsesIPv4TCP(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
			close(accepted)
		}
	}()

	conn, err := NewDirectStreamDialer().Dial(context.Background(), listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	<-accepted
}

func TestDirectPacketSessionSendsAndReceivesIPv4UDP(t *testing.T) {
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	go func() {
		buf := make([]byte, 64)
		n, peer, err := server.ReadFromUDP(buf)
		if err == nil && string(buf[:n]) == "ping" {
			_, _ = server.WriteToUDP([]byte("pong"), peer)
		}
	}()

	session, err := NewDirectPacketDialer().OpenPacket(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.Send([]byte("ping"), server.LocalAddr().String()); err != nil {
		t.Fatal(err)
	}
	received := make(chan struct {
		payload []byte
		target  string
		err     error
	}, 1)
	go func() {
		payload, target, err := session.Receive()
		received <- struct {
			payload []byte
			target  string
			err     error
		}{payload, target, err}
	}()
	select {
	case result := <-received:
		if result.err != nil || string(result.payload) != "pong" || result.target != server.LocalAddr().String() {
			t.Fatalf("payload=%q target=%q error=%v", result.payload, result.target, result.err)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive UDP response")
	}
}
