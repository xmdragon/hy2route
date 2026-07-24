package policy

import (
	"net/netip"
	"testing"
	"time"
)

func TestLearningConflictUsesInspectProxy(t *testing.T) {
	now := time.Unix(100, 0)
	table := NewLearningTable(8)
	ip := netip.MustParseAddr("203.0.113.8")
	if err := table.Observe(Observation{Domain: "cn.example", Action: Direct, IPs: []netip.Addr{ip}, Expires: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if err := table.Observe(Observation{Domain: "world.example", Action: Proxy, IPs: []netip.Addr{ip}, Expires: now.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	got := table.Lookup(ip, now)
	if !got.Direct || !got.Proxy || got.Action != Proxy || !got.Inspect {
		t.Fatalf("conflict result = %#v", got)
	}
}

func TestLearningExpiresAndEvictsOldest(t *testing.T) {
	table := NewLearningTable(2)
	now := time.Unix(100, 0)
	ip1 := netip.MustParseAddr("192.0.2.1")
	ip2 := netip.MustParseAddr("192.0.2.2")
	ip3 := netip.MustParseAddr("192.0.2.3")
	for _, observation := range []Observation{
		{Domain: "one.example", Action: Direct, IPs: []netip.Addr{ip1}, Expires: now.Add(time.Minute)},
		{Domain: "two.example", Action: Direct, IPs: []netip.Addr{ip2}, Expires: now.Add(time.Minute)},
		{Domain: "three.example", Action: Direct, IPs: []netip.Addr{ip3}, Expires: now.Add(time.Minute)},
	} {
		if err := table.Observe(observation); err != nil {
			t.Fatal(err)
		}
	}
	if got := table.Lookup(ip1, now); got.Action != Unknown {
		t.Fatalf("oldest entry was not evicted: %#v", got)
	}
	if got := table.Lookup(ip3, now.Add(61*time.Second)); got.Action != Unknown {
		t.Fatalf("expired entry remains: %#v", got)
	}
}

func TestLearningSnapshotNeverExceedsCapacity(t *testing.T) {
	table := NewLearningTable(2)
	now := time.Unix(100, 0)
	for _, address := range []string{"198.51.100.1", "198.51.100.2", "198.51.100.3"} {
		if err := table.Observe(Observation{
			Domain: "bulk.example", Action: Proxy,
			IPs: []netip.Addr{netip.MustParseAddr(address)}, Expires: now.Add(time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(table.Snapshot(now)); got != 2 {
		t.Fatalf("snapshot length = %d", got)
	}
}
