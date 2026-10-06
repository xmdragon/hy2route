package transport

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

// RelayFallback keeps failed relays in cooldown without changing the landing
// destination. Existing connections are never replayed onto a different path.
type RelayFallback struct {
	primary, backup         StreamDialer
	cooldown                time.Duration
	mu                      sync.Mutex
	retryAt                 time.Time
	active                  string
	primaryName, backupName string
}

func NewRelayFallback(primary, backup StreamDialer, primaryName, backupName string, cooldown time.Duration) *RelayFallback {
	return &RelayFallback{primary: primary, backup: backup, cooldown: cooldown, primaryName: primaryName, backupName: backupName, active: primaryName}
}
func (d *RelayFallback) Dial(ctx context.Context, target string) (net.Conn, error) {
	d.mu.Lock()
	tryPrimary := !time.Now().Before(d.retryAt)
	d.mu.Unlock()
	var primaryErr error
	if tryPrimary {
		timeout := 4 * time.Second
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline)/2 < timeout {
			timeout = time.Until(deadline) / 2
		}
		attempt, cancel := context.WithTimeout(ctx, timeout)
		conn, err := d.primary.Dial(attempt, target)
		cancel()
		if err == nil {
			d.mu.Lock()
			d.retryAt = time.Time{}
			d.active = d.primaryName
			d.mu.Unlock()
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		primaryErr = err
		d.mu.Lock()
		d.retryAt = time.Now().Add(d.cooldown)
		d.mu.Unlock()
	}
	conn, err := d.backup.Dial(ctx, target)
	if err == nil {
		d.mu.Lock()
		d.active = d.backupName
		d.mu.Unlock()
	}
	if err != nil && primaryErr != nil {
		return nil, errors.Join(primaryErr, err)
	}
	return conn, err
}
func (d *RelayFallback) Active() string { d.mu.Lock(); defer d.mu.Unlock(); return d.active }

// PacketRelayFallback uses HY2 by default and VLESS XUDP only during HY2
// failures. An optional SOCKS5 UDP landing can wrap both relay paths.
type PacketRelayFallback struct {
	primary, backup PacketDialer
	cooldown        time.Duration
	mu              sync.Mutex
	retryAt         time.Time
	active          string
}

func NewPacketRelayFallback(primary, backup PacketDialer, cooldown time.Duration) *PacketRelayFallback {
	return &PacketRelayFallback{primary: primary, backup: backup, cooldown: cooldown, active: "hy2"}
}
func (d *PacketRelayFallback) OpenPacket(ctx context.Context) (PacketSession, error) {
	d.mu.Lock()
	tryPrimary := !time.Now().Before(d.retryAt)
	d.mu.Unlock()
	if tryPrimary {
		attempt, cancel := context.WithTimeout(ctx, 4*time.Second)
		session, err := d.primary.OpenPacket(attempt)
		cancel()
		if err == nil {
			d.mu.Lock()
			d.active = "hy2"
			d.retryAt = time.Time{}
			d.mu.Unlock()
			return &fallbackPacketSession{owner: d, session: session}, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		d.failed()
	}
	session, err := d.backup.OpenPacket(ctx)
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	d.active = "reality"
	d.mu.Unlock()
	return session, nil
}
func (d *PacketRelayFallback) failed() {
	d.mu.Lock()
	d.retryAt = time.Now().Add(d.cooldown)
	d.mu.Unlock()
}
func (d *PacketRelayFallback) Active() string { d.mu.Lock(); defer d.mu.Unlock(); return d.active }

type fallbackPacketSession struct {
	owner          *PacketRelayFallback
	mu             sync.RWMutex
	session        PacketSession
	backup, closed bool
}

func (s *fallbackPacketSession) Send(payload []byte, target string) error {
	s.mu.RLock()
	if s.closed {
		s.mu.RUnlock()
		return net.ErrClosed
	}
	packet := s.session
	backup := s.backup
	s.mu.RUnlock()
	err := packet.Send(payload, target)
	if err == nil || backup {
		return err
	}
	s.owner.failed()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return net.ErrClosed
	}
	if !s.backup {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		next, openErr := s.owner.backup.OpenPacket(ctx)
		if openErr != nil {
			return errors.Join(err, openErr)
		}
		s.session = next
		s.backup = true
		// Publish the replacement before waking a blocked receiver.
		packet.Close()
		s.owner.mu.Lock()
		s.owner.active = "reality"
		s.owner.mu.Unlock()
	}
	return s.session.Send(payload, target)
}
func (s *fallbackPacketSession) Receive() ([]byte, string, error) {
	for {
		s.mu.RLock()
		if s.closed {
			s.mu.RUnlock()
			return nil, "", net.ErrClosed
		}
		packet := s.session
		s.mu.RUnlock()
		b, target, err := packet.Receive()
		if err == nil {
			return b, target, nil
		}
		s.mu.RLock()
		changed := s.session != packet && !s.closed
		closed, backup := s.closed, s.backup
		s.mu.RUnlock()
		if changed {
			continue
		}
		if !closed && !backup {
			s.owner.failed()
		}
		return nil, "", err
	}
}
func (s *fallbackPacketSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.session.Close()
}
