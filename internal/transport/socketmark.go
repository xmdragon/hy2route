package transport

import (
	"context"
	"net"
)

func NewMarkedDialer(mark uint32) *net.Dialer {
	dialer := &net.Dialer{}
	if mark != 0 {
		dialer.Control = socketMarkControl(mark)
	}
	return dialer
}

func ListenMarkedPacket(ctx context.Context, network, address string, mark uint32) (net.PacketConn, error) {
	listener := net.ListenConfig{}
	if mark != 0 {
		listener.Control = socketMarkControl(mark)
	}
	return listener.ListenPacket(ctx, network, address)
}
