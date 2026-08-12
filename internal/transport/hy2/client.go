package hy2

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"

	coreclient "github.com/apernet/hysteria/core/v2/client"
	coreErrs "github.com/apernet/hysteria/core/v2/errors"
	"github.com/xmdragon/hy2route/internal/config"
	"github.com/xmdragon/hy2route/internal/transport"
)

type coreClient interface {
	TCP(string) (net.Conn, uint64, error)
	UDP() (coreclient.HyUDPConn, uint64, error)
	Close() error
}

type BootstrapResolver interface {
	LookupIPv4(context.Context, string) (netip.Addr, error)
}

type Client struct {
	core   coreClient
	sem    chan struct{}
	events transport.EventSink
}

func New(cfg config.HY2Config, bootstrap BootstrapResolver, events transport.EventSink, mark ...uint32) (*Client, error) {
	if bootstrap == nil {
		return nil, errors.New("HY2 bootstrap resolver is required")
	}
	if events == nil {
		events = discardEvents{}
	}
	if cfg.MaxConcurrentDials < 1 {
		cfg.MaxConcurrentDials = 32
	}
	var bypassMark uint32
	if len(mark) != 0 {
		bypassMark = mark[0]
	}
	client := &Client{sem: make(chan struct{}, cfg.MaxConcurrentDials), events: events}
	client.core = newReconnectableCore(func() (*coreclient.Config, error) {
		return buildCoreConfig(context.Background(), cfg, bootstrap, bypassMark)
	}, events)
	return client, nil
}

func newWithCoreClient(core coreClient, maxConcurrentDials int) *Client {
	if maxConcurrentDials < 1 {
		maxConcurrentDials = 1
	}
	return &Client{core: core, sem: make(chan struct{}, maxConcurrentDials), events: discardEvents{}}
}

func (client *Client) Dial(ctx context.Context, target string) (net.Conn, error) {
	select {
	case client.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	type result struct {
		conn     net.Conn
		sequence uint64
		err      error
	}
	resultCh := make(chan result, 1)
	go func() {
		conn, sequence, err := client.core.TCP(target)
		resultCh <- result{conn: conn, sequence: sequence, err: err}
	}()
	select {
	case result := <-resultCh:
		<-client.sem
		if result.err != nil && isTransportFailure(result.err) {
			client.events.Emit(transport.Event{Stage: "hy2.tcp", Reason: result.err.Error(), Sequence: result.sequence})
		}
		return result.conn, result.err
	case <-ctx.Done():
		go func() {
			result := <-resultCh
			if result.conn != nil {
				_ = result.conn.Close()
			}
			<-client.sem
		}()
		return nil, ctx.Err()
	}
}

func (client *Client) OpenPacket(ctx context.Context) (transport.PacketSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	packet, sequence, err := client.core.UDP()
	if err != nil {
		if isTransportFailure(err) {
			client.events.Emit(transport.Event{Stage: "hy2.udp", Reason: err.Error(), Sequence: sequence})
		}
		return nil, err
	}
	return packet, nil
}

func (client *Client) Close() error { return client.core.Close() }

func isTransportFailure(err error) bool {
	var dialError coreErrs.DialError
	return !errors.As(err, &dialError)
}

func buildCoreConfig(ctx context.Context, cfg config.HY2Config, bootstrap BootstrapResolver, mark ...uint32) (*coreclient.Config, error) {
	host, rawPort, err := net.SplitHostPort(cfg.Server)
	if err != nil {
		return nil, fmt.Errorf("HY2 server: %w", err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("HY2 server port is invalid")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		ip, err = bootstrap.LookupIPv4(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("bootstrap HY2 server: %w", err)
		}
	}
	if !ip.Is4() || ip.Is4In6() {
		return nil, errors.New("HY2 server must resolve to IPv4")
	}
	var bypassMark uint32
	if len(mark) != 0 {
		bypassMark = mark[0]
	}
	return &coreclient.Config{
		ConnFactory: markedPacketConnFactory{mark: bypassMark},
		ServerAddr:  &net.UDPAddr{IP: ip.AsSlice(), Port: port},
		Auth:        cfg.Auth,
		TLSConfig: coreclient.TLSConfig{
			ServerName:            cfg.SNI,
			InsecureSkipVerify:    cfg.Insecure,
			VerifyPeerCertificate: pinVerifier(cfg.PinnedCertSHA256),
		},
		QUICConfig: coreclient.QUICConfig{
			InitialStreamReceiveWindow:     cfg.InitialStreamWindow,
			MaxStreamReceiveWindow:         cfg.MaxStreamWindow,
			InitialConnectionReceiveWindow: cfg.InitialConnectionWindow,
			MaxConnectionReceiveWindow:     cfg.MaxConnectionWindow,
			MaxIdleTimeout:                 cfg.MaxIdle.Value(),
			KeepAlivePeriod:                cfg.KeepAlive.Value(),
		},
		CongestionConfig: coreclient.CongestionConfig{Type: "bbr", BBRProfile: "standard"},
	}, nil
}

type markedPacketConnFactory struct{ mark uint32 }

func (factory markedPacketConnFactory) New(net.Addr) (net.PacketConn, error) {
	return transport.ListenMarkedPacket(context.Background(), "udp4", "0.0.0.0:0", factory.mark)
}

func pinVerifier(pin string) func([][]byte, [][]*x509.Certificate) error {
	if pin == "" {
		return nil
	}
	expected, err := hex.DecodeString(pin)
	if err != nil || len(expected) != sha256.Size {
		return func([][]byte, [][]*x509.Certificate) error { return errors.New("invalid certificate pin") }
	}
	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		if len(rawCerts) == 0 {
			return errors.New("peer sent no certificate")
		}
		actual := sha256.Sum256(rawCerts[0])
		if subtle.ConstantTimeCompare(actual[:], expected) != 1 {
			return errors.New("certificate pin mismatch")
		}
		return nil
	}
}

type discardEvents struct{}

func (discardEvents) Emit(transport.Event) {}
