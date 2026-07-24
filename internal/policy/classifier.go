package policy

import (
	"fmt"
	"net/netip"

	"github.com/xmdragon/hy2route/internal/config"
	"github.com/xmdragon/hy2route/internal/dataset"
)

type Classifier struct {
	explicitDomains domainTree
	chinaDomains    domainTree
	explicitIPv4    ipv4Tree
	chinaIPv4       ipv4Tree
}

func New(data dataset.Data, rules []config.RuleConfig) (*Classifier, error) {
	classifier := &Classifier{}
	for _, domain := range data.Domains {
		normalized, err := NormalizeDomain(domain.Name)
		if err != nil {
			return nil, fmt.Errorf("china domain: %w", err)
		}
		classifier.chinaDomains.add(normalized, domain.Exact, Direct)
	}
	for _, prefix := range data.Prefixes {
		if !prefix.IsValid() || !prefix.Addr().Is4() || prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("china prefix %q is not IPv4", prefix)
		}
		classifier.chinaIPv4.add(prefix, Direct)
	}
	for _, rule := range rules {
		action, err := parseAction(rule.Action)
		if err != nil {
			return nil, err
		}
		switch rule.Type {
		case "domain":
			normalized, err := NormalizeDomain(rule.Value)
			if err != nil {
				return nil, fmt.Errorf("rule domain: %w", err)
			}
			classifier.explicitDomains.add(normalized, false, action)
		case "ip":
			prefix, err := netip.ParsePrefix(rule.Value)
			if err != nil || !prefix.Addr().Is4() || prefix.Addr().Is4In6() {
				return nil, fmt.Errorf("rule IP %q is not IPv4 CIDR", rule.Value)
			}
			classifier.explicitIPv4.add(prefix, action)
		default:
			return nil, fmt.Errorf("rule type %q is invalid", rule.Type)
		}
	}
	return classifier, nil
}

func (classifier *Classifier) Domain(domain string) Decision {
	normalized, err := NormalizeDomain(domain)
	if err != nil {
		return Decision{Action: Proxy, Source: SourceDefault}
	}
	if action, ok := classifier.explicitDomains.match(normalized); ok {
		return Decision{Action: action, Source: SourceExplicitDomain, Domain: normalized}
	}
	if action, ok := classifier.chinaDomains.match(normalized); ok {
		return Decision{Action: action, Source: SourceChinaDomain, Domain: normalized}
	}
	return Decision{Action: Proxy, Source: SourceDefault, Domain: normalized}
}

func (classifier *Classifier) IP(addr netip.Addr) Decision {
	if action, ok := classifier.explicitIPv4.match(addr); ok {
		return Decision{Action: action, Source: SourceExplicitIP}
	}
	if action, ok := classifier.chinaIPv4.match(addr); ok {
		return Decision{Action: action, Source: SourceChinaIP}
	}
	return Decision{Action: Proxy, Source: SourceDefault}
}

func parseAction(value string) (Action, error) {
	switch value {
	case "direct":
		return Direct, nil
	case "proxy":
		return Proxy, nil
	default:
		return Unknown, fmt.Errorf("rule action %q is invalid", value)
	}
}
