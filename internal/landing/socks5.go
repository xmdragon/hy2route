package landing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/xmdragon/hy2route/internal/transport"
)

type socks5Dialer struct {
	base                   transport.StreamDialer
	server, user, password string
}

func newSOCKS5(base transport.StreamDialer, server, user, password string) transport.StreamDialer {
	return &socks5Dialer{base, server, user, password}
}

func (dialer *socks5Dialer) Dial(ctx context.Context, target string) (net.Conn, error) {
	conn, err := dialer.base.Dial(ctx, dialer.server)
	if err != nil {
		return nil, fmt.Errorf("SOCKS5 upstream dial: %w", err)
	}
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		conn.Close()
		return nil, err
	}
	defer conn.SetDeadline(time.Time{})
	methods := []byte{5, 1, 0}
	if dialer.user != "" || dialer.password != "" {
		methods = []byte{5, 1, 2}
	}
	if _, err := conn.Write(methods); err != nil {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 greeting write: %w", err)
	}
	response := make([]byte, 2)
	if _, err := readFull(conn, response); err != nil {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 greeting read: %w", err)
	}
	if response[0] != 5 {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 greeting: invalid version 0x%02x", response[0])
	}
	if response[1] == 0xff {
		conn.Close()
		return nil, errors.New("SOCKS5 method rejected")
	}
	if response[1] != methods[2] {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 greeting: server selected unoffered method 0x%02x", response[1])
	}
	if response[1] == 2 {
		if len(dialer.user) > 255 || len(dialer.password) > 255 {
			conn.Close()
			return nil, errors.New("SOCKS5 credentials too long")
		}
		auth := append([]byte{1, byte(len(dialer.user))}, []byte(dialer.user)...)
		auth = append(auth, byte(len(dialer.password)))
		auth = append(auth, []byte(dialer.password)...)
		if _, err := conn.Write(auth); err != nil {
			conn.Close()
			return nil, fmt.Errorf("SOCKS5 authentication write: %w", err)
		}
		if _, err := readFull(conn, response); err != nil {
			conn.Close()
			return nil, fmt.Errorf("SOCKS5 authentication read: %w", err)
		}
		if response[0] != 1 {
			conn.Close()
			return nil, fmt.Errorf("SOCKS5 authentication: invalid version 0x%02x", response[0])
		}
		if response[1] != 0 {
			conn.Close()
			return nil, fmt.Errorf("SOCKS5 authentication rejected (status 0x%02x)", response[1])
		}
	}
	host, portText, err := net.SplitHostPort(target)
	if err != nil {
		conn.Close()
		return nil, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		conn.Close()
		return nil, errors.New("invalid target port")
	}
	request := []byte{5, 1, 0}
	if ip := net.ParseIP(host).To4(); ip != nil {
		request = append(request, 1)
		request = append(request, ip...)
	} else {
		if len(host) == 0 || len(host) > 255 {
			conn.Close()
			return nil, errors.New("invalid target domain")
		}
		request = append(request, 3, byte(len(host)))
		request = append(request, host...)
	}
	request = append(request, byte(port>>8), byte(port))
	if _, err := conn.Write(request); err != nil {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 connect write: %w", err)
	}
	response = make([]byte, 4)
	if _, err := readFull(conn, response); err != nil {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 connect read: %w", err)
	}
	if response[0] != 5 || response[2] != 0 {
		conn.Close()
		return nil, errors.New("SOCKS5 connect: invalid reply header")
	}
	if response[1] != 0 {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 connect rejected (status 0x%02x)", response[1])
	}
	remaining := 0
	switch response[3] {
	case 1:
		remaining = 4
	case 3:
		n := make([]byte, 1)
		if _, err := readFull(conn, n); err != nil {
			conn.Close()
			return nil, fmt.Errorf("SOCKS5 connect address length read: %w", err)
		}
		remaining = int(n[0])
	case 4:
		remaining = 16
	default:
		conn.Close()
		return nil, errors.New("invalid SOCKS5 reply")
	}
	discard := make([]byte, remaining+2)
	if _, err := readFull(conn, discard); err != nil {
		conn.Close()
		return nil, fmt.Errorf("SOCKS5 connect address read: %w", err)
	}
	return conn, nil
}
func readFull(conn net.Conn, data []byte) (int, error) {
	return io.ReadFull(conn, data)
}
