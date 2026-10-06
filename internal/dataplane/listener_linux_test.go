//go:build linux

package dataplane

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestTCPServerRunStopsWhenContextIsCanceled(t *testing.T) {
	port := reserveLoopbackTCPPort(t)
	ready := make(chan struct{})
	server := &TCPServer{
		ListenAddr: fmt.Sprintf("127.0.0.1:%d", port),
		Ready:      ready,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	select {
	case <-ready:
	case err := <-done:
		if isMissingTPROXYCapability(err) {
			t.Skipf("TPROXY socket capability unavailable: %v", err)
		}
		t.Fatalf("TCP server stopped before ready: %v", err)
	case <-time.After(time.Second):
		t.Fatal("TCP server did not become ready")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("TCP server did not stop")
	}
}

func reserveLoopbackTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}
