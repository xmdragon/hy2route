package hy2

import (
	"errors"
	"net"
	"sync"

	coreclient "github.com/apernet/hysteria/core/v2/client"
	coreErrs "github.com/apernet/hysteria/core/v2/errors"
	"github.com/xmdragon/hy2route/internal/transport"
)

// reconnectableCore keeps the connection generation beside the exact underlying
// client used by an operation. This lets late failures from an old connection be
// rejected after a newer handshake succeeds.
type reconnectableCore struct {
	configFunc func() (*coreclient.Config, error)
	events     transport.EventSink

	mu       sync.Mutex
	client   coreclient.Client
	sequence uint64
	closed   bool
}

func newReconnectableCore(configFunc func() (*coreclient.Config, error), events transport.EventSink) *reconnectableCore {
	return &reconnectableCore{configFunc: configFunc, events: events}
}

func (core *reconnectableCore) reconnectLocked() error {
	if core.client != nil {
		_ = core.client.Close()
		core.client = nil
	}
	core.sequence++
	config, err := core.configFunc()
	if err != nil {
		return err
	}
	client, _, err := coreclient.NewClient(config)
	if err != nil {
		return err
	}
	core.client = client
	core.events.Emit(transport.Event{Stage: "hy2.connected", Reason: "connected", Sequence: core.sequence})
	return nil
}

func (core *reconnectableCore) clientDo(f func(coreclient.Client) (any, error)) (any, uint64, error) {
	core.mu.Lock()
	if core.closed {
		sequence := core.sequence
		core.mu.Unlock()
		return nil, sequence, coreErrs.ClosedError{}
	}
	if core.client == nil {
		if err := core.reconnectLocked(); err != nil {
			sequence := core.sequence
			core.mu.Unlock()
			return nil, sequence, err
		}
	}
	client, sequence := core.client, core.sequence
	core.mu.Unlock()

	value, err := f(client)
	var closedError coreErrs.ClosedError
	if errors.As(err, &closedError) {
		core.mu.Lock()
		if core.client == client {
			core.client = nil
		}
		core.mu.Unlock()
	}
	return value, sequence, err
}

func (core *reconnectableCore) TCP(target string) (net.Conn, uint64, error) {
	value, sequence, err := core.clientDo(func(client coreclient.Client) (any, error) {
		return client.TCP(target)
	})
	if err != nil {
		return nil, sequence, err
	}
	return value.(net.Conn), sequence, nil
}

func (core *reconnectableCore) UDP() (coreclient.HyUDPConn, uint64, error) {
	value, sequence, err := core.clientDo(func(client coreclient.Client) (any, error) {
		return client.UDP()
	})
	if err != nil {
		return nil, sequence, err
	}
	return value.(coreclient.HyUDPConn), sequence, nil
}

func (core *reconnectableCore) Close() error {
	core.mu.Lock()
	defer core.mu.Unlock()
	core.closed = true
	if core.client != nil {
		return core.client.Close()
	}
	return nil
}
