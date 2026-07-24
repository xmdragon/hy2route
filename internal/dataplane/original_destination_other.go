//go:build !linux

package dataplane

import (
	"errors"
	"net"
)

func socketOriginalDestination(net.Conn) (net.Addr, error) {
	return nil, errors.New("original destination is available only on Linux")
}
