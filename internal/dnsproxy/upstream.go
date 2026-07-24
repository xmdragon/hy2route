package dnsproxy

import (
	"context"
	"fmt"

	"github.com/miekg/dns"
)

type NetworkExchanger struct {
	address string
	client  dns.Client
}

func NewNetworkExchanger(address string) *NetworkExchanger {
	return &NetworkExchanger{address: address, client: dns.Client{Net: "udp", UDPSize: 1232}}
}

func (exchanger *NetworkExchanger) Exchange(ctx context.Context, request *dns.Msg) (*dns.Msg, error) {
	response, _, err := exchanger.client.ExchangeContext(ctx, request, exchanger.address)
	if err != nil {
		return nil, fmt.Errorf("DNS exchange: %w", err)
	}
	return response, nil
}
