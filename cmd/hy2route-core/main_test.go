package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xmdragon/hy2route/internal/config"
	"github.com/xmdragon/hy2route/internal/dataset"
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
		"data":{"routing":"/routing.bin"},"log_level":"info","fail_open":true
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

func TestRunServeLoadsConfiguration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"serve", "--dns-only", "--config", "/does-not-exist"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "configuration invalid") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestNewApplicationBuildsDNSService(t *testing.T) {
	dataPath := filepath.Join(t.TempDir(), "routing.bin")
	file, err := os.Create(dataPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := dataset.Write(file, dataset.Data{}); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Listen:      config.ListenConfig{DNS: "127.0.0.1:0"},
		DomesticDNS: "127.0.0.1:53",
		TrustedDNS:  "127.0.0.1:53",
		Limits:      config.LimitsConfig{DNSCacheEntries: 64, LearnedIPEntries: 64},
		Data:        config.DataConfig{Routing: dataPath},
	}
	app, err := newApplication(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for app.dns.Addr() == "" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if app.dns.Addr() == "" {
		t.Fatal("application did not start DNS")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
