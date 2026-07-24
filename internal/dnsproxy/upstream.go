package dnsproxy

import (
	"context"
	"fmt"

	"github.com/miekg/dns"
	"github.com/xmdragon/hy2route/internal/transport"
)

type NetworkExchanger struct {
	address string
	client  dns.Client
}

func NewNetworkExchanger(address string, mark ...uint32) *NetworkExchanger {
	var bypassMark uint32
	if len(mark) != 0 {
		bypassMark = mark[0]
	}
	return &NetworkExchanger{address: address, client: dns.Client{Net: "udp", UDPSize: 1232, Dialer: transport.NewMarkedDialer(bypassMark)}}
}

func (exchanger *NetworkExchanger) Exchange(ctx context.Context, request *dns.Msg) (*dns.Msg, error) {
	response, _, err := exchanger.client.ExchangeContext(ctx, request, exchanger.address)
	if err != nil {
		return nil, fmt.Errorf("DNS exchange: %w", err)
	}
	return response, nil
}
