package dnsproxy

import (
	"context"
	"net"
	"testing"

	"github.com/miekg/dns"
)

func TestNetworkExchangerQueriesUDPUpstream(t *testing.T) {
	packetConn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	upstream := &dns.Server{
		PacketConn: packetConn,
		Handler: dns.HandlerFunc(func(writer dns.ResponseWriter, request *dns.Msg) {
			response := aReply(request.Question[0].Name, "120.233.109.151")
			response.Id = request.Id
			_ = writer.WriteMsg(response)
		}),
	}
	done := make(chan error, 1)
	go func() { done <- upstream.ActivateAndServe() }()
	defer func() {
		_ = upstream.Shutdown()
		<-done
	}()

	exchanger := NewNetworkExchanger(packetConn.LocalAddr().String())
	response, err := exchanger.Exchange(context.Background(), aQuestion("wechat.com."))
	if err != nil {
		t.Fatal(err)
	}
	if got := allA(response); len(got) != 1 || got[0] != "120.233.109.151" {
		t.Fatalf("response A records = %v", got)
	}
}
