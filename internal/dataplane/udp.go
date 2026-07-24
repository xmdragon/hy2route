package dataplane

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/apernet/go-tproxy"
	"github.com/xmdragon/hy2route/internal/policy"
	"github.com/xmdragon/hy2route/internal/transport"
)

type UDPServer struct {
	ListenAddr string
	Classifier *policy.Classifier
	Learned    *policy.LearningTable
	Direct     transport.PacketDialer
	Proxy      transport.PacketDialer
	Sessions   *sessionTable
}

type udpSession struct {
	packet transport.PacketSession
	reply  *net.UDPConn
	once   sync.Once
}

func (s *udpSession) Send(payload []byte, target string) error { return s.packet.Send(payload, target) }
func (s *udpSession) Close() error {
	var err error
	s.once.Do(func() {
		if s.reply != nil {
			_ = s.reply.Close()
		}
		err = s.packet.Close()
	})
	return err
}

func (server *UDPServer) Run(ctx context.Context) error {
	addr, err := net.ResolveUDPAddr("udp4", server.ListenAddr)
	if err != nil {
		return err
	}
	conn, err := tproxy.ListenUDP("udp4", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	go func() { <-ctx.Done(); _ = conn.Close() }()
	buffer := make([]byte, 64<<10)
	for {
		n, source, target, err := tproxy.ReadFromUDP(conn, buffer)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if source == nil || target == nil {
			continue
		}
		sourceAddr, ok1 := netip.AddrFromSlice(source.IP)
		targetAddr, ok2 := netip.AddrFromSlice(target.IP)
		if !ok1 || !ok2 || !sourceAddr.Is4() || !targetAddr.Is4() {
			continue
		}
		payload := append([]byte(nil), buffer[:n]...)
		if err := server.handlePacket(ctx, netip.AddrPortFrom(sourceAddr, uint16(source.Port)), netip.AddrPortFrom(targetAddr, uint16(target.Port)), payload); err != nil && ctx.Err() != nil {
			return nil
		}
	}
}

func newUDPServerForTest(learned *policy.LearningTable, direct, proxy transport.PacketDialer) *UDPServer {
	return &UDPServer{Learned: learned, Direct: direct, Proxy: proxy, Sessions: newSessionTable(8, time.Minute, nil)}
}

func (server *UDPServer) handleFirst(ctx context.Context, source, target netip.AddrPort, payload []byte) error {
	if server.Direct == nil || server.Proxy == nil {
		return errors.New("UDP server is not configured")
	}
	key := sessionKey{Source: source, Target: target}
	if existing := server.Sessions.get(key); existing != nil {
		return existing.(interface{ Send([]byte, string) error }).Send(payload, target.String())
	}
	dialer := server.Proxy
	if server.shouldDirect(target.Addr()) {
		dialer = server.Direct
	}
	session, err := dialer.OpenPacket(ctx)
	if err != nil {
		return err
	}
	server.Sessions.add(key, session)
	if err := session.Send(payload, target.String()); err != nil {
		return err
	}
	return nil
}

func (server *UDPServer) handlePacket(ctx context.Context, source, target netip.AddrPort, payload []byte) error {
	key := sessionKey{Source: source, Target: target}
	if existing := server.Sessions.get(key); existing != nil {
		return existing.(interface{ Send([]byte, string) error }).Send(payload, target.String())
	}
	if server.Direct == nil || server.Proxy == nil {
		return errors.New("UDP server is not configured")
	}
	dialer := server.Proxy
	if server.shouldDirect(target.Addr()) {
		dialer = server.Direct
	}
	packet, err := dialer.OpenPacket(ctx)
	if err != nil {
		return err
	}
	reply, err := tproxy.DialUDP("udp4", net.UDPAddrFromAddrPort(target), net.UDPAddrFromAddrPort(source))
	if err != nil {
		_ = packet.Close()
		return err
	}
	session := &udpSession{packet: packet, reply: reply}
	server.Sessions.add(key, session)
	go server.forwardUDPReply(ctx, session)
	return session.Send(payload, target.String())
}

func (server *UDPServer) forwardUDPReply(ctx context.Context, session *udpSession) {
	for {
		payload, _, err := session.packet.Receive()
		if err != nil {
			return
		}
		if _, err := session.reply.Write(payload); err != nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func (server *UDPServer) shouldDirect(target netip.Addr) bool {
	if server.Classifier != nil {
		decision := server.Classifier.IP(target)
		if decision.Source == policy.SourceExplicitIP {
			return decision.Action == policy.Direct
		}
	}
	if server.Learned != nil {
		learned := server.Learned.Lookup(target, time.Now())
		if learned.Proxy {
			return false
		}
		if learned.Direct {
			return true
		}
	}
	return server.Classifier != nil && server.Classifier.IP(target).Action == policy.Direct
}
