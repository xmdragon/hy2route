package dataplane

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"sync/atomic"
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
				if err := server.handle(ctx, conn); err != nil && ctx.Err() == nil {
					log.Printf("stage=tcp-connection remote=%s local=%s error=%v", conn.RemoteAddr(), conn.LocalAddr(), err)
				}
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
	target, targetIP, err := originalTarget(inbound, server.ListenAddr)
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

func originalTarget(conn net.Conn, listenAddr string) (string, netip.Addr, error) {
	addr, err := socketOriginalDestination(conn)
	if err != nil || addr == nil {
		addr = conn.LocalAddr()
		if samePort(addr, listenAddr) {
			return "", netip.Addr{}, fmt.Errorf("read redirected original destination: %w", err)
		}
	}
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

func samePort(addr net.Addr, listenAddr string) bool {
	if addr == nil || listenAddr == "" {
		return false
	}
	_, localPort, localErr := net.SplitHostPort(addr.String())
	_, listenPort, listenErr := net.SplitHostPort(listenAddr)
	return localErr == nil && listenErr == nil && localPort == listenPort
}

func relay(inbound, outbound net.Conn) error {
	return relayFrom(inbound, nil, outbound)
}

func relayFrom(inbound net.Conn, replay *bufio.Reader, outbound net.Conn) error {
	if replay == nil {
		replay = bufio.NewReader(inbound)
	}
	type copyResult struct {
		first       bool
		err         error
		fullyClosed bool
	}
	results := make(chan copyResult, 2)
	var completed atomic.Int32
	go func() {
		_, err := io.CopyBuffer(outbound, replay, make([]byte, 32<<10))
		first := completed.CompareAndSwap(0, 1)
		results <- copyResult{first: first, err: err, fullyClosed: closeWrite(outbound)}
	}()
	go func() {
		_, err := io.CopyBuffer(inbound, outbound, make([]byte, 32<<10))
		first := completed.CompareAndSwap(0, 2)
		results <- copyResult{first: first, err: err, fullyClosed: closeWrite(inbound)}
	}()
	var first, second copyResult
	for range 2 {
		result := <-results
		if result.first {
			first = result
		} else {
			second = result
		}
	}
	if err := relayError(first.err); err != nil {
		return err
	}
	if first.fullyClosed {
		// Closing a connection without half-close support is what releases the
		// opposite copy. Its resulting local-close error is expected.
		return nil
	}
	return relayError(second.err)
}

func closeWrite(conn net.Conn) bool {
	if closer, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = closer.CloseWrite()
		return false
	}
	// Hysteria's TCP stream exposes net.Conn but not CloseWrite. Leaving that
	// stream open keeps the opposite copy blocked after the TCP peer exits,
	// so the handler and its active-session slot never return.
	_ = conn.Close()
	return true
}

func relayError(err error) error {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
