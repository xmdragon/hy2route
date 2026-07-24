// Package control exposes a root-only local status socket for hy2route-core.
package control

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"
)

const maxRequestBytes = 4096

type Snapshot struct {
	Mode         string `json:"mode"`
	HY2Connected bool   `json:"hy2_connected"`
	DNSCache     int    `json:"dns_cache"`
	LearnedIPs   int    `json:"learned_ips"`
	UDPSessions  int    `json:"udp_sessions"`
	ActiveTCP    int    `json:"active_tcp"`
	RSSBytes     uint64 `json:"rss_bytes"`
}

type Server struct {
	path     string
	listener *net.UnixListener
	snapshot func() Snapshot
}

type request struct {
	Operation string `json:"op"`
}

type response struct {
	OK bool `json:"ok"`
	Snapshot
	Error string `json:"error,omitempty"`
}

func Listen(path string, snapshot func() Snapshot) (*Server, error) {
	if path == "" {
		return nil, errors.New("control socket path is required")
	}
	if snapshot == nil {
		return nil, errors.New("control snapshot is required")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("control path is not a socket: %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	oldUmask := syscall.Umask(0o077)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	syscall.Umask(oldUmask)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, err
	}
	server := &Server{path: path, listener: listener, snapshot: snapshot}
	go server.accept()
	return server, nil
}

func (s *Server) Close() error {
	err := s.listener.Close()
	if removeErr := os.Remove(s.path); removeErr != nil && !os.IsNotExist(removeErr) && err == nil {
		err = removeErr
	}
	return err
}

func (s *Server) accept() {
	for {
		conn, err := s.listener.AcceptUnix()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn *net.UnixConn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	var req request
	if err := json.NewDecoder(io.LimitReader(conn, maxRequestBytes)).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(response{Error: "invalid request"})
		return
	}
	if req.Operation != "status" {
		_ = json.NewEncoder(conn).Encode(response{Error: "unsupported operation"})
		return
	}
	_ = json.NewEncoder(conn).Encode(response{OK: true, Snapshot: s.snapshot()})
}

func Request(path, operation string) (Snapshot, error) {
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return Snapshot{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if err := json.NewEncoder(conn).Encode(request{Operation: operation}); err != nil {
		return Snapshot{}, err
	}
	var result response
	if err := json.NewDecoder(io.LimitReader(conn, maxRequestBytes)).Decode(&result); err != nil {
		return Snapshot{}, err
	}
	if !result.OK {
		return Snapshot{}, errors.New(result.Error)
	}
	return result.Snapshot, nil
}
