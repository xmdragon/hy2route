package dataplane

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/xmdragon/hy2route/internal/policy"
	"github.com/xmdragon/hy2route/internal/transport"
)

type UDPServer struct {
	Classifier *policy.Classifier
	Learned    *policy.LearningTable
	Direct     transport.PacketDialer
	Proxy      transport.PacketDialer
	Sessions   *sessionTable
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
		return existing.(transport.PacketSession).Send(payload, target.String())
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
