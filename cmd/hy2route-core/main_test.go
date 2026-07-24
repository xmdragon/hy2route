package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCheckValidatesConfig(t *testing.T) {
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
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check", "--config", path}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if got := stdout.String(); got != "hy2route-core configuration is valid\n" {
		t.Fatalf("stdout=%q", got)
	}
}

func TestRunCheckRedactsSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.json")
	if err := os.WriteFile(path, []byte(`{"hy2":{"auth":"TOP-SECRET"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"check", "--config", path}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "TOP-SECRET") {
		t.Fatalf("secret leaked: %s", stderr.String())
	}
}
