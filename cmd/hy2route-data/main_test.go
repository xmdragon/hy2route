package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xmdragon/hy2route/internal/dataset"
)

func TestRunCompilesNormalizedDataset(t *testing.T) {
	dir := t.TempDir()
	domains := filepath.Join(dir, "domains.txt")
	ipv4 := filepath.Join(dir, "ipv4.txt")
	output := filepath.Join(dir, "data.bin")
	writeTestFile(t, domains, "# generated\nWeChat.COM.\nwx.qq.com\n")
	writeTestFile(t, ipv4, "# generated\n120.232.0.0/12\n")
	if err := run([]string{"--domains", domains, "--ipv4", ipv4, "--output", output}); err != nil {
		t.Fatal(err)
	}
	data, err := dataset.Load(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Domains) != 2 || data.Domains[0].Name != "wechat.com" || len(data.Prefixes) != 1 {
		t.Fatalf("compiled data = %#v", data)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
