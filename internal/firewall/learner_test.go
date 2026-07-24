package firewall

import (
	"context"
	"github.com/xmdragon/hy2route/internal/policy"
	"net/netip"
	"testing"
	"time"
)

func TestLearnerMovesConflictFromDirectToInspectAndBack(t *testing.T) {
	now := time.Unix(100, 0)
	table := policy.NewLearningTable(16)
	sets := &fakeSets{}
	l := NewLearner(table, sets, func() time.Time { return now })
	ip := netip.MustParseAddr("203.0.113.8")
	if err := l.Observe(context.Background(), policy.Observation{Domain: "cn.example", Action: policy.Direct, IPs: []netip.Addr{ip}, Expires: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if sets.last(ip) != SetDirect {
		t.Fatal(sets.last(ip))
	}
	if err := l.Observe(context.Background(), policy.Observation{Domain: "world.example", Action: policy.Proxy, IPs: []netip.Addr{ip}, Expires: now.Add(10 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	if sets.last(ip) != SetInspect {
		t.Fatal(sets.last(ip))
	}
	now = now.Add(11 * time.Second)
	if err := l.Expire(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sets.last(ip) != SetDirect {
		t.Fatal(sets.last(ip))
	}
}

type fakeSets struct{ updates []SetUpdate }

func (s *fakeSets) Replace(_ context.Context, updates []SetUpdate) error {
	s.updates = append(s.updates, updates...)
	return nil
}
func (*fakeSets) Heartbeat(context.Context, time.Duration) error { return nil }
func (s *fakeSets) last(ip netip.Addr) SetState {
	for i := len(s.updates) - 1; i >= 0; i-- {
		if s.updates[i].IP == ip {
			return s.updates[i].State
		}
	}
	return SetNone
}
