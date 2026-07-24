package transport

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xmdragon/hy2route/internal/failover"
)

type failOpen struct {
	proxy, direct StreamDialer
	controller    *failover.Controller
	events        EventSink
	probeInterval time.Duration
	probeRunning  atomic.Bool
	probeMu       sync.Mutex
	nextProbe     time.Time
}

func NewFailOpen(proxy, direct StreamDialer, c *failover.Controller, e EventSink) StreamDialer {
	return NewFailOpenWithProbe(proxy, direct, c, e, 10*time.Second)
}

func NewFailOpenWithProbe(proxy, direct StreamDialer, c *failover.Controller, e EventSink, probeInterval time.Duration) StreamDialer {
	if e == nil {
		e = discardSink{}
	}
	if probeInterval <= 0 {
		probeInterval = time.Second
	}
	return &failOpen{proxy: proxy, direct: direct, controller: c, events: e, probeInterval: probeInterval}
}
func (d *failOpen) Dial(ctx context.Context, target string) (net.Conn, error) {
	if d.controller.Mode() != failover.Proxy {
		d.maybeProbe(target)
		return d.direct.Dial(ctx, target)
	}
	conn, err := d.proxy.Dial(ctx, target)
	if err == nil {
		d.controller.RecordSuccess()
		return conn, nil
	}
	d.controller.RecordFailure()
	d.events.Emit(Event{Stage: "fail-open", Reason: err.Error()})
	return d.direct.Dial(ctx, target)
}

func (d *failOpen) maybeProbe(target string) {
	if d.controller.Mode() != failover.DirectRecovery || !d.probeRunning.CompareAndSwap(false, true) {
		return
	}
	now := time.Now()
	d.probeMu.Lock()
	if now.Before(d.nextProbe) {
		d.probeMu.Unlock()
		d.probeRunning.Store(false)
		return
	}
	d.nextProbe = now.Add(d.probeInterval)
	d.probeMu.Unlock()
	go func() {
		defer d.probeRunning.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, err := d.proxy.Dial(ctx, target)
		if err != nil {
			d.controller.RecordFailure()
			d.events.Emit(Event{Stage: "recovery-probe", Reason: err.Error()})
			return
		}
		_ = conn.Close()
		d.controller.RecordSuccess()
	}()
}

type discardSink struct{}

func (discardSink) Emit(Event) {}
