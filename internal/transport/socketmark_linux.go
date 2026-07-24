//go:build linux

package transport

import (
	"syscall"

	"golang.org/x/sys/unix"
)

func socketMarkControl(mark uint32) func(string, string, syscall.RawConn) error {
	return func(_, _ string, raw syscall.RawConn) error {
		var socketErr error
		if err := raw.Control(func(fd uintptr) {
			socketErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(mark))
		}); err != nil {
			return err
		}
		return socketErr
	}
}
