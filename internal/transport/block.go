package transport

import (
	"context"
	"errors"
)

type BlockPacketDialer struct{}

func (BlockPacketDialer) OpenPacket(context.Context) (PacketSession, error) {
	return nil, errors.New("UDP blocked by policy")
}
