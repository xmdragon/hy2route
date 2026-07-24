package sniff

import (
	"bufio"
	"context"
	"errors"
	"net"
	"time"
)

func Parse(raw []byte) Result {
	result, _ := parse(raw)
	return result
}

func Peek(ctx context.Context, conn net.Conn, limits Limits) (Result, *bufio.Reader, error) {
	if conn == nil {
		return Result{}, nil, errors.New("sniff connection is required")
	}
	if limits.Bytes < 1 {
		return Result{}, nil, errors.New("sniff byte limit must be positive")
	}
	if limits.Timeout <= 0 {
		return Result{}, nil, errors.New("sniff timeout must be positive")
	}
	reader := bufio.NewReaderSize(conn, limits.Bytes)
	deadline := time.Now().Add(limits.Timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetReadDeadline(deadline); err != nil {
		return Result{}, reader, err
	}
	defer conn.SetReadDeadline(time.Time{})
	for wanted := 1; ; wanted++ {
		if err := ctx.Err(); err != nil {
			return Result{}, reader, err
		}
		if wanted > limits.Bytes {
			return ParseFromReader(reader), reader, nil
		}
		raw, err := reader.Peek(wanted)
		result, state := parse(raw)
		if state != parseIncomplete || len(raw) >= limits.Bytes {
			return result, reader, nil
		}
		if err != nil {
			return result, reader, nil
		}
	}
}

func ParseFromReader(reader *bufio.Reader) Result {
	if reader == nil || reader.Buffered() == 0 {
		return Result{}
	}
	raw, err := reader.Peek(reader.Buffered())
	if err != nil {
		return Result{}
	}
	return Parse(raw)
}

func parse(raw []byte) (Result, parseState) {
	if len(raw) == 0 {
		return Result{}, parseIncomplete
	}
	if raw[0] == 22 {
		return parseTLS(raw)
	}
	return parseHTTP(raw)
}
