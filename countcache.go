package faceted

import "sync"

// countEntry stores the linked facet counts for one base filter.
// facetCounts[facet][nodeKey] is the candidate count of that node,
// computed over the base result set.
type countEntry struct {
	scope       *resultSet
	facetCounts map[string]map[string]int
}

// countCache memoizes linked counts by canonical filter key.
// Every successful write bumps the epoch, logically invalidating all
// entries in O(1); stale entries are lazily discarded.
type countCache struct {
	mu      sync.Mutex
	epoch   uint64
	entries map[string]epochEntry
	sets    map[string]epochSet
}

type epochEntry struct {
	epoch uint64
	entry *countEntry
}

type epochSet struct {
	epoch uint64
	set   *resultSet
}

func newCountCache() *countCache {
	return &countCache{
		entries: make(map[string]epochEntry),
		sets:    make(map[string]epochSet),
	}
}

// invalidate drops all cached entries on the next access.
func (c *countCache) invalidate() {
	c.mu.Lock()
	c.epoch++
	c.mu.Unlock()
}

func (c *countCache) get(key string) (*countEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || e.epoch != c.epoch {
		if ok {
			delete(c.entries, key)
		}
		return nil, false
	}
	return e.entry, true
}

func (c *countCache) put(key string, e *countEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = epochEntry{epoch: c.epoch, entry: e}
}

func (c *countCache) getSet(key string) (*resultSet, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.sets[key]
	if !ok || e.epoch != c.epoch {
		if ok {
			delete(c.sets, key)
		}
		return nil, false
	}
	return e.set, true
}

func (c *countCache) putSet(key string, s *resultSet) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sets[key] = epochSet{epoch: c.epoch, set: s}
}
