package dnsproxy

import (
	"context"

	"github.com/miekg/dns"
	"github.com/xmdragon/hy2route/internal/policy"
)

type Exchanger interface {
	Exchange(context.Context, *dns.Msg) (*dns.Msg, error)
}

type Learner interface {
	Observe(context.Context, policy.Observation) error
}
