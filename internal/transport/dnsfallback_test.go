package transport

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/miekg/dns"
)

func TestDNSFallbackUsesDomesticWhenTrustedChainFails(t *testing.T) {
	primary := &testDNSExchanger{err: errors.New("trusted chain unavailable")}
	fallback := &testDNSExchanger{reply: dnsReply("www.example.", "120.233.109.151")}
	exchanger := NewDNSFallback(primary, fallback)

	response, err := exchanger.Exchange(context.Background(), aDNSQuestion("www.example."))
	if err != nil {
		t.Fatal(err)
	}
	if firstA(response) != "120.233.109.151" || primary.calls != 1 || fallback.calls != 1 {
		t.Fatalf("response=%v primary=%d fallback=%d", response, primary.calls, fallback.calls)
	}
}

func TestDNSFallbackDoesNotMaskTrustedSuccess(t *testing.T) {
	primary := &testDNSExchanger{reply: dnsReply("www.example.", "203.0.113.8")}
	fallback := &testDNSExchanger{reply: dnsReply("www.example.", "120.233.109.151")}
	exchanger := NewDNSFallback(primary, fallback)

	response, err := exchanger.Exchange(context.Background(), aDNSQuestion("www.example."))
	if err != nil {
		t.Fatal(err)
	}
	if firstA(response) != "203.0.113.8" || primary.calls != 1 || fallback.calls != 0 {
		t.Fatalf("response=%v primary=%d fallback=%d", response, primary.calls, fallback.calls)
	}
}

type testDNSExchanger struct {
	calls int
	reply *dns.Msg
	err   error
}

func (x *testDNSExchanger) Exchange(context.Context, *dns.Msg) (*dns.Msg, error) {
	x.calls++
	if x.reply == nil {
		return nil, x.err
	}
	return x.reply.Copy(), x.err
}

func aDNSQuestion(name string) *dns.Msg {
	q := new(dns.Msg)
	q.SetQuestion(name, dns.TypeA)
	return q
}

func dnsReply(name, address string) *dns.Msg {
	q := aDNSQuestion(name)
	r := new(dns.Msg)
	r.SetReply(q)
	r.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: netip.MustParseAddr(address).AsSlice()}}
	return r
}
