package landing

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/xmdragon/hy2route/internal/config"
	"github.com/xmdragon/hy2route/internal/transport"
)

type socksPacketDialer struct {
	control    *socks5Dialer
	packets    transport.PacketDialer
	maxPayload int
}

// NewPacket keeps UDP on the configured landing. Rejected associations are
// errors; they never silently become direct relay egress.
func NewPacket(cfg config.LandingConfig, stream transport.StreamDialer, packets transport.PacketDialer) (transport.PacketDialer, error) {
	if cfg.Type != "socks5" {
		return nil, errors.New("UDP landing requires SOCKS5")
	}
	return &socksPacketDialer{control: &socks5Dialer{base: stream, server: cfg.Server, user: cfg.Username, password: cfg.Password}, packets: packets, maxPayload: cfg.MaxUDPPayload}, nil
}

func (d *socksPacketDialer) OpenPacket(ctx context.Context) (transport.PacketSession, error) {
	conn, err := d.control.authenticatedConn(ctx)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			conn.Close()
		}
	}()
	// The packet source address is allocated at the remote relay, so RFC 1928's
	// unspecified source is the correct association request.
	if _, err = conn.Write([]byte{5, 3, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return nil, fmt.Errorf("SOCKS5 UDP associate write: %w", err)
	}
	header := make([]byte, 4)
	if _, err = io.ReadFull(conn, header); err != nil {
		return nil, fmt.Errorf("SOCKS5 UDP associate read: %w", err)
	}
	if header[0] != 5 || header[2] != 0 {
		return nil, errors.New("SOCKS5 UDP associate: invalid reply")
	}
	if header[1] != 0 {
		return nil, fmt.Errorf("SOCKS5 UDP associate rejected (status 0x%02x)", header[1])
	}
	relay, err := readAddress(conn, header[3])
	if err != nil {
		return nil, fmt.Errorf("SOCKS5 UDP relay address: %w", err)
	}
	host, port, _ := net.SplitHostPort(relay)
	if port == "0" {
		return nil, errors.New("SOCKS5 UDP relay returned zero port")
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		host, _, _ = net.SplitHostPort(d.control.server)
		relay = net.JoinHostPort(host, port)
	}
	packet, err := d.packets.OpenPacket(ctx)
	if err != nil {
		return nil, err
	}
	if err = conn.SetDeadline(time.Time{}); err != nil {
		packet.Close()
		return nil, err
	}
	session := &socksPacketSession{control: conn, packet: packet, relay: relay, maxPayload: d.maxPayload}
	success = true
	// A UDP association remains valid only while its TCP control stream lives.
	go func() { _, _ = io.Copy(io.Discard, conn); session.Close() }()
	return session, nil
}

func readAddress(reader io.Reader, typ byte) (string, error) {
	var host string
	switch typ {
	case 1:
		b := make([]byte, 4)
		if _, err := io.ReadFull(reader, b); err != nil {
			return "", err
		}
		host = net.IP(b).String()
	case 4:
		b := make([]byte, 16)
		if _, err := io.ReadFull(reader, b); err != nil {
			return "", err
		}
		host = net.IP(b).String()
	case 3:
		var n [1]byte
		if _, err := io.ReadFull(reader, n[:]); err != nil {
			return "", err
		}
		if n[0] == 0 {
			return "", errors.New("empty domain")
		}
		b := make([]byte, int(n[0]))
		if _, err := io.ReadFull(reader, b); err != nil {
			return "", err
		}
		host = string(b)
	default:
		return "", errors.New("unknown address type")
	}
	var p [2]byte
	if _, err := io.ReadFull(reader, p[:]); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(p[:])))), nil
}

func encodeUDP(payload []byte, target string) ([]byte, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return nil, errors.New("invalid UDP destination port")
	}
	b := []byte{0, 0, 0}
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			b = append(b, 1)
			b = append(b, v4...)
		} else {
			b = append(b, 4)
			b = append(b, ip.To16()...)
		}
	} else {
		if len(host) == 0 || len(host) > 255 {
			return nil, errors.New("invalid UDP destination domain")
		}
		b = append(b, 3, byte(len(host)))
		b = append(b, host...)
	}
	b = append(b, byte(p>>8), byte(p))
	return append(b, payload...), nil
}

type socksPacketSession struct {
	control    net.Conn
	packet     transport.PacketSession
	relay      string
	maxPayload int
	sendMu     sync.Mutex
	mu         sync.RWMutex
	target     string
	closed     bool
	once       sync.Once
}

func (s *socksPacketSession) Send(payload []byte, target string) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if s.maxPayload > 0 && len(payload) > s.maxPayload {
		return fmt.Errorf("UDP payload exceeds landing limit (%d bytes)", s.maxPayload)
	}
	b, err := encodeUDP(payload, target)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return net.ErrClosed
	}
	// The deployed landing pins an association to its first destination. The
	// dataplane already keys sessions by source AND destination; enforce this
	// contract for other callers rather than misrouting a later destination.
	if s.target != "" && s.target != target {
		s.mu.Unlock()
		return errors.New("SOCKS5 UDP association is bound to one destination")
	}
	s.target = target
	s.mu.Unlock()
	return s.packet.Send(b, s.relay)
}
func (s *socksPacketSession) Receive() ([]byte, string, error) {
	for {
		b, source, err := s.packet.Receive()
		if err != nil {
			return nil, "", err
		}
		// Accept only replies from the negotiated UDP relay when it is numeric.
		relayHost, _, _ := net.SplitHostPort(s.relay)
		if net.ParseIP(relayHost) != nil && source != s.relay {
			continue
		}
		if len(b) < 4 || b[0] != 0 || b[1] != 0 || b[2] != 0 {
			continue
		}
		reader := bytes.NewReader(b[4:])
		target, err := readAddress(reader, b[3])
		if err != nil {
			continue
		}
		s.mu.RLock()
		bound, closed := s.target, s.closed
		s.mu.RUnlock()
		if closed {
			return nil, "", net.ErrClosed
		}
		host, _, _ := net.SplitHostPort(bound)
		if net.ParseIP(host) != nil && target != bound {
			continue
		}
		return b[len(b)-reader.Len():], target, nil
	}
}
func (s *socksPacketSession) Close() error {
	var err error
	s.once.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		err = errors.Join(s.control.Close(), s.packet.Close())
	})
	return err
}
