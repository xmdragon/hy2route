package dataset

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
)

var magic = [8]byte{'H', '2', 'R', 'D', 'A', 'T', 'A', '1'}

type Domain struct {
	Name  string
	Exact bool
}

type Data struct {
	Domains  []Domain
	Prefixes []netip.Prefix
}

func Load(path string) (Data, error) {
	file, err := os.Open(path)
	if err != nil {
		return Data{}, fmt.Errorf("open data: %w", err)
	}
	defer file.Close()
	return Read(file)
}

func Write(writer io.Writer, data Data) error {
	var payload bytes.Buffer
	payload.Write(magic[:])
	if len(data.Domains) > int(^uint32(0)) || len(data.Prefixes) > int(^uint32(0)) {
		return errors.New("dataset has too many entries")
	}
	writeUint32(&payload, uint32(len(data.Domains)))
	for _, domain := range data.Domains {
		if err := validateDomain(domain); err != nil {
			return err
		}
		kind := byte(0)
		if domain.Exact {
			kind = 1
		}
		payload.WriteByte(kind)
		writeUint16(&payload, uint16(len(domain.Name)))
		payload.WriteString(domain.Name)
	}
	writeUint32(&payload, uint32(len(data.Prefixes)))
	for _, prefix := range data.Prefixes {
		if !prefix.IsValid() || !prefix.Addr().Is4() || prefix.Addr().Is4In6() {
			return fmt.Errorf("invalid IPv4 prefix %q", prefix)
		}
		addr := prefix.Addr().As4()
		payload.Write(addr[:])
		payload.WriteByte(byte(prefix.Bits()))
	}
	sum := sha256.Sum256(payload.Bytes())
	if _, err := writer.Write(payload.Bytes()); err != nil {
		return err
	}
	_, err := writer.Write(sum[:])
	return err
}

func Read(reader io.Reader) (Data, error) {
	raw, err := io.ReadAll(reader)
	if err != nil {
		return Data{}, err
	}
	if len(raw) < len(magic)+4+4+sha256.Size {
		return Data{}, errors.New("dataset is too short")
	}
	payload, expectedSum := raw[:len(raw)-sha256.Size], raw[len(raw)-sha256.Size:]
	actualSum := sha256.Sum256(payload)
	if !bytes.Equal(actualSum[:], expectedSum) {
		return Data{}, errors.New("dataset checksum mismatch")
	}
	offset := 0
	if !bytes.Equal(payload[:len(magic)], magic[:]) {
		return Data{}, errors.New("dataset magic mismatch")
	}
	offset += len(magic)
	domainCount, ok := readUint32(payload, &offset)
	if !ok {
		return Data{}, errors.New("dataset domain count is truncated")
	}
	data := Data{Domains: make([]Domain, 0, domainCount)}
	for range domainCount {
		if offset+3 > len(payload) {
			return Data{}, errors.New("dataset domain is truncated")
		}
		kind := payload[offset]
		offset++
		length := int(binary.BigEndian.Uint16(payload[offset : offset+2]))
		offset += 2
		if (kind != 0 && kind != 1) || length == 0 || offset+length > len(payload) {
			return Data{}, errors.New("dataset domain entry is invalid")
		}
		domain := Domain{Name: string(payload[offset : offset+length]), Exact: kind == 1}
		offset += length
		if err := validateDomain(domain); err != nil {
			return Data{}, err
		}
		data.Domains = append(data.Domains, domain)
	}
	prefixCount, ok := readUint32(payload, &offset)
	if !ok {
		return Data{}, errors.New("dataset prefix count is truncated")
	}
	data.Prefixes = make([]netip.Prefix, 0, prefixCount)
	for range prefixCount {
		if offset+5 > len(payload) {
			return Data{}, errors.New("dataset prefix is truncated")
		}
		addr := netip.AddrFrom4([4]byte(payload[offset : offset+4]))
		bits := int(payload[offset+4])
		offset += 5
		if bits > 32 {
			return Data{}, errors.New("dataset prefix bits are invalid")
		}
		data.Prefixes = append(data.Prefixes, netip.PrefixFrom(addr, bits))
	}
	if offset != len(payload) {
		return Data{}, errors.New("dataset has trailing payload")
	}
	return data, nil
}

func validateDomain(domain Domain) error {
	if len(domain.Name) == 0 || len(domain.Name) > 65535 {
		return errors.New("dataset domain length is invalid")
	}
	for _, char := range domain.Name {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '.' || char == '-') {
			return fmt.Errorf("dataset domain %q is not lower-case ASCII", domain.Name)
		}
	}
	return nil
}

func writeUint16(buffer *bytes.Buffer, value uint16) {
	var raw [2]byte
	binary.BigEndian.PutUint16(raw[:], value)
	buffer.Write(raw[:])
}

func writeUint32(buffer *bytes.Buffer, value uint32) {
	var raw [4]byte
	binary.BigEndian.PutUint32(raw[:], value)
	buffer.Write(raw[:])
}

func readUint32(raw []byte, offset *int) (uint32, bool) {
	if *offset+4 > len(raw) {
		return 0, false
	}
	value := binary.BigEndian.Uint32(raw[*offset : *offset+4])
	*offset += 4
	return value, true
}
