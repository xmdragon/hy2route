package transport

import (
	"context"
	"errors"
	"net"
)

type directStreamDialer struct {
	dialer *net.Dialer
}

func NewDirectStreamDialer(mark ...uint32) StreamDialer {
	return &directStreamDialer{dialer: NewMarkedDialer(firstMark(mark))}
}

func (d *directStreamDialer) Dial(ctx context.Context, target string) (net.Conn, error) {
	return d.dialer.DialContext(ctx, "tcp4", target)
}

type directPacketDialer struct{ mark uint32 }

type directPacketSession struct {
	conn *net.UDPConn
}

func NewDirectPacketDialer(mark ...uint32) PacketDialer {
	return directPacketDialer{mark: firstMark(mark)}
}

func (d directPacketDialer) OpenPacket(ctx context.Context) (PacketSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	packet, err := ListenMarkedPacket(ctx, "udp4", "0.0.0.0:0", d.mark)
	if err != nil {
		return nil, err
	}
	conn, ok := packet.(*net.UDPConn)
	if !ok {
		packet.Close()
		return nil, errors.New("marked packet listener is not UDP")
	}
	return &directPacketSession{conn: conn}, nil
}

func firstMark(marks []uint32) uint32 {
	if len(marks) == 0 {
		return 0
	}
	return marks[0]
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
