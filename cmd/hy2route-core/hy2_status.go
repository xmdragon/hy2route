package main

import (
	"log"
	"strings"
	"sync"
	"time"

	"github.com/xmdragon/hy2route/internal/transport"
)

type hy2StatusSnapshot struct {
	State           string
	Connected       bool
	LastSuccess     string
	LastError       string
	LastErrorReason string
	LastErrorStage  string
}

type hy2StatusTracker struct {
	mu       sync.RWMutex
	now      func() time.Time
	snapshot hy2StatusSnapshot
	sequence uint64
	secrets  []string
	logf     func(string, ...any)
	lastLog  time.Time
}

func newHY2StatusTracker(now func() time.Time, secrets ...string) *hy2StatusTracker {
	if now == nil {
		now = time.Now
	}
	return &hy2StatusTracker{
		now:      now,
		snapshot: hy2StatusSnapshot{State: "idle"},
		secrets:  secrets,
		logf:     log.Printf,
	}
}

func (tracker *hy2StatusTracker) Emit(event transport.Event) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	switch event.Stage {
	case "hy2.connected":
		if event.Sequence < tracker.sequence {
			return
		}
		tracker.sequence = event.Sequence
		tracker.snapshot.State = "connected"
		tracker.snapshot.Connected = true
		tracker.snapshot.LastSuccess = tracker.now().UTC().Format(time.RFC3339Nano)
	case "hy2.tcp", "hy2.udp":
		if event.Sequence < tracker.sequence {
			return
		}
		now := tracker.now().UTC()
		reason := event.Reason
		for _, secret := range tracker.secrets {
			if secret != "" {
				reason = strings.ReplaceAll(reason, secret, "[redacted]")
			}
		}
		reason = strings.Join(strings.Fields(reason), " ")
		if len([]rune(reason)) > 1024 {
			reason = string([]rune(reason)[:1024]) + "..."
		}
		if tracker.logf != nil && (tracker.snapshot.State != "degraded" || tracker.lastLog.IsZero() || now.Sub(tracker.lastLog) >= time.Minute) {
			tracker.logf("stage=%s reason=%q", event.Stage, reason)
			tracker.lastLog = now
		}
		tracker.sequence = event.Sequence
		tracker.snapshot.State = "degraded"
		tracker.snapshot.Connected = false
		tracker.snapshot.LastError = now.Format(time.RFC3339Nano)
		tracker.snapshot.LastErrorReason = reason
		tracker.snapshot.LastErrorStage = event.Stage
	}
}

func (tracker *hy2StatusTracker) Snapshot() hy2StatusSnapshot {
	tracker.mu.RLock()
	defer tracker.mu.RUnlock()
	return tracker.snapshot
}
