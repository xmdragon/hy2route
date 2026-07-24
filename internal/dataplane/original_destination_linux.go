//go:build linux

package dataplane

import (
	"errors"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func socketOriginalDestination(conn net.Conn) (net.Addr, error) {
	syscallConn, ok := conn.(interface {
		SyscallConn() (syscall.RawConn, error)
	})
	if !ok {
		return nil, errors.New("connection does not expose a socket")
	}
	raw, err := syscallConn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var (
		target    *net.TCPAddr
		socketErr error
	)
	if err := raw.Control(func(fd uintptr) {
		address, err := unix.GetsockoptIPv6Mreq(int(fd), unix.IPPROTO_IP, unix.SO_ORIGINAL_DST)
		if err != nil {
			socketErr = err
			return
		}
		rawAddress := address.Multiaddr
		port := int(rawAddress[2])<<8 | int(rawAddress[3])
		target = &net.TCPAddr{IP: net.IPv4(rawAddress[4], rawAddress[5], rawAddress[6], rawAddress[7]), Port: port}
	}); err != nil {
		return nil, err
	}
	if socketErr != nil {
		return nil, socketErr
	}
	if target == nil || target.Port == 0 || target.IP.IsUnspecified() {
		return nil, errors.New("original destination is invalid")
	}
	return target, nil
}
