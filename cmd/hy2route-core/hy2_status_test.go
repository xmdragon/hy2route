package main

import (
	"testing"
	"time"

	"github.com/xmdragon/hy2route/internal/transport"
)

func TestHY2StatusStartsIdle(t *testing.T) {
	tracker := newHY2StatusTracker(func() time.Time { return time.Unix(1, 0).UTC() })
	got := tracker.Snapshot()
	if got.State != "idle" || got.Connected || got.LastSuccess != "" || got.LastError != "" {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestHY2StatusTracksConnectionErrorAndRecovery(t *testing.T) {
	now := time.Date(2026, 8, 11, 7, 0, 0, 123, time.UTC)
	tracker := newHY2StatusTracker(func() time.Time { return now })
	tracker.Emit(transport.Event{Stage: "hy2.connected", Reason: "connected"})
	connected := tracker.Snapshot()
	if !connected.Connected || connected.State != "connected" || connected.LastSuccess != now.Format(time.RFC3339Nano) {
		t.Fatalf("connected = %+v", connected)
	}

	now = now.Add(time.Second)
	tracker.Emit(transport.Event{Stage: "hy2.tcp", Reason: "secret-bearing detail must not be retained"})
	degraded := tracker.Snapshot()
	if degraded.Connected || degraded.State != "degraded" || degraded.LastError != now.Format(time.RFC3339Nano) {
		t.Fatalf("degraded = %+v", degraded)
	}

	now = now.Add(time.Second)
	tracker.Emit(transport.Event{Stage: "hy2.connected"})
	recovered := tracker.Snapshot()
	if !recovered.Connected || recovered.State != "connected" || recovered.LastError != degraded.LastError || recovered.LastSuccess != now.Format(time.RFC3339Nano) {
		t.Fatalf("recovered = %+v", recovered)
	}
}

func TestHY2StatusTracksUDPError(t *testing.T) {
	now := time.Date(2026, 8, 11, 7, 1, 0, 0, time.FixedZone("test", -7*60*60))
	tracker := newHY2StatusTracker(func() time.Time { return now })
	tracker.Emit(transport.Event{Stage: "hy2.udp", Reason: "packet session failed"})
	got := tracker.Snapshot()
	if got.Connected || got.State != "degraded" || got.LastError != now.UTC().Format(time.RFC3339Nano) {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestHY2StatusIgnoresUnrelatedEvents(t *testing.T) {
	tracker := newHY2StatusTracker(func() time.Time { return time.Unix(1, 0).UTC() })
	tracker.Emit(transport.Event{Stage: "fail-open", Reason: "unrelated"})
	got := tracker.Snapshot()
	if got.State != "idle" || got.Connected || got.LastSuccess != "" || got.LastError != "" {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestApplicationSnapshotReportsHY2Status(t *testing.T) {
	now := time.Date(2026, 8, 11, 7, 2, 0, 456, time.UTC)
	tracker := newHY2StatusTracker(func() time.Time { return now })
	tracker.Emit(transport.Event{Stage: "hy2.connected"})
	application := &application{hy2Status: tracker}

	got := application.snapshot()
	if !got.HY2Connected || got.HY2State != "connected" || got.HY2LastSuccess != now.Format(time.RFC3339Nano) || got.HY2LastError != "" {
		t.Fatalf("snapshot = %+v", got)
	}
}
