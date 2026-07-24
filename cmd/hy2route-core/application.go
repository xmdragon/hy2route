package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/xmdragon/hy2route/internal/config"
	"github.com/xmdragon/hy2route/internal/dataset"
	"github.com/xmdragon/hy2route/internal/dnsproxy"
	"github.com/xmdragon/hy2route/internal/failover"
	"github.com/xmdragon/hy2route/internal/policy"
	"github.com/xmdragon/hy2route/internal/transport"
	"github.com/xmdragon/hy2route/internal/transport/hy2"
)

type application struct {
	dns *dnsproxy.Server
}

func newApplication(cfg config.Config, dnsOnly bool) (*application, error) {
	if !dnsOnly {
		return nil, errors.New("serve currently requires --dns-only")
	}
	data, err := dataset.Load(cfg.Data.Routing)
	if err != nil {
		return nil, fmt.Errorf("load routing data: %w", err)
	}
	classifier, err := policy.New(data, cfg.Rules)
	if err != nil {
		return nil, fmt.Errorf("build classifier: %w", err)
	}
	learner := policy.NewLearningTable(cfg.Limits.LearnedIPEntries)
	domestic := dnsproxy.NewNetworkExchanger(cfg.DomesticDNS)
	hy2Client, err := hy2.New(cfg.HY2, hy2.NewBootstrapResolver(domestic), nil)
	if err != nil {
		return nil, fmt.Errorf("build HY2 transport: %w", err)
	}
	direct := transport.NewDirectStreamDialer()
	controller := failover.New(failover.Config{
		Failures:  cfg.Health.FailureThreshold,
		Successes: cfg.Health.SuccessThreshold,
		Cooldown:  cfg.Health.Cooldown.Value(),
	}, nil)
	trustedRoute := transport.NewFailOpenWithProbe(hy2Client, direct, controller, nil, cfg.Health.ProbeInterval.Value())
	trusted := transport.NewDNSFallback(
		transport.NewDNSOverStream(trustedRoute, cfg.TrustedDNS, cfg.Limits.SniffTimeout.Value()),
		domestic,
	)
	resolver := dnsproxy.NewResolver(
		classifier,
		domestic,
		trusted,
		learningAdapter{table: learner},
		cfg.Limits.DNSCacheEntries,
		cfg.Limits.SniffTimeout.Value(),
	)
	return &application{dns: dnsproxy.NewServer(cfg.Listen.DNS, resolver)}, nil
}

func (application *application) Run(ctx context.Context) error {
	return application.dns.Run(ctx)
}

type learningAdapter struct{ table *policy.LearningTable }

func (adapter learningAdapter) Observe(_ context.Context, observation policy.Observation) error {
	return adapter.table.Observe(observation)
}
