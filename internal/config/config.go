package config

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

const (
	defaultInitialStreamWindow     = 1 << 20
	defaultMaxStreamWindow         = 4 << 20
	defaultInitialConnectionWindow = 4 << 20
	defaultMaxConnectionWindow     = 8 << 20
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

type Config struct {
	Listen      ListenConfig   `json:"listen"`
	DomesticDNS string         `json:"domestic_dns"`
	TrustedDNS  string         `json:"trusted_dns"`
	HY2         HY2Config      `json:"hy2"`
	Landing     LandingConfig  `json:"landing"`
	Limits      LimitsConfig   `json:"limits"`
	Health      HealthConfig   `json:"health"`
	Firewall    FirewallConfig `json:"firewall"`
	Rules       []RuleConfig   `json:"rules"`
	Data        DataConfig     `json:"data"`
	LogLevel    string         `json:"log_level"`
	FailOpen    bool           `json:"fail_open"`
}

type ListenConfig struct {
	DNS string `json:"dns"`
	TCP string `json:"tcp"`
	UDP string `json:"udp"`
}

type HY2Config struct {
	Server                  string   `json:"server"`
	Auth                    string   `json:"auth"`
	SNI                     string   `json:"sni"`
	Insecure                bool     `json:"insecure"`
	PinnedCertSHA256        string   `json:"pinned_cert_sha256"`
	MaxIdle                 Duration `json:"max_idle"`
	KeepAlive               Duration `json:"keep_alive"`
	InitialStreamWindow     uint64   `json:"initial_stream_window"`
	MaxStreamWindow         uint64   `json:"max_stream_window"`
	InitialConnectionWindow uint64   `json:"initial_connection_window"`
	MaxConnectionWindow     uint64   `json:"max_connection_window"`
	MaxConcurrentDials      int      `json:"max_concurrent_dials"`
}

type LandingConfig struct {
	Type     string `json:"type"`
	Server   string `json:"server"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type LimitsConfig struct {
	DNSCacheEntries  int      `json:"dns_cache_entries"`
	LearnedIPEntries int      `json:"learned_ip_entries"`
	UDPSessions      int      `json:"udp_sessions"`
	UDPIdle          Duration `json:"udp_idle"`
	SniffBytes       int      `json:"sniff_bytes"`
	SniffTimeout     Duration `json:"sniff_timeout"`
}

type HealthConfig struct {
	FailureThreshold int      `json:"failure_threshold"`
	SuccessThreshold int      `json:"success_threshold"`
	Cooldown         Duration `json:"cooldown"`
	ProbeInterval    Duration `json:"probe_interval"`
}

type FirewallConfig struct {
	Table        string `json:"table"`
	LANInterface string `json:"lan_interface"`
	Mark         uint32 `json:"mark"`
	RouteTable   uint32 `json:"route_table"`
	CanarySource string `json:"canary_source,omitempty"`
}

type RuleConfig struct {
	Action string `json:"action"`
	Type   string `json:"type"`
	Value  string `json:"value"`
}

type DataConfig struct {
	Routing string `json:"routing"`
}

type Duration time.Duration

func (d Duration) Value() time.Duration { return time.Duration(d) }

func (d *Duration) UnmarshalJSON(raw []byte) error {
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return errors.New("duration must be a JSON string")
	}
	parsed, err := time.ParseDuration(text)
	if err != nil {
		return fmt.Errorf("invalid duration")
	}
	*d = Duration(parsed)
	return nil
}

func (c *Config) Validate() error {
	if err := validateListen(c.Listen); err != nil {
		return err
	}
	if err := validateIPv4Endpoint("domestic_dns", c.DomesticDNS); err != nil {
		return err
	}
	if err := validateIPv4Endpoint("trusted_dns", c.TrustedDNS); err != nil {
		return err
	}
	if err := c.validateHY2(); err != nil {
		return err
	}
	if err := c.validateLanding(); err != nil {
		return err
	}
	if err := c.validateLimits(); err != nil {
		return err
	}
	if err := c.validateHealth(); err != nil {
		return err
	}
	if err := c.validateFirewall(); err != nil {
		return err
	}
	if err := validateRules(c.Rules); err != nil {
		return err
	}
	if strings.TrimSpace(c.Data.Routing) == "" {
		return errors.New("data routing path is required")
	}
	if c.LogLevel != "" && c.LogLevel != "debug" && c.LogLevel != "info" && c.LogLevel != "warn" && c.LogLevel != "error" {
		return errors.New("log_level must be debug, info, warn, or error")
	}
	return nil
}

func validateListen(listen ListenConfig) error {
	ports := make(map[uint16]struct{}, 3)
	for name, endpoint := range map[string]string{"listen.dns": listen.DNS, "listen.tcp": listen.TCP, "listen.udp": listen.UDP} {
		addr, port, err := parseIPv4Endpoint(endpoint)
		if err != nil || !addr.Is4() || port == 0 {
			return fmt.Errorf("%s must be an IPv4 address and port", name)
		}
		ports[port] = struct{}{}
	}
	if len(ports) != 3 {
		return errors.New("listen ports must be distinct")
	}
	return nil
}

func validateIPv4Endpoint(name, endpoint string) error {
	if _, _, err := parseIPv4Endpoint(endpoint); err != nil {
		return fmt.Errorf("%s must be an IPv4 address and port", name)
	}
	return nil
}

func parseIPv4Endpoint(endpoint string) (netip.Addr, uint16, error) {
	host, service, err := net.SplitHostPort(endpoint)
	if err != nil {
		return netip.Addr{}, 0, err
	}
	addr, err := netip.ParseAddr(host)
	if err != nil || !addr.Is4() || addr.Is4In6() {
		return netip.Addr{}, 0, errors.New("not IPv4")
	}
	port, err := net.LookupPort("udp", service)
	if err != nil || port < 1 || port > 65535 {
		return netip.Addr{}, 0, errors.New("invalid port")
	}
	return addr, uint16(port), nil
}

func (c *Config) validateHY2() error {
	if err := validateHostPort("hy2.server", c.HY2.Server); err != nil {
		return err
	}
	if strings.TrimSpace(c.HY2.Auth) == "" {
		return errors.New("hy2 auth is required")
	}
	if !validDNSName(c.HY2.SNI) {
		return errors.New("hy2 sni must be a DNS name")
	}
	if c.HY2.PinnedCertSHA256 != "" {
		if len(c.HY2.PinnedCertSHA256) != 64 {
			return errors.New("hy2 pinned_cert_sha256 must be 64 hex characters")
		}
		if _, err := hex.DecodeString(c.HY2.PinnedCertSHA256); err != nil {
			return errors.New("hy2 pinned_cert_sha256 must be hexadecimal")
		}
	}
	if !between(c.HY2.MaxIdle.Value(), 4*time.Second, 120*time.Second) {
		return errors.New("hy2 max_idle must be between 4s and 120s")
	}
	if !between(c.HY2.KeepAlive.Value(), 2*time.Second, 60*time.Second) {
		return errors.New("hy2 keep_alive must be between 2s and 60s")
	}
	setWindowDefaults(&c.HY2)
	if c.HY2.MaxConcurrentDials == 0 {
		c.HY2.MaxConcurrentDials = 32
	}
	if !betweenInt(c.HY2.MaxConcurrentDials, 1, 256) {
		return errors.New("hy2 max_concurrent_dials must be between 1 and 256")
	}
	if c.HY2.InitialStreamWindow == 0 || c.HY2.MaxStreamWindow == 0 || c.HY2.InitialConnectionWindow == 0 || c.HY2.MaxConnectionWindow == 0 ||
		c.HY2.InitialStreamWindow > c.HY2.MaxStreamWindow || c.HY2.InitialConnectionWindow > c.HY2.MaxConnectionWindow {
		return errors.New("hy2 windows are invalid")
	}
	return nil
}

func setWindowDefaults(h *HY2Config) {
	if h.InitialStreamWindow == 0 {
		h.InitialStreamWindow = defaultInitialStreamWindow
	}
	if h.MaxStreamWindow == 0 {
		h.MaxStreamWindow = defaultMaxStreamWindow
	}
	if h.InitialConnectionWindow == 0 {
		h.InitialConnectionWindow = defaultInitialConnectionWindow
	}
	if h.MaxConnectionWindow == 0 {
		h.MaxConnectionWindow = defaultMaxConnectionWindow
	}
}

func (c Config) validateLanding() error {
	switch c.Landing.Type {
	case "direct":
		if c.Landing.Server != "" || c.Landing.Username != "" || c.Landing.Password != "" {
			return errors.New("landing credentials are allowed only for http or socks5")
		}
	case "http", "socks5":
		if err := validateHostPort("landing.server", c.Landing.Server); err != nil {
			return err
		}
	default:
		return errors.New("landing type must be direct, http, or socks5")
	}
	return nil
}

func (c Config) validateLimits() error {
	if !betweenInt(c.Limits.DNSCacheEntries, 64, 65536) || !betweenInt(c.Limits.LearnedIPEntries, 64, 131072) || !betweenInt(c.Limits.UDPSessions, 64, 65536) {
		return errors.New("cache and session limits are out of range")
	}
	if !between(c.Limits.UDPIdle.Value(), 2*time.Second, 600*time.Second) || !between(c.Limits.SniffTimeout.Value(), 10*time.Millisecond, 2*time.Second) || !betweenInt(c.Limits.SniffBytes, 1024, 16384) {
		return errors.New("udp or sniff limits are out of range")
	}
	return nil
}

func (c *Config) validateHealth() error {
	if c.Health.ProbeInterval == 0 {
		c.Health.ProbeInterval = Duration(10 * time.Second)
	}
	if !betweenInt(c.Health.FailureThreshold, 1, 10) || !betweenInt(c.Health.SuccessThreshold, 1, 10) || !between(c.Health.Cooldown.Value(), 5*time.Second, 15*time.Minute) || !between(c.Health.ProbeInterval.Value(), time.Second, 5*time.Minute) {
		return errors.New("health settings are out of range")
	}
	return nil
}

func (c Config) validateFirewall() error {
	if !validName(c.Firewall.Table) || !validName(c.Firewall.LANInterface) {
		return errors.New("firewall table and lan_interface must contain only [A-Za-z0-9_.-]")
	}
	if c.Firewall.Mark == 0 || c.Firewall.RouteTable == 0 {
		return errors.New("firewall mark and route_table must be non-zero")
	}
	if c.Firewall.CanarySource == "" {
		return nil
	}
	addr, err := netip.ParseAddr(c.Firewall.CanarySource)
	if err != nil || !addr.Is4() || addr.Is4In6() {
		return errors.New("firewall canary_source must be an IPv4 host address")
	}
	return nil
}

func validateRules(rules []RuleConfig) error {
	for _, rule := range rules {
		if rule.Action != "direct" && rule.Action != "proxy" {
			return errors.New("rule action must be direct or proxy")
		}
		switch rule.Type {
		case "domain":
			if !validDNSName(rule.Value) {
				return errors.New("rule domain is invalid")
			}
		case "ip":
			prefix, err := netip.ParsePrefix(rule.Value)
			if err != nil || !prefix.Addr().Is4() || prefix.Addr().Is4In6() {
				return errors.New("rule IP must be an IPv4 CIDR")
			}
		default:
			return errors.New("rule type must be domain or ip")
		}
	}
	return nil
}

func validateHostPort(name, endpoint string) error {
	host, service, err := net.SplitHostPort(endpoint)
	if err != nil || host == "" || strings.Contains(host, ":") {
		return fmt.Errorf("%s must be an IPv4 or DNS host and port", name)
	}
	if _, err := net.LookupPort("tcp", service); err != nil {
		return fmt.Errorf("%s must have a valid port", name)
	}
	return nil
}

func validName(value string) bool {
	return value != "" && namePattern.MatchString(value)
}

func validDNSName(name string) bool {
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
				return false
			}
		}
	}
	return true
}

func between(value, min, max time.Duration) bool { return value >= min && value <= max }

func betweenInt(value, min, max int) bool { return value >= min && value <= max }
