package main

import (
	"fmt"
	"strings"
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
	tracker.Emit(transport.Event{Stage: "hy2.tcp", Reason: "tls: certificate has expired"})
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

func TestHY2DiagnosticReasonIsRedactedAndRateLimited(t *testing.T) {
	now := time.Date(2026, 9, 12, 6, 0, 0, 0, time.UTC)
	tracker := newHY2StatusTracker(func() time.Time { return now }, "relay-secret", "landing-secret")
	var logs []string
	tracker.logf = func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	reason := "tls: certificate has expired\nrelay-secret landing-secret"
	tracker.Emit(transport.Event{Stage: "hy2.tcp", Reason: reason, Sequence: 2})
	for i := 0; i < 20; i++ {
		now = now.Add(time.Second)
		tracker.Emit(transport.Event{Stage: "hy2.tcp", Reason: reason, Sequence: 2})
	}
	got := (&application{hy2Status: tracker}).snapshot()
	if got.HY2LastErrorReason != "tls: certificate has expired [redacted] [redacted]" || got.HY2LastErrorStage != "hy2.tcp" {
		t.Fatalf("snapshot = %+v", got)
	}
	if len(logs) != 1 || strings.Contains(logs[0], "secret") || !strings.Contains(logs[0], "certificate has expired") {
		t.Fatalf("logs = %v", logs)
	}
	now = now.Add(time.Minute)
	tracker.Emit(transport.Event{Stage: "hy2.tcp", Reason: reason, Sequence: 2})
	if len(logs) != 2 {
		t.Fatalf("periodic log missing: %v", logs)
	}
	tracker.Emit(transport.Event{Stage: "hy2.connected", Sequence: 3})
	tracker.Emit(transport.Event{Stage: "hy2.tcp", Reason: "stale error", Sequence: 2})
	if tracker.Snapshot().State != "connected" || len(logs) != 2 || tracker.Snapshot().LastErrorReason != got.HY2LastErrorReason {
		t.Fatalf("stale error replaced recovery: %+v %v", tracker.Snapshot(), logs)
	}
	tracker.Emit(transport.Event{Stage: "hy2.udp", Reason: "new failure", Sequence: 3})
	if len(logs) != 3 {
		t.Fatalf("new outage not logged: %v", logs)
	}
}

func TestHY2DiagnosticReasonIsBoundedWithoutLogging(t *testing.T) {
	tracker := newHY2StatusTracker(nil)
	tracker.logf = nil
	tracker.Emit(transport.Event{Stage: "hy2.udp", Reason: strings.Repeat("错", 2000)})
	if len([]rune(tracker.Snapshot().LastErrorReason)) != 1027 {
		t.Fatal("unbounded diagnostic")
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

func TestHY2StatusIgnoresFailureOlderThanSuccessfulHandshake(t *testing.T) {
	now := time.Date(2026, 8, 11, 7, 2, 0, 0, time.UTC)
	tracker := newHY2StatusTracker(func() time.Time { return now })
	tracker.Emit(transport.Event{Stage: "hy2.connected", Sequence: 2})

	now = now.Add(time.Second)
	tracker.Emit(transport.Event{Stage: "hy2.tcp", Sequence: 1})
	got := tracker.Snapshot()
	if !got.Connected || got.State != "connected" || got.LastError != "" {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestHY2StatusAcceptsFailureFromHandshakeAttempt(t *testing.T) {
	now := time.Date(2026, 8, 11, 7, 3, 0, 0, time.UTC)
	tracker := newHY2StatusTracker(func() time.Time { return now })
	tracker.Emit(transport.Event{Stage: "hy2.connected", Sequence: 2})

	now = now.Add(time.Second)
	tracker.Emit(transport.Event{Stage: "hy2.tcp", Sequence: 2})
	got := tracker.Snapshot()
	if got.Connected || got.State != "degraded" || got.LastError != now.Format(time.RFC3339Nano) {
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
