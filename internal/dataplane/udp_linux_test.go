//go:build linux

package dataplane

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/xmdragon/hy2route/internal/transport"
)

func TestUDPServerRunUnmapsTPROXYIPv4Destination(t *testing.T) {
	port := reserveLoopbackUDPPort(t)
	payload := []byte("stun-like-payload")
	proxy := &tproxyIntegrationProxyDialer{opened: make(chan struct{}, 1)}
	ready := make(chan struct{})
	server := &UDPServer{
		ListenAddr: fmt.Sprintf("127.0.0.1:%d", port),
		Ready:      ready,
		Direct:     transport.BlockPacketDialer{},
		Proxy:      proxy,
		Sessions:   newSessionTable(4, time.Millisecond, nil),
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
		t.Fatalf("UDP server stopped before ready: %v", err)
	case <-time.After(time.Second):
		t.Fatal("UDP server did not become ready")
	}

	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	target := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	if _, err := sender.WriteToUDP(payload, target); err != nil {
		t.Fatal(err)
	}

	select {
	case <-proxy.opened:
	case <-time.After(time.Second):
		t.Fatal("loopback TPROXY packet did not reach proxy selection")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("UDP server did not stop")
	}
}

func reserveLoopbackUDPPort(t *testing.T) int {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).Port
}

func isMissingTPROXYCapability(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "operation not permitted") || strings.Contains(message, "permission denied")
}

var errStopAfterProxySelection = errors.New("stop after proxy selection")

type tproxyIntegrationProxyDialer struct {
	opened chan struct{}
}

func (dialer *tproxyIntegrationProxyDialer) OpenPacket(context.Context) (transport.PacketSession, error) {
	select {
	case dialer.opened <- struct{}{}:
	default:
	}
	return nil, errStopAfterProxySelection
}
