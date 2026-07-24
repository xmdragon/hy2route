package sniff

import "testing"

func FuzzTLSAndHTTPParsers(f *testing.F) {
	f.Add(buildClientHello("wechat.com"))
	f.Add([]byte("GET / HTTP/1.1\r\nHost: wechat.com\r\n\r\n"))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 16384 {
			raw = raw[:16384]
		}
		_ = Parse(raw)
	})
}
