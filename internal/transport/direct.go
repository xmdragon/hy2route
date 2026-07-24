package transport

import (
	"context"
	"net"
)

type directStreamDialer struct {
	dialer net.Dialer
}

func NewDirectStreamDialer() StreamDialer {
	return &directStreamDialer{}
}

func (d *directStreamDialer) Dial(ctx context.Context, target string) (net.Conn, error) {
	return d.dialer.DialContext(ctx, "tcp4", target)
}

type directPacketDialer struct{}

type directPacketSession struct {
	conn *net.UDPConn
}

func NewDirectPacketDialer() PacketDialer {
	return directPacketDialer{}
}

func (directPacketDialer) OpenPacket(ctx context.Context) (PacketSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, err
	}
	return &directPacketSession{conn: conn}, nil
}

func (s *directPacketSession) Send(payload []byte, target string) error {
	addr, err := net.ResolveUDPAddr("udp4", target)
	if err != nil {
		return err
	}
	_, err = s.conn.WriteToUDP(payload, addr)
	return err
}

func (s *directPacketSession) Receive() ([]byte, string, error) {
	payload := make([]byte, 64<<10)
	n, addr, err := s.conn.ReadFromUDP(payload)
	if err != nil {
		return nil, "", err
	}
	return payload[:n], addr.String(), nil
}

func (s *directPacketSession) Close() error { return s.conn.Close() }
