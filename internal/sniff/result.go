package sniff

import "time"

type Limits struct {
	Bytes   int
	Timeout time.Duration
}

type Result struct {
	Domain   string
	Protocol string
	Complete bool
}

type parseState uint8

const (
	parseInvalid parseState = iota
	parseIncomplete
	parseComplete
)
