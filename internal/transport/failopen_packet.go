package transport

import (
	"context"
	"sync"

	"github.com/xmdragon/hy2route/internal/failover"
)

type failOpenPacketDialer struct {
	proxy, direct PacketDialer
	controller    *failover.Controller
	events        EventSink
}

func NewFailOpenPacket(proxy, direct PacketDialer, controller *failover.Controller, events EventSink) PacketDialer {
	if events == nil {
		events = discardSink{}
	}
	return &failOpenPacketDialer{proxy: proxy, direct: direct, controller: controller, events: events}
}

func (d *failOpenPacketDialer) OpenPacket(ctx context.Context) (PacketSession, error) {
	if d.controller.Mode() != failover.Proxy {
		return d.direct.OpenPacket(ctx)
	}
	session, err := d.proxy.OpenPacket(ctx)
	if err == nil {
		d.controller.RecordSuccess()
		return &failOpenPacketSession{session: session, direct: d.direct, controller: d.controller, events: d.events}, nil
	}
	d.controller.RecordFailure()
	d.events.Emit(Event{Stage: "fail-open-udp", Reason: err.Error()})
	return d.direct.OpenPacket(ctx)
}

type failOpenPacketSession struct {
	mu         sync.RWMutex
	session    PacketSession
	direct     PacketDialer
	controller *failover.Controller
	events     EventSink
	directMode bool
}

func (s *failOpenPacketSession) Send(payload []byte, target string) error {
	s.mu.RLock()
	session := s.session
	directMode := s.directMode
	s.mu.RUnlock()
	err := session.Send(payload, target)
	if err == nil || directMode {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.directMode {
		return err
	}
	s.controller.RecordFailure()
	s.events.Emit(Event{Stage: "fail-open-udp", Reason: err.Error()})
	_ = s.session.Close()
	direct, openErr := s.direct.OpenPacket(context.Background())
	if openErr != nil {
		return openErr
	}
	s.session = direct
	s.directMode = true
	return err
}

func (s *failOpenPacketSession) Receive() ([]byte, string, error) {
	s.mu.RLock()
	session := s.session
	s.mu.RUnlock()
	return session.Receive()
}

func (s *failOpenPacketSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session.Close()
}
