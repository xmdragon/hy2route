package transport

import (
	"context"
	"encoding/binary"
	"errors"
	"github.com/miekg/dns"
	"io"
	"time"
)

type dnsOverStream struct {
	dialer   StreamDialer
	endpoint string
	timeout  time.Duration
}

func NewDNSOverStream(d StreamDialer, e string, t time.Duration) *dnsOverStream {
	return &dnsOverStream{d, e, t}
}
func (x *dnsOverStream) Exchange(ctx context.Context, q *dns.Msg) (*dns.Msg, error) {
	raw, err := q.Pack()
	if err != nil || len(raw) > 4096 {
		return nil, errors.New("DNS query too large")
	}
	c, err := x.dialer.Dial(ctx, x.endpoint)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(x.timeout))
	defer c.SetDeadline(time.Time{})
	if err := binary.Write(c, binary.BigEndian, uint16(len(raw))); err != nil {
		return nil, err
	}
	if _, err = c.Write(raw); err != nil {
		return nil, err
	}
	var n uint16
	if err := binary.Read(c, binary.BigEndian, &n); err != nil {
		return nil, err
	}
	if n > 4096 {
		return nil, errors.New("DNS reply too large")
	}
	out := make([]byte, n)
	if _, err := io.ReadFull(c, out); err != nil {
		return nil, err
	}
	r := new(dns.Msg)
	if err := r.Unpack(out); err != nil {
		return nil, err
	}
	r.Id = q.Id
	return r, nil
}
