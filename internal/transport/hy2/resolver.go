package hy2

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
	"github.com/xmdragon/hy2route/internal/dnsproxy"
)

type bootstrapCacheEntry struct {
	ip      netip.Addr
	expires time.Time
}

type DomesticBootstrapResolver struct {
	exchanger dnsproxy.Exchanger
	now       func() time.Time

	mu    sync.Mutex
	cache map[string]bootstrapCacheEntry
}

func NewBootstrapResolver(exchanger dnsproxy.Exchanger) *DomesticBootstrapResolver {
	return &DomesticBootstrapResolver{exchanger: exchanger, now: time.Now, cache: make(map[string]bootstrapCacheEntry)}
}

func (resolver *DomesticBootstrapResolver) LookupIPv4(ctx context.Context, host string) (netip.Addr, error) {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	now := resolver.now()
	resolver.mu.Lock()
	if cached, ok := resolver.cache[host]; ok && cached.expires.After(now) {
		resolver.mu.Unlock()
		return cached.ip, nil
	}
	resolver.mu.Unlock()
	request := new(dns.Msg)
	request.SetQuestion(dns.Fqdn(host), dns.TypeA)
	response, err := resolver.exchanger.Exchange(ctx, request)
	if err != nil {
		return netip.Addr{}, err
	}
	if response == nil {
		return netip.Addr{}, errors.New("bootstrap DNS returned no response")
	}
	for _, answer := range response.Answer {
		record, ok := answer.(*dns.A)
		if !ok {
			continue
		}
		ip, ok := netip.AddrFromSlice(record.A)
		if !ok || !ip.Is4() || ip.Is4In6() {
			continue
		}
		ttl := time.Duration(record.Hdr.Ttl) * time.Second
		if ttl < 5*time.Minute {
			ttl = 5 * time.Minute
		}
		if ttl > time.Hour {
			ttl = time.Hour
		}
		resolver.mu.Lock()
		resolver.cache[host] = bootstrapCacheEntry{ip: ip, expires: resolver.now().Add(ttl)}
		resolver.mu.Unlock()
		return ip, nil
	}
	return netip.Addr{}, errors.New("bootstrap DNS returned no IPv4 answer")
}
