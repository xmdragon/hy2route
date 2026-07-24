package dataset

import (
	"bytes"
	"net/netip"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestDatasetRoundTrip(t *testing.T) {
	want := Data{
		Domains:  []Domain{{Name: "wechat.com", Exact: false}, {Name: "wx.qq.com", Exact: true}},
		Prefixes: []netip.Prefix{netip.MustParsePrefix("120.232.0.0/12")},
	}
	var buf bytes.Buffer
	if err := Write(&buf, want); err != nil {
		t.Fatal(err)
	}
	got, err := Read(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(want, got, cmpopts.EquateComparable(netip.Prefix{})); diff != "" {
		t.Fatal(diff)
	}
}

func TestReadRejectsTamperedChecksum(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Data{Domains: []Domain{{Name: "wechat.com"}}}); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	raw[len(raw)-1] ^= 0xff
	if _, err := Read(bytes.NewReader(raw)); err == nil {
		t.Fatal("Read accepted a tampered checksum")
	}
}
