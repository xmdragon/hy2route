//go:build linux

package dataplane

import (
	"net"
	"syscall"
)

func prepareListener(conn syscall.Conn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		controlErr = syscall.SetNonblock(int(fd), true)
	}); err != nil {
		return err
	}
	return controlErr
}

func listenTransparentTCP(network string, addr *net.TCPAddr) (*net.TCPListener, error) {
	// go-tproxy's TCP wrapper hides its net.TCPListener, so its File() call
	// cannot be followed by a SyscallConn nonblocking repair here.
	listener, err := net.ListenTCP(network, addr)
	if err != nil {
		return nil, err
	}
	if err := setTransparent(listener); err != nil {
		_ = listener.Close()
		return nil, err
	}
	if err := prepareListener(listener); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}

func setTransparent(conn syscall.Conn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		controlErr = syscall.SetsockoptInt(int(fd), syscall.SOL_IP, syscall.IP_TRANSPARENT, 1)
	}); err != nil {
		return err
	}
	return controlErr
}
