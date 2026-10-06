// Package reality implements the optional VLESS Vision relay using upstream
// sing-box TLS and sing-vmess protocol implementations.
package reality

import (
	"context"
	"fmt"
	"net"
	"time"

	stls "github.com/sagernet/sing-box/common/tls"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-vmess/vless"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/xmdragon/hy2route/internal/config"
	"github.com/xmdragon/hy2route/internal/transport"
)

type Client struct {
	cfg      config.TCPRelayConfig
	tls      stls.Config
	protocol *vless.Client
	dialer   *net.Dialer
}

func New(cfg config.TCPRelayConfig, mark uint32) (*Client, error) {
	host, _, err := net.SplitHostPort(cfg.Server)
	if err != nil {
		return nil, err
	}
	tlsConfig, err := stls.NewClient(context.Background(), host, option.OutboundTLSOptions{
		Enabled: true, ServerName: cfg.ServerName,
		UTLS:    &option.OutboundUTLSOptions{Enabled: true, Fingerprint: cfg.Fingerprint},
		Reality: &option.OutboundRealityOptions{Enabled: true, PublicKey: cfg.PublicKey, ShortID: cfg.ShortID},
	})
	if err != nil {
		return nil, fmt.Errorf("Reality TLS: %w", err)
	}
	flow := cfg.Flow
	if flow == "xtls-rprx-vision-udp443" {
		flow = "xtls-rprx-vision"
	}
	protocol, err := vless.NewClient(cfg.UUID, flow, logger.NOP())
	if err != nil {
		return nil, err
	}
	return &Client{cfg: cfg, tls: tlsConfig, protocol: protocol, dialer: transport.NewMarkedDialer(mark)}, nil
}

func (c *Client) connect(ctx context.Context) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	raw, err := c.dialer.DialContext(ctx, "tcp4", c.cfg.Server)
	if err != nil {
		return nil, err
	}
	conn, err := stls.ClientHandshake(ctx, raw, c.tls)
	if err != nil {
		raw.Close()
		return nil, err
	}
	return conn, nil
}

func (c *Client) Dial(ctx context.Context, target string) (net.Conn, error) {
	destination := M.ParseSocksaddr(target)
	if !destination.IsValid() {
		return nil, fmt.Errorf("invalid VLESS destination")
	}
	conn, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(4 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	protocol, err := c.protocol.DialConn(conn, destination)
	if err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	return protocol, nil
}

func (c *Client) OpenPacket(ctx context.Context) (transport.PacketSession, error) {
	conn, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}
	conn.SetDeadline(time.Now().Add(4 * time.Second))
	packet, err := c.protocol.DialEarlyXUDPPacketConn(conn, M.Socksaddr{})
	if err != nil {
		conn.Close()
		return nil, err
	}
	conn.SetDeadline(time.Time{})
	return &packetSession{conn: packet}, nil
}

type packetSession struct{ conn net.PacketConn }

func (s *packetSession) Send(payload []byte, target string) error {
	addr := M.ParseSocksaddr(target)
	if !addr.IsValid() {
		return fmt.Errorf("invalid XUDP destination")
	}
	_, err := s.conn.WriteTo(payload, addr)
	return err
}
func (s *packetSession) Receive() ([]byte, string, error) {
	b := make([]byte, 65536)
	n, addr, err := s.conn.ReadFrom(b)
	if err != nil {
		return nil, "", err
	}
	return b[:n], addr.String(), nil
}
func (s *packetSession) Close() error { return s.conn.Close() }
