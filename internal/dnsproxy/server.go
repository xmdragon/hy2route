package dnsproxy

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/miekg/dns"
)

type Server struct {
	listen   string
	resolver *Resolver

	mu   sync.RWMutex
	addr string
}

func NewServer(listen string, resolver *Resolver) *Server {
	return &Server{listen: listen, resolver: resolver}
}

func (server *Server) Addr() string {
	server.mu.RLock()
	defer server.mu.RUnlock()
	return server.addr
}

func (server *Server) Run(ctx context.Context) error {
	if server.resolver == nil {
		return errors.New("DNS server resolver is required")
	}
	udpConn, err := net.ListenPacket("udp4", server.listen)
	if err != nil {
		return err
	}
	defer udpConn.Close()
	udpAddress, ok := udpConn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return errors.New("DNS UDP listener did not return a UDP address")
	}
	tcpListener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: udpAddress.IP, Port: udpAddress.Port})
	if err != nil {
		return err
	}
	defer tcpListener.Close()

	handler := dns.HandlerFunc(server.serveDNS)
	udpServer := &dns.Server{PacketConn: udpConn, Handler: handler, UDPSize: 1232}
	tcpServer := &dns.Server{Listener: tcpListener, Handler: handler}
	server.mu.Lock()
	server.addr = tcpListener.Addr().String()
	server.mu.Unlock()

	errors := make(chan error, 2)
	go func() { errors <- udpServer.ActivateAndServe() }()
	go func() { errors <- tcpServer.ActivateAndServe() }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = udpServer.ShutdownContext(shutdownCtx)
		_ = tcpServer.ShutdownContext(shutdownCtx)
		first := <-errors
		second := <-errors
		if isServerClosed(first) && isServerClosed(second) {
			return nil
		}
		if !isServerClosed(first) {
			return first
		}
		return second
	case err := <-errors:
		if isServerClosed(err) {
			return nil
		}
		return err
	}
}

func (server *Server) serveDNS(writer dns.ResponseWriter, request *dns.Msg) {
	response, err := server.resolver.Resolve(context.Background(), request)
	if err != nil {
		response = errorReply(request, dns.RcodeServerFailure)
	}
	_ = writer.WriteMsg(response)
}

func isServerClosed(err error) bool {
	return err == nil || errors.Is(err, net.ErrClosed)
}
