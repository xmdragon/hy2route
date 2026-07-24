package dnsproxy

import (
	"context"
	"testing"

	"github.com/miekg/dns"
)

func FuzzResolverNeverPanics(f *testing.F) {
	f.Add([]byte{0, 1, 0, 0})
	f.Add(mustPackQuestion(f, "wechat.com.", dns.TypeA))
	f.Fuzz(func(t *testing.T, raw []byte) {
		var message dns.Msg
		if message.Unpack(raw) != nil {
			return
		}
		resolver := newTestResolver(t)
		_, _ = resolver.Resolve(context.Background(), &message)
	})
}

func mustPackQuestion(t testing.TB, name string, qtype uint16) []byte {
	t.Helper()
	message := new(dns.Msg)
	message.SetQuestion(name, qtype)
	raw, err := message.Pack()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
