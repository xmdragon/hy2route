package hy2

import (
	"errors"
	"net"
	"testing"

	coreclient "github.com/apernet/hysteria/core/v2/client"
	coreErrs "github.com/apernet/hysteria/core/v2/errors"
)

type blockingCoreClient struct {
	started chan struct{}
	release chan struct{}
}

func (client *blockingCoreClient) TCP(string) (net.Conn, error) {
	close(client.started)
	<-client.release
	return nil, coreErrs.ClosedError{}
}
func (*blockingCoreClient) UDP() (coreclient.HyUDPConn, error) { return nil, nil }
func (*blockingCoreClient) Close() error                       { return nil }

func TestReconnectableCoreReturnsSequenceOfActualClient(t *testing.T) {
	old := &blockingCoreClient{started: make(chan struct{}), release: make(chan struct{})}
	core := &reconnectableCore{client: old, sequence: 1}
	type result struct {
		sequence uint64
		err      error
	}
	resultCh := make(chan result, 1)
	go func() {
		_, sequence, err := core.TCP("203.0.113.8:443")
		resultCh <- result{sequence: sequence, err: err}
	}()
	<-old.started

	core.mu.Lock()
	core.client = &blockingCoreClient{started: make(chan struct{}), release: make(chan struct{})}
	core.sequence = 2
	core.mu.Unlock()
	close(old.release)

	got := <-resultCh
	if !errors.As(got.err, new(coreErrs.ClosedError)) || got.sequence != 1 {
		t.Fatalf("result = %+v", got)
	}
}
