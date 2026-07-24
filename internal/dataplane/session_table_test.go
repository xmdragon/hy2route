package dataplane

import (
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

func TestUDPSessionKeyIncludesSourceAndDestination(t *testing.T) {
	a := sessionKey{Source: netip.MustParseAddrPort("192.168.80.20:40000"), Target: netip.MustParseAddrPort("8.8.8.8:53")}
	b := sessionKey{Source: netip.MustParseAddrPort("192.168.80.20:40000"), Target: netip.MustParseAddrPort("1.1.1.1:53")}
	if a == b {
		t.Fatal("destinations collapsed")
	}
}

func TestSessionTableEvictsLRUAtCapacity(t *testing.T) {
	now := time.Unix(100, 0)
	table := newSessionTable(2, time.Minute, func() time.Time { return now })
	first := &testSession{}
	table.add(sessionKey{Source: netip.MustParseAddrPort("192.168.80.20:40000"), Target: netip.MustParseAddrPort("203.0.113.1:53")}, first)
	now = now.Add(time.Second)
	table.add(sessionKey{Source: netip.MustParseAddrPort("192.168.80.20:40001"), Target: netip.MustParseAddrPort("203.0.113.2:53")}, &testSession{})
	now = now.Add(time.Second)
	table.add(sessionKey{Source: netip.MustParseAddrPort("192.168.80.20:40002"), Target: netip.MustParseAddrPort("203.0.113.3:53")}, &testSession{})
	if !first.closed.Load() || table.len() != 2 {
		t.Fatalf("closed=%v len=%d", first.closed.Load(), table.len())
	}
}

func TestSessionTableExpiresIdleEntries(t *testing.T) {
	now := time.Unix(100, 0)
	table := newSessionTable(2, time.Second, func() time.Time { return now })
	session := &testSession{}
	table.add(sessionKey{Source: netip.MustParseAddrPort("192.168.80.20:40000"), Target: netip.MustParseAddrPort("203.0.113.1:53")}, session)
	now = now.Add(time.Second)
	table.expire()
	if !session.closed.Load() || table.len() != 0 {
		t.Fatalf("closed=%v len=%d", session.closed.Load(), table.len())
	}
}

type testSession struct{ closed atomic.Bool }

func (s *testSession) Close() error { s.closed.Store(true); return nil }
