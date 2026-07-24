package control

import (
	"bufio"
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestControlSocketIs0600AndNeverReturnsSecrets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.sock")
	server, err := Listen(path, func() Snapshot {
		return Snapshot{Mode: "proxy", HY2Connected: true, DNSCache: 12, LearnedIPs: 8, UDPSessions: 2, ActiveTCP: 4, RSSBytes: 25165824}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(`{"op":"status"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	raw, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"ok":true`)) || !bytes.Contains(raw, []byte(`"mode":"proxy"`)) {
		t.Fatalf("unexpected response: %s", raw)
	}
	if bytes.Contains(bytes.ToLower(raw), []byte("auth")) || bytes.Contains(bytes.ToLower(raw), []byte("password")) {
		t.Fatalf("secret field in response: %s", raw)
	}
}

func TestClientRejectsUnknownOperation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.sock")
	server, err := Listen(path, func() Snapshot { return Snapshot{} })
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if _, err := Request(path, "invalid"); err == nil {
		t.Fatal("unknown control operation unexpectedly succeeded")
	}
}
