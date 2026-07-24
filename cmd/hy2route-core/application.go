package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/xmdragon/hy2route/internal/config"
	"github.com/xmdragon/hy2route/internal/dataset"
	"github.com/xmdragon/hy2route/internal/dnsproxy"
	"github.com/xmdragon/hy2route/internal/policy"
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
	resolver := dnsproxy.NewResolver(
		classifier,
		dnsproxy.NewNetworkExchanger(cfg.DomesticDNS),
		dnsproxy.NewNetworkExchanger(cfg.TrustedDNS),
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
