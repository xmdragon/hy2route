package transport

import (
	"context"
	"errors"

	"github.com/miekg/dns"
)

type DNSExchanger interface {
	Exchange(context.Context, *dns.Msg) (*dns.Msg, error)
}

type dnsFallback struct {
	primary  DNSExchanger
	fallback DNSExchanger
}

func NewDNSFallback(primary, fallback DNSExchanger) DNSExchanger {
	return &dnsFallback{primary: primary, fallback: fallback}
}

func (x *dnsFallback) Exchange(ctx context.Context, request *dns.Msg) (*dns.Msg, error) {
	if x.primary == nil || x.fallback == nil {
		return nil, errors.New("DNS fallback exchanger is not configured")
	}
	response, err := x.primary.Exchange(ctx, request)
	if err == nil && response != nil {
		return response, nil
	}
	return x.fallback.Exchange(ctx, request)
}
