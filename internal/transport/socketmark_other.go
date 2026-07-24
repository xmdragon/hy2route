//go:build !linux

package transport

import "syscall"

func socketMarkControl(uint32) func(string, string, syscall.RawConn) error {
	return func(_, _ string, _ syscall.RawConn) error { return nil }
}
