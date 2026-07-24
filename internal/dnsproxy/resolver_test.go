package dnsproxy

import (
	"context"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/xmdragon/hy2route/internal/config"
	"github.com/xmdragon/hy2route/internal/dataset"
	"github.com/xmdragon/hy2route/internal/policy"
)

type fakeExchanger struct {
	mu    sync.Mutex
	calls int
	reply *dns.Msg
	err   error
}

func (exchanger *fakeExchanger) Exchange(context.Context, *dns.Msg) (*dns.Msg, error) {
	exchanger.mu.Lock()
	defer exchanger.mu.Unlock()
	exchanger.calls++
	if exchanger.reply == nil {
		return nil, exchanger.err
	}
	return exchanger.reply.Copy(), exchanger.err
}

type recordingLearner struct {
	mu   sync.Mutex
	last policy.Observation
}

func (learner *recordingLearner) Observe(_ context.Context, observation policy.Observation) error {
	learner.mu.Lock()
	defer learner.mu.Unlock()
	learner.last = observation
	return nil
}

func resolverFixture(t *testing.T) (*Resolver, *fakeExchanger, *fakeExchanger, *recordingLearner) {
	t.Helper()
	classifier, err := policy.New(dataset.Data{
		Domains:  []dataset.Domain{{Name: "wechat.com"}},
		Prefixes: []netip.Prefix{netip.MustParsePrefix("120.232.0.0/12")},
	}, []config.RuleConfig{{Action: "proxy", Type: "domain", Value: "proxy.example"}})
	if err != nil {
		t.Fatal(err)
	}
	domestic, trusted, learner := &fakeExchanger{}, &fakeExchanger{}, &recordingLearner{}
	return NewResolver(classifier, domestic, trusted, learner, 16, time.Second), domestic, trusted, learner
}

func newTestResolver(t *testing.T) *Resolver {
	t.Helper()
	resolver, _, _, _ := resolverFixture(t)
	return resolver
}

func aQuestion(name string) *dns.Msg {
	request := new(dns.Msg)
	request.SetQuestion(name, dns.TypeA)
	return request
}

func aReply(name string, addresses ...string) *dns.Msg {
	response := new(dns.Msg)
	response.SetReply(aQuestion(name))
	for _, address := range addresses {
		response.Answer = append(response.Answer, &dns.A{
			Hdr: dns.RR_Header{Name: name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120},
			A:   netip.MustParseAddr(address).AsSlice(),
		})
	}
	return response
}

func allA(response *dns.Msg) []string {
	addresses := make([]string, 0)
	for _, answer := range response.Answer {
		if record, ok := answer.(*dns.A); ok {
			addresses = append(addresses, record.A.String())
		}
	}
	return addresses
}

func TestAAAAIsNODATAWithoutUpstream(t *testing.T) {
	resolver := newTestResolver(t)
	request := new(dns.Msg)
	request.SetQuestion("wechat.com.", dns.TypeAAAA)
	response, err := resolver.Resolve(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Rcode != dns.RcodeSuccess || len(response.Answer) != 0 {
		t.Fatalf("response = %#v", response)
	}
	if resolver.domestic.(*fakeExchanger).calls+resolver.trusted.(*fakeExchanger).calls != 0 {
		t.Fatal("AAAA reached upstream")
	}
}

func TestUnknownUsesDomesticOnlyWhenEveryAIsChina(t *testing.T) {
	resolver, domestic, trusted, learner := resolverFixture(t)
	domestic.reply = aReply("unknown.example.", "120.233.109.151", "120.233.109.196")
	trusted.reply = aReply("unknown.example.", "203.0.113.8")
	response, err := resolver.Resolve(context.Background(), aQuestion("unknown.example."))
	if err != nil {
		t.Fatal(err)
	}
	if got := allA(response); !slices.Equal(got, []string{"120.233.109.151", "120.233.109.196"}) {
		t.Fatalf("domestic answer = %v", got)
	}
	if learner.last.Action != policy.Direct {
		t.Fatalf("observation = %#v", learner.last)
	}
	if domestic.calls != 1 || trusted.calls != 1 {
		t.Fatalf("calls domestic=%d trusted=%d", domestic.calls, trusted.calls)
	}

	resolver, domestic, trusted, learner = resolverFixture(t)
	domestic.reply = aReply("mixed.example.", "120.233.109.151", "203.0.113.9")
	trusted.reply = aReply("mixed.example.", "203.0.113.8")
	response, err = resolver.Resolve(context.Background(), aQuestion("mixed.example."))
	if err != nil {
		t.Fatal(err)
	}
	if got := allA(response); !slices.Equal(got, []string{"203.0.113.8"}) {
		t.Fatalf("trusted answer = %v", got)
	}
	if learner.last.Action != policy.Proxy {
		t.Fatalf("observation = %#v", learner.last)
	}
}

func TestChinaDomainUsesDomesticEvenForNonChinaAddress(t *testing.T) {
	resolver, domestic, trusted, learner := resolverFixture(t)
	domestic.reply = aReply("wechat.com.", "203.0.113.8")
	response, err := resolver.Resolve(context.Background(), aQuestion("wechat.com."))
	if err != nil {
		t.Fatal(err)
	}
	if got := allA(response); !slices.Equal(got, []string{"203.0.113.8"}) {
		t.Fatalf("answer = %v", got)
	}
	if trusted.calls != 0 {
		t.Fatalf("trusted calls = %d", trusted.calls)
	}
	if learner.last.Action != policy.Direct || domestic.calls != 1 {
		t.Fatalf("learner=%#v domestic calls=%d", learner.last, domestic.calls)
	}
}

func TestCacheReturnsIndependentResponses(t *testing.T) {
	resolver, domestic, _, _ := resolverFixture(t)
	domestic.reply = aReply("wechat.com.", "120.233.109.151")
	first, err := resolver.Resolve(context.Background(), aQuestion("wechat.com."))
	if err != nil {
		t.Fatal(err)
	}
	first.Answer = nil
	second, err := resolver.Resolve(context.Background(), aQuestion("wechat.com."))
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Answer) != 1 || domestic.calls != 1 {
		t.Fatalf("response=%#v calls=%d", second, domestic.calls)
	}
}

func TestResolverPreservesClientTransactionID(t *testing.T) {
	resolver, domestic, _, _ := resolverFixture(t)
	domestic.reply = aReply("wechat.com.", "120.233.109.151")
	request := aQuestion("wechat.com.")
	request.Id = 4242
	response, err := resolver.Resolve(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Id != request.Id {
		t.Fatalf("response ID = %d, want %d", response.Id, request.Id)
	}
}
