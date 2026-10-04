package faceted

import "sync"

// countEntry stores the candidate count vector for one base filter.
// facetCounts[facet][nodeKey] is the number of result-set documents
// carrying that value (flat) or assigned to that node or a descendant
// (hierarchy). The vector is computed over the filter's full result set.
type countEntry struct {
	facetCounts map[string]map[string]int
}

// countCache memoizes result sets and count vectors by canonical filter
// key. Every successful write bumps the epoch, logically invalidating all
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
