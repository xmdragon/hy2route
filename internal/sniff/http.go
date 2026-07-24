package sniff

import (
	"bytes"
	"net"
	"strconv"
	"strings"

	"github.com/xmdragon/hy2route/internal/policy"
)

func parseHTTP(raw []byte) (Result, parseState) {
	space := bytes.IndexByte(raw, ' ')
	if space < 0 {
		for _, char := range raw {
			if !isHTTPMethodChar(char) {
				return Result{}, parseInvalid
			}
		}
		return Result{}, parseIncomplete
	}
	if space == 0 {
		return Result{}, parseInvalid
	}
	for _, char := range raw[:space] {
		if !isHTTPMethodChar(char) {
			return Result{}, parseInvalid
		}
	}
	end := bytes.Index(raw, []byte("\r\n\r\n"))
	if end < 0 {
		return Result{}, parseIncomplete
	}
	var host string
	for _, line := range bytes.Split(raw[:end], []byte("\r\n"))[1:] {
		colon := bytes.IndexByte(line, ':')
		if colon <= 0 {
			return Result{Protocol: "http", Complete: true}, parseComplete
		}
		name := string(line[:colon])
		if strings.EqualFold(name, "host") {
			if host != "" {
				return Result{Protocol: "http", Complete: true}, parseComplete
			}
			host = string(line[colon+1:])
		}
	}
	domain := normalizeHTTPHost(host)
	return Result{Domain: domain, Protocol: "http", Complete: true}, parseComplete
}

func isHTTPMethodChar(char byte) bool {
	return char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z'
}

func normalizeHTTPHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, " \t\r\n") || strings.Contains(host, "[") || strings.Contains(host, "]") {
		return ""
	}
	if name, port, err := net.SplitHostPort(host); err == nil {
		if _, err := strconv.ParseUint(port, 10, 16); err != nil || name == "" {
			return ""
		}
		host = name
	}
	domain, err := policy.NormalizeDomain(host)
	if err != nil {
		return ""
	}
	return domain
}
