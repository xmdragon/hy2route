package firewall

import (
	"context"
	"github.com/xmdragon/hy2route/internal/policy"
	"net/netip"
	"time"
)

type SetState uint8

const (
	SetNone SetState = iota
	SetDirect
	SetInspect
)

type SetUpdate struct {
	IP    netip.Addr
	State SetState
	TTL   time.Duration
}
type SetClient interface {
	Replace(context.Context, []SetUpdate) error
	Heartbeat(context.Context, time.Duration) error
}
type Learner struct {
	table *policy.LearningTable
	sets  SetClient
	now   func() time.Time
}

func NewLearner(t *policy.LearningTable, s SetClient, n func() time.Time) *Learner {
	if n == nil {
		n = time.Now
	}
	return &Learner{t, s, n}
}
func (l *Learner) Observe(ctx context.Context, o policy.Observation) error {
	if err := l.table.Observe(o); err != nil {
		return err
	}
	return l.reconcile(ctx, o.IPs)
}
func (l *Learner) Expire(ctx context.Context) error { return l.reconcile(ctx, l.table.Expire(l.now())) }
func (l *Learner) reconcile(ctx context.Context, ips []netip.Addr) error {
	updates := make([]SetUpdate, 0, len(ips))
	for _, ip := range ips {
		d := l.table.Lookup(ip, l.now())
		state := SetNone
		if d.Inspect {
			state = SetInspect
		} else if d.Direct && !d.Proxy {
			state = SetDirect
		}
		updates = append(updates, SetUpdate{IP: ip, State: state, TTL: time.Hour})
	}
	if len(updates) == 0 {
		return nil
	}
	return l.sets.Replace(ctx, updates)
}
