package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xmdragon/hy2route/internal/config"
	"github.com/xmdragon/hy2route/internal/dataplane"
	"github.com/xmdragon/hy2route/internal/dataset"
	"github.com/xmdragon/hy2route/internal/dnsproxy"
	"github.com/xmdragon/hy2route/internal/failover"
	"github.com/xmdragon/hy2route/internal/firewall"
	"github.com/xmdragon/hy2route/internal/landing"
	"github.com/xmdragon/hy2route/internal/policy"
	"github.com/xmdragon/hy2route/internal/sniff"
	"github.com/xmdragon/hy2route/internal/transport"
	"github.com/xmdragon/hy2route/internal/transport/hy2"
)

type application struct {
	dns     *dnsproxy.Server
	tcp     *dataplane.TCPServer
	udp     *dataplane.UDPServer
	sets    firewall.SetClient
	dnsOnly bool
}

func newApplication(cfg config.Config, dnsOnly bool) (*application, error) {
	data, err := dataset.Load(cfg.Data.Routing)
	if err != nil {
		return nil, fmt.Errorf("load routing data: %w", err)
	}
	classifier, err := policy.New(data, cfg.Rules)
	if err != nil {
		return nil, fmt.Errorf("build classifier: %w", err)
	}
	learner := policy.NewLearningTable(cfg.Limits.LearnedIPEntries)
	sets := firewall.NewNftSetClient(cfg.Firewall.Table)
	domestic := dnsproxy.NewNetworkExchanger(cfg.DomesticDNS)
	hy2Client, err := hy2.New(cfg.HY2, hy2.NewBootstrapResolver(domestic), nil)
	if err != nil {
		return nil, fmt.Errorf("build HY2 transport: %w", err)
	}
	direct := transport.NewDirectStreamDialer()
	directPacket := transport.NewDirectPacketDialer()
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
		learningAdapter{table: learner, firewall: firewall.NewLearner(learner, sets, nil)},
		cfg.Limits.DNSCacheEntries,
		cfg.Limits.SniffTimeout.Value(),
	)
	app := &application{dns: dnsproxy.NewServer(cfg.Listen.DNS, resolver), sets: sets, dnsOnly: dnsOnly}
	if !dnsOnly {
		tcpProxy, err := landing.New(cfg.Landing, trustedRoute)
		if err != nil {
			return nil, fmt.Errorf("build landing transport: %w", err)
		}
		udpProxy := transport.NewFailOpenPacket(hy2Client, directPacket, controller, nil)
		app.tcp = &dataplane.TCPServer{ListenAddr: cfg.Listen.TCP, Classifier: classifier, Learned: learner, Direct: direct, Proxy: tcpProxy, Sniff: dataplaneSniff(cfg), MaxActive: cfg.HY2.MaxConcurrentDials}
		app.udp = &dataplane.UDPServer{ListenAddr: cfg.Listen.UDP, Classifier: classifier, Learned: learner, Direct: directPacket, Proxy: udpProxy, Sessions: dataplane.NewSessionTable(cfg.Limits.UDPSessions, cfg.Limits.UDPIdle.Value())}
	}
	return app, nil
}

func (application *application) Run(ctx context.Context) error {
	if application.dnsOnly {
		return application.dns.Run(ctx)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make(chan error, 3)
	dnsReady, tcpReady, udpReady := make(chan struct{}), make(chan struct{}), make(chan struct{})
	application.dns.Ready, application.tcp.Ready, application.udp.Ready = dnsReady, tcpReady, udpReady
	var group sync.WaitGroup
	for _, run := range []func(context.Context) error{application.dns.Run, application.tcp.Run, application.udp.Run} {
		group.Add(1)
		go func(f func(context.Context) error) { defer group.Done(); errs <- f(ctx) }(run)
	}
	go func() { group.Wait(); close(errs) }()
	for _, ready := range []<-chan struct{}{dnsReady, tcpReady, udpReady} {
		select {
		case <-ready:
		case err := <-errs:
			if err != nil {
				return err
			}
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	go application.heartbeat(ctx)
	for err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func (application *application) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		if application.sets.Heartbeat(ctx, 10*time.Second) != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func dataplaneSniff(cfg config.Config) sniff.Limits {
	return sniff.Limits{Bytes: cfg.Limits.SniffBytes, Timeout: cfg.Limits.SniffTimeout.Value()}
}

type learningAdapter struct {
	table    *policy.LearningTable
	firewall *firewall.Learner
}

func (adapter learningAdapter) Observe(_ context.Context, observation policy.Observation) error {
	if adapter.firewall != nil {
		return adapter.firewall.Observe(context.Background(), observation)
	}
	return adapter.table.Observe(observation)
}
