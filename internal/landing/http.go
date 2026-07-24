package landing

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"time"

	"github.com/xmdragon/hy2route/internal/config"
	"github.com/xmdragon/hy2route/internal/transport"
)

type httpDialer struct {
	base                   transport.StreamDialer
	server, user, password string
}

func newHTTP(base transport.StreamDialer, server, user, password string) transport.StreamDialer {
	return &httpDialer{base, server, user, password}
}

func New(cfg config.LandingConfig, base transport.StreamDialer) (transport.StreamDialer, error) {
	switch cfg.Type {
	case "direct":
		return base, nil
	case "http":
		return newHTTP(base, cfg.Server, cfg.Username, cfg.Password), nil
	case "socks5":
		return newSOCKS5(base, cfg.Server, cfg.Username, cfg.Password), nil
	default:
		return nil, fmt.Errorf("unsupported landing type %q", cfg.Type)
	}
}

func (dialer *httpDialer) Dial(ctx context.Context, target string) (net.Conn, error) {
	conn, err := dialer.base.Dial(ctx, dialer.server)
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		conn.Close()
		return nil, err
	}
	defer conn.SetDeadline(time.Time{})
	request := "CONNECT " + target + " HTTP/1.1\r\nHost: " + target + "\r\n"
	if dialer.user != "" || dialer.password != "" {
		request += "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(dialer.user+":"+dialer.password)) + "\r\n"
	}
	if _, err := conn.Write([]byte(request + "\r\n")); err != nil {
		conn.Close()
		return nil, err
	}
	reader := bufio.NewReaderSize(conn, 8192)
	line, err := reader.ReadString('\n')
	if err != nil || len(line) > 1024 || !strings.HasPrefix(line, "HTTP/") || !strings.Contains(line, " 200 ") {
		conn.Close()
		return nil, fmt.Errorf("HTTP CONNECT rejected: %v", err)
	}
	headers, err := textproto.NewReader(reader).ReadMIMEHeader()
	if err != nil || headers == nil {
		conn.Close()
		return nil, fmt.Errorf("HTTP CONNECT headers: %w", err)
	}
	return &prefixedConn{Conn: conn, reader: reader}, nil
}
