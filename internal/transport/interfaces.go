package transport

import (
	"context"
	"net"
)

type StreamDialer interface {
	Dial(context.Context, string) (net.Conn, error)
}

type PacketDialer interface {
	OpenPacket(context.Context) (PacketSession, error)
}

type PacketSession interface {
	Send([]byte, string) error
	Receive() ([]byte, string, error)
	Close() error
}

type Event struct {
	Stage  string
	Reason string
}

type EventSink interface {
	Emit(Event)
}
