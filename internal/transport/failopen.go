package transport

import (
	"context"
	"github.com/xmdragon/hy2route/internal/failover"
	"net"
)

type failOpen struct {
	proxy, direct StreamDialer
	controller    *failover.Controller
	events        EventSink
}

func NewFailOpen(proxy, direct StreamDialer, c *failover.Controller, e EventSink) StreamDialer {
	if e == nil {
		e = discardSink{}
	}
	return &failOpen{proxy, direct, c, e}
}
func (d *failOpen) Dial(ctx context.Context, target string) (net.Conn, error) {
	if d.controller.Mode() != failover.Proxy {
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

type discardSink struct{}

func (discardSink) Emit(Event) {}
