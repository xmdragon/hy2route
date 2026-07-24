package dataplane

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/apernet/go-tproxy"
	"github.com/xmdragon/hy2route/internal/policy"
	"github.com/xmdragon/hy2route/internal/sniff"
	"github.com/xmdragon/hy2route/internal/transport"
)

type TCPServer struct {
	ListenAddr string
	Ready      chan<- struct{}
	Classifier *policy.Classifier
	Learned    *policy.LearningTable
	Direct     transport.StreamDialer
	Proxy      transport.StreamDialer
	Sniff      sniff.Limits
	MaxActive  int
}

func (server *TCPServer) Run(ctx context.Context) error {
	addr, err := net.ResolveTCPAddr("tcp4", server.ListenAddr)
	if err != nil {
		return err
	}
	listener, err := tproxy.ListenTCP("tcp4", addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	if server.Ready != nil {
		close(server.Ready)
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	maxActive := server.MaxActive
	if maxActive < 1 {
		maxActive = 1024
	}
	active := make(chan struct{}, maxActive)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case active <- struct{}{}:
			go func() {
				defer func() { <-active }()
				_ = server.handle(ctx, conn)
			}()
		default:
			_ = conn.Close()
		}
	}
}

func (server *TCPServer) handle(ctx context.Context, inbound net.Conn) error {
	defer inbound.Close()
	if server.Classifier == nil || server.Direct == nil || server.Proxy == nil {
		return errors.New("TCP server is not configured")
	}
	target, targetIP, err := originalTarget(inbound.LocalAddr())
	if err != nil {
		return err
	}
	result, reader, err := sniff.Peek(ctx, inbound, server.Sniff)
	if err != nil {
		return err
	}
	dialer := server.selectDialer(targetIP, result.Domain)
	outbound, err := dialer.Dial(ctx, target)
	if err != nil {
		return err
	}
	defer outbound.Close()
	return relayFrom(inbound, reader, outbound)
}

func (server *TCPServer) selectDialer(target netip.Addr, domain string) transport.StreamDialer {
	if domain != "" {
		if server.Classifier.Domain(domain).Action == policy.Direct {
			return server.Direct
		}
		return server.Proxy
	}
	ipDecision := server.Classifier.IP(target)
	if ipDecision.Source == policy.SourceExplicitIP {
		if ipDecision.Action == policy.Direct {
			return server.Direct
		}
		return server.Proxy
	}
	if server.Learned != nil {
		learned := server.Learned.Lookup(target, time.Now())
		if learned.Direct && !learned.Proxy {
			return server.Direct
		}
		if learned.Proxy {
			return server.Proxy
		}
	}
	if ipDecision.Action == policy.Direct {
		return server.Direct
	}
	return server.Proxy
}

func originalTarget(addr net.Addr) (string, netip.Addr, error) {
	if addr == nil {
		return "", netip.Addr{}, errors.New("original destination is missing")
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "", netip.Addr{}, err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Is4() || ip.Is4In6() {
		return "", netip.Addr{}, errors.New("original destination must be IPv4")
	}
	return addr.String(), ip, nil
}

func relay(inbound, outbound net.Conn) error {
	return relayFrom(inbound, nil, outbound)
}

func relayFrom(inbound net.Conn, replay *bufio.Reader, outbound net.Conn) error {
	if replay == nil {
		replay = bufio.NewReader(inbound)
	}
	errorsCh := make(chan error, 2)
	var group sync.WaitGroup
	group.Add(2)
	go func() {
		defer group.Done()
		_, err := io.CopyBuffer(outbound, replay, make([]byte, 32<<10))
		closeWrite(outbound)
		errorsCh <- relayError(err)
	}()
	go func() {
		defer group.Done()
		_, err := io.CopyBuffer(inbound, outbound, make([]byte, 32<<10))
		closeWrite(inbound)
		errorsCh <- relayError(err)
	}()
	group.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func closeWrite(conn net.Conn) {
	if closer, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = closer.CloseWrite()
	}
}

func relayError(err error) error {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
