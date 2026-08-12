package main

import (
	"sync"
	"time"

	"github.com/xmdragon/hy2route/internal/transport"
)

type hy2StatusSnapshot struct {
	State       string
	Connected   bool
	LastSuccess string
	LastError   string
}

type hy2StatusTracker struct {
	mu       sync.RWMutex
	now      func() time.Time
	snapshot hy2StatusSnapshot
	sequence uint64
}

func newHY2StatusTracker(now func() time.Time) *hy2StatusTracker {
	if now == nil {
		now = time.Now
	}
	return &hy2StatusTracker{
		now:      now,
		snapshot: hy2StatusSnapshot{State: "idle"},
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
		tracker.sequence = event.Sequence
		tracker.snapshot.State = "degraded"
		tracker.snapshot.Connected = false
		tracker.snapshot.LastError = tracker.now().UTC().Format(time.RFC3339Nano)
	}
}

func (tracker *hy2StatusTracker) Snapshot() hy2StatusSnapshot {
	tracker.mu.RLock()
	defer tracker.mu.RUnlock()
	return tracker.snapshot
}
