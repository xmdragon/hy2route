package policy

import (
	"container/heap"
	"container/list"
	"errors"
	"net/netip"
	"sync"
	"time"
)

type Observation struct {
	Domain  string
	Action  Action
	IPs     []netip.Addr
	Expires time.Time
}

type LearnedDecision struct {
	Action  Action
	Direct  bool
	Proxy   bool
	Inspect bool
}

type observationKey struct {
	domain string
	action Action
	ip     netip.Addr
}

type learningObservation struct {
	expires time.Time
	lru     *list.Element
}

type expiryItem struct {
	key     observationKey
	expires time.Time
}

type expiryHeap []expiryItem

func (items expiryHeap) Len() int { return len(items) }

func (items expiryHeap) Less(left, right int) bool {
	return items[left].expires.Before(items[right].expires)
}

func (items expiryHeap) Swap(left, right int) { items[left], items[right] = items[right], items[left] }

func (items *expiryHeap) Push(value any) { *items = append(*items, value.(expiryItem)) }

func (items *expiryHeap) Pop() any {
	old := *items
	last := len(old) - 1
	value := old[last]
	*items = old[:last]
	return value
}

type LearningTable struct {
	mu          sync.Mutex
	capacity    int
	entries     map[observationKey]*learningObservation
	byIP        map[netip.Addr]map[observationKey]struct{}
	expirations expiryHeap
	lru         list.List
}

func NewLearningTable(capacity int) *LearningTable {
	if capacity < 1 {
		capacity = 1
	}
	table := &LearningTable{
		capacity: capacity,
		entries:  make(map[observationKey]*learningObservation),
		byIP:     make(map[netip.Addr]map[observationKey]struct{}),
	}
	heap.Init(&table.expirations)
	return table
}

func (table *LearningTable) Observe(observation Observation) error {
	if observation.Action != Direct && observation.Action != Proxy {
		return errors.New("learning action must be direct or proxy")
	}
	if observation.Expires.IsZero() {
		return errors.New("learning expiry is required")
	}
	domain, err := NormalizeDomain(observation.Domain)
	if err != nil {
		return err
	}
	seen := make(map[netip.Addr]struct{}, len(observation.IPs))
	table.mu.Lock()
	defer table.mu.Unlock()
	for _, ip := range observation.IPs {
		if !ip.Is4() || ip.Is4In6() {
			return errors.New("learned IP must be IPv4")
		}
		if _, duplicate := seen[ip]; duplicate {
			continue
		}
		seen[ip] = struct{}{}
		key := observationKey{domain: domain, action: observation.Action, ip: ip}
		if existing := table.entries[key]; existing != nil {
			existing.expires = observation.Expires
			table.lru.MoveToBack(existing.lru)
		} else {
			for len(table.entries) >= table.capacity {
				table.removeLocked(table.lru.Front().Value.(observationKey))
			}
			entry := &learningObservation{expires: observation.Expires}
			entry.lru = table.lru.PushBack(key)
			table.entries[key] = entry
			if table.byIP[ip] == nil {
				table.byIP[ip] = make(map[observationKey]struct{})
			}
			table.byIP[ip][key] = struct{}{}
		}
		heap.Push(&table.expirations, expiryItem{key: key, expires: observation.Expires})
	}
	return nil
}

func (table *LearningTable) Lookup(ip netip.Addr, now time.Time) LearnedDecision {
	table.mu.Lock()
	defer table.mu.Unlock()
	table.expireLocked(now)
	return table.lookupLocked(ip)
}

func (table *LearningTable) Expire(now time.Time) []netip.Addr {
	table.mu.Lock()
	defer table.mu.Unlock()
	return table.expireLocked(now)
}

func (table *LearningTable) Snapshot(now time.Time) map[netip.Addr]LearnedDecision {
	table.mu.Lock()
	defer table.mu.Unlock()
	table.expireLocked(now)
	result := make(map[netip.Addr]LearnedDecision, len(table.byIP))
	for ip := range table.byIP {
		result[ip] = table.lookupLocked(ip)
	}
	return result
}

func (table *LearningTable) lookupLocked(ip netip.Addr) LearnedDecision {
	var result LearnedDecision
	for key := range table.byIP[ip] {
		switch key.action {
		case Direct:
			result.Direct = true
		case Proxy:
			result.Proxy = true
		}
	}
	if result.Proxy {
		result.Action = Proxy
	} else if result.Direct {
		result.Action = Direct
	}
	result.Inspect = result.Direct && result.Proxy
	return result
}

func (table *LearningTable) expireLocked(now time.Time) []netip.Addr {
	changed := make(map[netip.Addr]struct{})
	for table.expirations.Len() > 0 && !table.expirations[0].expires.After(now) {
		item := heap.Pop(&table.expirations).(expiryItem)
		entry := table.entries[item.key]
		if entry == nil || !entry.expires.Equal(item.expires) {
			continue
		}
		changed[item.key.ip] = struct{}{}
		table.removeLocked(item.key)
	}
	result := make([]netip.Addr, 0, len(changed))
	for ip := range changed {
		result = append(result, ip)
	}
	return result
}

func (table *LearningTable) removeLocked(key observationKey) {
	entry := table.entries[key]
	if entry == nil {
		return
	}
	table.lru.Remove(entry.lru)
	delete(table.entries, key)
	delete(table.byIP[key.ip], key)
	if len(table.byIP[key.ip]) == 0 {
		delete(table.byIP, key.ip)
	}
}
