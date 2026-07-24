package transport

import (
	"context"
	"encoding/binary"
	"github.com/miekg/dns"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestDNSOverStreamUsesRFC1035TCPFraming(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()
	d := &dnsDialer{conn: client}
	ex := NewDNSOverStream(d, "8.8.8.8:53", time.Second)
	go serveOneDNS(server, "www.google.com.", netip.MustParseAddr("142.250.1.1"))
	req := new(dns.Msg)
	req.SetQuestion("www.google.com.", dns.TypeA)
	resp, err := ex.Exchange(context.Background(), req)
	if err != nil || firstA(resp) != "142.250.1.1" {
		t.Fatalf("%v %v", resp, err)
	}
	if d.target != "8.8.8.8:53" {
		t.Fatal(d.target)
	}
}

type dnsDialer struct {
	conn   net.Conn
	target string
}

func (d *dnsDialer) Dial(_ context.Context, t string) (net.Conn, error) {
	d.target = t
	return d.conn, nil
}
func serveOneDNS(c net.Conn, name string, ip netip.Addr) {
	var n uint16
	_ = binary.Read(c, binary.BigEndian, &n)
	raw := make([]byte, n)
	_, _ = io.ReadFull(c, raw)
	q := new(dns.Msg)
	_ = q.Unpack(raw)
	r := new(dns.Msg)
	r.SetReply(q)
	r.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60}, A: ip.AsSlice()}}
	out, _ := r.Pack()
	_ = binary.Write(c, binary.BigEndian, uint16(len(out)))
	_, _ = c.Write(out)
}
func firstA(m *dns.Msg) string {
	for _, rr := range m.Answer {
		if a, ok := rr.(*dns.A); ok {
			return a.A.String()
		}
	}
	return ""
}
