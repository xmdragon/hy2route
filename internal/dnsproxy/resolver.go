package dnsproxy

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/miekg/dns"
	"github.com/xmdragon/hy2route/internal/policy"
)

type Resolver struct {
	classifier *policy.Classifier
	domestic   Exchanger
	trusted    Exchanger
	learner    Learner
	cache      *Cache
	now        func() time.Time
	timeout    time.Duration
}

func NewResolver(classifier *policy.Classifier, domestic, trusted Exchanger, learner Learner, cacheEntries int, timeout time.Duration) *Resolver {
	if timeout <= 0 {
		timeout = time.Second
	}
	return &Resolver{
		classifier: classifier,
		domestic:   domestic,
		trusted:    trusted,
		learner:    learner,
		cache:      NewCache(cacheEntries),
		now:        time.Now,
		timeout:    timeout,
	}
}

func (resolver *Resolver) Resolve(ctx context.Context, request *dns.Msg) (*dns.Msg, error) {
	if request == nil || len(request.Question) != 1 {
		return errorReply(request, dns.RcodeFormatError), nil
	}
	question := request.Question[0]
	if question.Qclass != dns.ClassINET {
		return errorReply(request, dns.RcodeFormatError), nil
	}
	if question.Qtype != dns.TypeA && question.Qtype != dns.TypeAAAA {
		return errorReply(request, dns.RcodeNotImplemented), nil
	}
	if question.Qtype == dns.TypeAAAA {
		return errorReply(request, dns.RcodeSuccess), nil
	}
	if response, ok := resolver.cache.Get(question.Name, question.Qtype, resolver.now()); ok {
		response.Id = request.Id
		response.Question = append(response.Question[:0], request.Question...)
		return response, nil
	}
	if resolver.classifier == nil || resolver.domestic == nil || resolver.trusted == nil {
		return nil, errors.New("DNS resolver is not configured")
	}
	decision := resolver.classifier.Domain(question.Name)
	var (
		response *dns.Msg
		err      error
	)
	switch decision.Source {
	case policy.SourceDefault:
		response, decision.Action, err = resolver.resolveUnknown(ctx, request)
	case policy.SourceExplicitDomain, policy.SourceChinaDomain:
		if decision.Action == policy.Direct {
			response, err = resolver.exchange(ctx, resolver.domestic, request)
		} else {
			response, err = resolver.exchange(ctx, resolver.trusted, request)
		}
	default:
		err = errors.New("unsupported DNS decision")
	}
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("DNS upstream returned no response")
	}
	response.Id = request.Id
	response.Question = append(response.Question[:0], request.Question...)
	resolver.learn(ctx, decision.Domain, decision.Action, response)
	resolver.cacheResponse(question, response)
	return response.Copy(), nil
}

func (resolver *Resolver) resolveUnknown(parent context.Context, request *dns.Msg) (*dns.Msg, policy.Action, error) {
	ctx, cancel := context.WithTimeout(parent, resolver.timeout)
	defer cancel()
	type result struct {
		response *dns.Msg
		err      error
	}
	domesticResult := make(chan result, 1)
	trustedResult := make(chan result, 1)
	go func() {
		response, err := resolver.exchange(ctx, resolver.domestic, request)
		domesticResult <- result{response: response, err: err}
	}()
	go func() {
		response, err := resolver.exchange(ctx, resolver.trusted, request)
		trustedResult <- result{response: response, err: err}
	}()
	domestic := <-domesticResult
	trusted := <-trustedResult
	if domestic.err == nil && domesticAnswerIsChina(resolver.classifier, domestic.response) {
		return domestic.response, policy.Direct, nil
	}
	if trusted.err == nil && trusted.response != nil {
		return trusted.response, policy.Proxy, nil
	}
	if trusted.err != nil {
		return nil, policy.Unknown, trusted.err
	}
	return nil, policy.Unknown, errors.New("neither DNS upstream returned a usable answer")
}

func (resolver *Resolver) exchange(parent context.Context, upstream Exchanger, request *dns.Msg) (*dns.Msg, error) {
	ctx, cancel := context.WithTimeout(parent, resolver.timeout)
	defer cancel()
	response, err := upstream.Exchange(ctx, request.Copy())
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("DNS upstream returned no response")
	}
	return response, nil
}

func domesticAnswerIsChina(classifier *policy.Classifier, response *dns.Msg) bool {
	addresses, _ := responseARecords(response)
	if len(addresses) == 0 {
		return false
	}
	for _, address := range addresses {
		if classifier.IP(address).Source != policy.SourceChinaIP {
			return false
		}
	}
	return true
}

func (resolver *Resolver) learn(ctx context.Context, domain string, action policy.Action, response *dns.Msg) {
	if resolver.learner == nil {
		return
	}
	addresses, ttl := responseARecords(response)
	if len(addresses) == 0 || action == policy.Unknown {
		return
	}
	ttl = clampDuration(ttl, 30*time.Second, 24*time.Hour)
	_ = resolver.learner.Observe(ctx, policy.Observation{
		Domain: domain, Action: action, IPs: addresses, Expires: resolver.now().Add(ttl),
	})
}

func (resolver *Resolver) cacheResponse(question dns.Question, response *dns.Msg) {
	_, ttl := responseARecords(response)
	if ttl <= 0 {
		ttl = 60 * time.Second
	} else {
		ttl = clampDuration(ttl, 30*time.Second, 24*time.Hour)
	}
	resolver.cache.Put(question.Name, question.Qtype, response, resolver.now().Add(ttl))
}

func responseARecords(response *dns.Msg) ([]netip.Addr, time.Duration) {
	if response == nil {
		return nil, 0
	}
	addresses := make([]netip.Addr, 0)
	var minimum uint32
	for _, answer := range response.Answer {
		record, ok := answer.(*dns.A)
		if !ok {
			continue
		}
		addr, ok := netip.AddrFromSlice(record.A)
		if !ok || !addr.Is4() {
			continue
		}
		addresses = append(addresses, addr)
		if minimum == 0 || record.Hdr.Ttl < minimum {
			minimum = record.Hdr.Ttl
		}
	}
	return addresses, time.Duration(minimum) * time.Second
}

func errorReply(request *dns.Msg, rcode int) *dns.Msg {
	response := new(dns.Msg)
	if request != nil {
		response.SetReply(request)
	}
	response.Rcode = rcode
	return response
}

func clampDuration(value, minimum, maximum time.Duration) time.Duration {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
