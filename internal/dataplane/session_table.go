package dataplane

import (
	"container/list"
	"net/netip"
	"sync"
	"time"
)

type sessionKey struct {
	Source netip.AddrPort
	Target netip.AddrPort
}

type sessionCloser interface {
	Close() error
}

type sessionEntry struct {
	key     sessionKey
	session sessionCloser
	last    time.Time
	lru     *list.Element
}

type sessionTable struct {
	mu       sync.Mutex
	capacity int
	idle     time.Duration
	now      func() time.Time
	entries  map[sessionKey]*sessionEntry
	lru      list.List
}

func newSessionTable(capacity int, idle time.Duration, now func() time.Time) *sessionTable {
	if capacity < 1 {
		capacity = 1
	}
	if idle <= 0 {
		idle = time.Minute
	}
	if now == nil {
		now = time.Now
	}
	return &sessionTable{capacity: capacity, idle: idle, now: now, entries: make(map[sessionKey]*sessionEntry)}
}

func (table *sessionTable) get(key sessionKey) sessionCloser {
	table.mu.Lock()
	defer table.mu.Unlock()
	table.expireLocked(table.now())
	entry := table.entries[key]
	if entry == nil {
		return nil
	}
	entry.last = table.now()
	table.lru.MoveToBack(entry.lru)
	return entry.session
}

func (table *sessionTable) add(key sessionKey, session sessionCloser) {
	table.mu.Lock()
	defer table.mu.Unlock()
	now := table.now()
	table.expireLocked(now)
	if old := table.entries[key]; old != nil {
		table.removeLocked(old)
	}
	for len(table.entries) >= table.capacity {
		table.removeLocked(table.lru.Front().Value.(*sessionEntry))
	}
	entry := &sessionEntry{key: key, session: session, last: now}
	entry.lru = table.lru.PushBack(entry)
	table.entries[key] = entry
}

func (table *sessionTable) expire() {
	table.mu.Lock()
	defer table.mu.Unlock()
	table.expireLocked(table.now())
}

func (table *sessionTable) expireLocked(now time.Time) {
	for entry := table.lru.Front(); entry != nil; {
		next := entry.Next()
		value := entry.Value.(*sessionEntry)
		if !value.last.Add(table.idle).After(now) {
			table.removeLocked(value)
		}
		entry = next
	}
}

func (table *sessionTable) removeLocked(entry *sessionEntry) {
	delete(table.entries, entry.key)
	table.lru.Remove(entry.lru)
	if entry.session != nil {
		_ = entry.session.Close()
	}
}

func (table *sessionTable) len() int {
	table.mu.Lock()
	defer table.mu.Unlock()
	return len(table.entries)
}
