package landing

import (
	"context"
	"errors"
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
		return nil, err
	}
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		conn.Close()
		return nil, err
	}
	defer conn.SetDeadline(time.Time{})
	methods := []byte{5, 1, 0}
	if dialer.user != "" || dialer.password != "" {
		methods = []byte{5, 2, 0, 2}
	}
	if _, err := conn.Write(methods); err != nil {
		conn.Close()
		return nil, err
	}
	response := make([]byte, 2)
	if _, err := readFull(conn, response); err != nil || response[0] != 5 || response[1] == 0xff {
		conn.Close()
		return nil, errors.New("SOCKS5 method rejected")
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
			return nil, err
		}
		if _, err := readFull(conn, response); err != nil || response[1] != 0 {
			conn.Close()
			return nil, errors.New("SOCKS5 authentication rejected")
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
		return nil, err
	}
	response = make([]byte, 4)
	if _, err := readFull(conn, response); err != nil || response[0] != 5 || response[1] != 0 {
		conn.Close()
		return nil, errors.New("SOCKS5 connect rejected")
	}
	remaining := 0
	switch response[3] {
	case 1:
		remaining = 4
	case 3:
		n := make([]byte, 1)
		if _, err := readFull(conn, n); err != nil {
			conn.Close()
			return nil, err
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
		return nil, err
	}
	return conn, nil
}
func readFull(conn net.Conn, data []byte) (int, error) {
	total := 0
	for total < len(data) {
		n, err := conn.Read(data[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
