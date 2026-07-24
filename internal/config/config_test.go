package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		Listen: ListenConfig{
			DNS: "127.0.0.1:1053",
			TCP: "127.0.0.1:12345",
			UDP: "127.0.0.1:12346",
		},
		DomesticDNS: "223.5.5.5:53",
		TrustedDNS:  "1.1.1.1:53",
		HY2: HY2Config{
			Server:    "hy2.example:443",
			Auth:      "test-auth",
			SNI:       "hy2.example",
			MaxIdle:   Duration(30 * time.Second),
			KeepAlive: Duration(10 * time.Second),
		},
		Landing: LandingConfig{Type: "direct"},
		Limits: LimitsConfig{
			DNSCacheEntries:  256,
			LearnedIPEntries: 512,
			UDPSessions:      256,
			UDPIdle:          Duration(60 * time.Second),
			SniffBytes:       4096,
			SniffTimeout:     Duration(250 * time.Millisecond),
		},
		Health: HealthConfig{
			FailureThreshold: 2,
			SuccessThreshold: 2,
			Cooldown:         Duration(30 * time.Second),
		},
		Firewall: FirewallConfig{
			Table:        "hy2route",
			LANInterface: "br-lan",
			Mark:         102,
			RouteTable:   100,
		},
		Data: DataConfig{
			Domains: "/usr/share/hy2route/china-domains.bin",
			IPv4:    "/usr/share/hy2route/china4.bin",
		},
		LogLevel: "info",
		FailOpen: true,
	}
}

func TestValidateCompleteConfig(t *testing.T) {
	cfg := validConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.HY2.InitialStreamWindow != 1<<20 || cfg.HY2.MaxStreamWindow != 4<<20 {
		t.Fatalf("unexpected stream defaults: %#v", cfg.HY2)
	}
	if cfg.HY2.InitialConnectionWindow != 4<<20 || cfg.HY2.MaxConnectionWindow != 8<<20 {
		t.Fatalf("unexpected connection defaults: %#v", cfg.HY2)
	}
}

func TestValidateRejectsIPv6AndPortCollisions(t *testing.T) {
	cfg := validConfig()
	cfg.DomesticDNS = "[2001:db8::1]:53"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "domestic_dns") {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg = validConfig()
	cfg.Listen.DNS = cfg.Listen.TCP
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "listen ports") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeRejectsUnknownAndNumericDuration(t *testing.T) {
	raw := []byte(`{
		"listen":{"dns":"127.0.0.1:1053","tcp":"127.0.0.1:12345","udp":"127.0.0.1:12346"},
		"domestic_dns":"223.5.5.5:53","trusted_dns":"1.1.1.1:53",
		"hy2":{"server":"hy2.example:443","auth":"test-auth","sni":"hy2.example","max_idle":30,"keep_alive":"10s"},
		"landing":{"type":"direct"},
		"limits":{"dns_cache_entries":256,"learned_ip_entries":512,"udp_sessions":256,"udp_idle":"60s","sniff_bytes":4096,"sniff_timeout":"250ms"},
		"health":{"failure_threshold":2,"success_threshold":2,"cooldown":"30s"},
		"firewall":{"table":"hy2route","lan_interface":"br-lan","mark":102,"route_table":100},
		"data":{"domains":"/domains","ipv4":"/ipv4"},"unknown":true
	}`)
	_, err := Decode(raw)
	if err == nil || (!strings.Contains(err.Error(), "unknown") && !strings.Contains(err.Error(), "duration")) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestErrorsNeverContainCredentials(t *testing.T) {
	raw := []byte(`{"hy2":{"auth":"TOP-SECRET"}}`)
	_, err := Decode(raw)
	if err == nil || strings.Contains(err.Error(), "TOP-SECRET") {
		t.Fatalf("secret leaked in error: %v", err)
	}
}

func TestLoadReadsAndValidatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.json")
	raw := []byte(`{
		"listen":{"dns":"127.0.0.1:1053","tcp":"127.0.0.1:12345","udp":"127.0.0.1:12346"},
		"domestic_dns":"223.5.5.5:53","trusted_dns":"1.1.1.1:53",
		"hy2":{"server":"hy2.example:443","auth":"test-auth","sni":"hy2.example","max_idle":"30s","keep_alive":"10s"},
		"landing":{"type":"direct"},
		"limits":{"dns_cache_entries":256,"learned_ip_entries":512,"udp_sessions":256,"udp_idle":"60s","sniff_bytes":4096,"sniff_timeout":"250ms"},
		"health":{"failure_threshold":2,"success_threshold":2,"cooldown":"30s"},
		"firewall":{"table":"hy2route","lan_interface":"br-lan","mark":102,"route_table":100},
		"data":{"domains":"/domains","ipv4":"/ipv4"},"log_level":"info","fail_open":true
	}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen.DNS != "127.0.0.1:1053" || cfg.HY2.MaxStreamWindow != 4<<20 {
		t.Fatalf("loaded config = %#v", cfg)
	}
}
