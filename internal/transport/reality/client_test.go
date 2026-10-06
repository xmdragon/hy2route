package reality

import (
	"github.com/xmdragon/hy2route/internal/config"
	"testing"
)

func TestInvalidRealityKeyFailsBeforeNetwork(t *testing.T) {
	_, err := New(config.TCPRelayConfig{Server: "127.0.0.1:443", ServerName: "example.com", Fingerprint: "chrome", PublicKey: "bad"}, 0)
	if err == nil {
		t.Fatal("invalid Reality public key accepted")
	}
}
