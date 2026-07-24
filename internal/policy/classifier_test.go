package policy

import (
	"net/netip"
	"testing"

	"github.com/xmdragon/hy2route/internal/config"
	"github.com/xmdragon/hy2route/internal/dataset"
)

func testClassifier(t *testing.T, domains []string, prefixes []string, rules []config.RuleConfig) *Classifier {
	t.Helper()
	data := dataset.Data{}
	for _, domain := range domains {
		data.Domains = append(data.Domains, dataset.Domain{Name: domain})
	}
	for _, prefix := range prefixes {
		data.Prefixes = append(data.Prefixes, netip.MustParsePrefix(prefix))
	}
	classifier, err := New(data, rules)
	if err != nil {
		t.Fatal(err)
	}
	return classifier
}

func assertDecision(t *testing.T, got Decision, action Action, source Source) {
	t.Helper()
	if got.Action != action || got.Source != source {
		t.Fatalf("decision = %#v, want action=%v source=%s", got, action, source)
	}
}

func TestClassifierPrecedence(t *testing.T) {
	classifier := testClassifier(t,
		[]string{"wechat.com"},
		[]string{"120.232.0.0/12"},
		[]config.RuleConfig{
			{Action: "proxy", Type: "domain", Value: "pay.wechat.com"},
			{Action: "proxy", Type: "ip", Value: "120.233.109.151/32"},
		},
	)
	assertDecision(t, classifier.Domain("img.wechat.com"), Direct, SourceChinaDomain)
	assertDecision(t, classifier.Domain("pay.wechat.com"), Proxy, SourceExplicitDomain)
	assertDecision(t, classifier.IP(netip.MustParseAddr("120.233.109.151")), Proxy, SourceExplicitIP)
	assertDecision(t, classifier.IP(netip.MustParseAddr("120.233.109.196")), Direct, SourceChinaIP)
	assertDecision(t, classifier.Domain("notwechat.com"), Proxy, SourceDefault)
}

func TestClassifierRespectsExactDomainAndLongestIPv4Prefix(t *testing.T) {
	data := dataset.Data{
		Domains: []dataset.Domain{{Name: "wx.qq.com", Exact: true}},
		Prefixes: []netip.Prefix{
			netip.MustParsePrefix("203.0.113.0/24"),
			netip.MustParsePrefix("203.0.113.128/25"),
		},
	}
	classifier, err := New(data, []config.RuleConfig{{Action: "proxy", Type: "ip", Value: "203.0.113.128/25"}})
	if err != nil {
		t.Fatal(err)
	}
	assertDecision(t, classifier.Domain("wx.qq.com."), Direct, SourceChinaDomain)
	assertDecision(t, classifier.Domain("api.wx.qq.com"), Proxy, SourceDefault)
	assertDecision(t, classifier.IP(netip.MustParseAddr("203.0.113.129")), Proxy, SourceExplicitIP)
	assertDecision(t, classifier.IP(netip.MustParseAddr("203.0.113.1")), Direct, SourceChinaIP)
}

func TestNormalizeDomainRejectsEmptyLabels(t *testing.T) {
	got, err := NormalizeDomain("WeChat.COM.")
	if err != nil || got != "wechat.com" {
		t.Fatalf("NormalizeDomain = %q, %v", got, err)
	}
	if _, err := NormalizeDomain("bad..example"); err == nil {
		t.Fatal("NormalizeDomain accepted an empty label")
	}
}
