package dataplane

import (
	"net"
	"testing"
)

func TestIPv4MappedOriginalUDPDestinationIsAccepted(t *testing.T) {
	// go-tproxy builds IP_RECVORIGDSTADDR with net.IPv4, a 16-byte mapped
	// address even on a udp4 socket. It must reach the proxy session handler.
	endpoint, ok := ipv4UDPEndpoint(&net.UDPAddr{IP: net.IPv4(8, 8, 8, 8), Port: 53})
	if !ok || endpoint.String() != "8.8.8.8:53" {
		t.Fatalf("IPv4 original destination discarded: %v %v", endpoint, ok)
	}
}
func TestNativeIPv6UDPDestinationIsRejected(t *testing.T) {
	if _, ok := ipv4UDPEndpoint(&net.UDPAddr{IP: net.ParseIP("2001:db8::1"), Port: 53}); ok {
		t.Fatal("IPv6 entered IPv4 dataplane")
	}
}
