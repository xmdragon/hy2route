package dnsproxy

import (
	"context"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func TestServerAnswersUDPAndTCPAndStops(t *testing.T) {
	resolver, domestic, _, _ := resolverFixture(t)
	domestic.reply = aReply("wechat.com.", "120.233.109.151")
	server := NewServer("127.0.0.1:0", resolver)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	address := waitReady(t, server)
	select {
	case err := <-done:
		t.Fatalf("server stopped before first query: %v", err)
	default:
	}
	assertAQuery(t, "udp", address, "wechat.com.")
	assertAQuery(t, "tcp", address, "wechat.com.")
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func waitReady(t *testing.T, server *Server) string {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if address := server.Addr(); address != "" {
			return address
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("DNS server did not bind")
	return ""
}

func assertAQuery(t *testing.T, network, address, name string) {
	t.Helper()
	client := &dns.Client{Net: network, Timeout: time.Second}
	response, _, err := client.Exchange(aQuestion(name), address)
	if err != nil {
		t.Fatal(err)
	}
	if got := allA(response); len(got) != 1 || got[0] != "120.233.109.151" {
		t.Fatalf("response A records = %v", got)
	}
}
