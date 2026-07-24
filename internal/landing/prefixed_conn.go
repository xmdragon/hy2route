package landing

import (
	"bufio"
	"net"
)

type prefixedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (conn *prefixedConn) Read(data []byte) (int, error) { return conn.reader.Read(data) }
