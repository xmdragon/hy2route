package dnsproxy

import (
	"container/list"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

type cacheKey struct {
	name  string
	qtype uint16
}

type cacheEntry struct {
	response *dns.Msg
	expires  time.Time
	lru      *list.Element
}

type Cache struct {
	mu       sync.Mutex
	capacity int
	entries  map[cacheKey]*cacheEntry
	lru      list.List
}

func NewCache(capacity int) *Cache {
	if capacity < 1 {
		capacity = 1
	}
	return &Cache{capacity: capacity, entries: make(map[cacheKey]*cacheEntry)}
}

func (cache *Cache) Get(name string, qtype uint16, now time.Time) (*dns.Msg, bool) {
	key := cacheKey{name: strings.ToLower(name), qtype: qtype}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry := cache.entries[key]
	if entry == nil {
		return nil, false
	}
	if !entry.expires.After(now) {
		cache.removeLocked(key)
		return nil, false
	}
	cache.lru.MoveToBack(entry.lru)
	return entry.response.Copy(), true
}

func (cache *Cache) Put(name string, qtype uint16, response *dns.Msg, expires time.Time) {
	if response == nil || !expires.After(time.Time{}) {
		return
	}
	key := cacheKey{name: strings.ToLower(name), qtype: qtype}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if entry := cache.entries[key]; entry != nil {
		entry.response = response.Copy()
		entry.expires = expires
		cache.lru.MoveToBack(entry.lru)
		return
	}
	for len(cache.entries) >= cache.capacity {
		cache.removeLocked(cache.lru.Front().Value.(cacheKey))
	}
	entry := &cacheEntry{response: response.Copy(), expires: expires}
	entry.lru = cache.lru.PushBack(key)
	cache.entries[key] = entry
}

func (cache *Cache) removeLocked(key cacheKey) {
	entry := cache.entries[key]
	if entry == nil {
		return
	}
	cache.lru.Remove(entry.lru)
	delete(cache.entries, key)
}
