package transport

import (
	"context"
	"net"
	"testing"
)

type interfaceProbe struct{}

func (interfaceProbe) Dial(context.Context, string) (net.Conn, error)    { return nil, nil }
func (interfaceProbe) OpenPacket(context.Context) (PacketSession, error) { return nil, nil }
func (interfaceProbe) Send([]byte, string) error                         { return nil }
func (interfaceProbe) Receive() ([]byte, string, error)                  { return nil, "", nil }
func (interfaceProbe) Close() error                                      { return nil }
func (interfaceProbe) Emit(Event)                                        {}

func TestTransportInterfacesAcceptMinimalImplementations(t *testing.T) {
	var _ StreamDialer = interfaceProbe{}
	var _ PacketDialer = interfaceProbe{}
	var _ PacketSession = interfaceProbe{}
	var _ EventSink = interfaceProbe{}
}
