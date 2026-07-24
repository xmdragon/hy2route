package sniff

import (
	"encoding/binary"

	"github.com/xmdragon/hy2route/internal/policy"
)

func parseTLS(raw []byte) (Result, parseState) {
	if len(raw) < 5 {
		return Result{}, parseIncomplete
	}
	if raw[0] != 22 {
		return Result{}, parseInvalid
	}
	recordLength := int(binary.BigEndian.Uint16(raw[3:5]))
	if recordLength < 4 {
		return Result{}, parseInvalid
	}
	if len(raw) < 5+recordLength {
		return Result{}, parseIncomplete
	}
	record := raw[5 : 5+recordLength]
	if record[0] != 1 || len(record) < 4 {
		return Result{}, parseInvalid
	}
	handshakeLength := int(record[1])<<16 | int(record[2])<<8 | int(record[3])
	if handshakeLength > len(record)-4 {
		return Result{}, parseInvalid
	}
	if handshakeLength < 34 {
		return Result{}, parseInvalid
	}
	body := record[4 : 4+handshakeLength]
	pos := 34
	var ok bool
	if pos, ok = skipVector(body, pos, 1); !ok {
		return Result{}, parseInvalid
	}
	if pos, ok = skipVector(body, pos, 2); !ok {
		return Result{}, parseInvalid
	}
	if pos, ok = skipVector(body, pos, 1); !ok {
		return Result{}, parseInvalid
	}
	if pos+2 > len(body) {
		return Result{}, parseInvalid
	}
	extensionsLength := int(binary.BigEndian.Uint16(body[pos : pos+2]))
	pos += 2
	if extensionsLength > len(body)-pos {
		return Result{}, parseInvalid
	}
	extensions := body[pos : pos+extensionsLength]
	for pos := 0; pos < len(extensions); {
		if pos+4 > len(extensions) {
			return Result{}, parseInvalid
		}
		typeID := binary.BigEndian.Uint16(extensions[pos : pos+2])
		length := int(binary.BigEndian.Uint16(extensions[pos+2 : pos+4]))
		pos += 4
		if length > len(extensions)-pos {
			return Result{}, parseInvalid
		}
		if typeID == 0 {
			if domain := parseServerName(extensions[pos : pos+length]); domain != "" {
				return Result{Domain: domain, Protocol: "tls", Complete: true}, parseComplete
			}
			return Result{Protocol: "tls", Complete: true}, parseComplete
		}
		pos += length
	}
	return Result{Protocol: "tls", Complete: true}, parseComplete
}

func skipVector(raw []byte, pos, width int) (int, bool) {
	if width != 1 && width != 2 {
		return 0, false
	}
	if pos+width > len(raw) {
		return 0, false
	}
	length := int(raw[pos])
	if width == 2 {
		length = int(binary.BigEndian.Uint16(raw[pos : pos+2]))
	}
	pos += width
	if length > len(raw)-pos {
		return 0, false
	}
	return pos + length, true
}

func parseServerName(raw []byte) string {
	if len(raw) < 2 {
		return ""
	}
	listLength := int(binary.BigEndian.Uint16(raw[:2]))
	if listLength != len(raw)-2 {
		return ""
	}
	for pos := 2; pos < len(raw); {
		if pos+3 > len(raw) {
			return ""
		}
		nameType := raw[pos]
		length := int(binary.BigEndian.Uint16(raw[pos+1 : pos+3]))
		pos += 3
		if length > len(raw)-pos {
			return ""
		}
		if nameType == 0 {
			domain, err := policy.NormalizeDomain(string(raw[pos : pos+length]))
			if err == nil {
				return domain
			}
			return ""
		}
		pos += length
	}
	return ""
}
