package faceted

import "sync"

// facetCountResult is a cached facet-count computation. counts maps every
// relevant value (including every ancestor for hierarchical facets) to the
// number of distinct matching documents. docsScanned is the size of the
// candidate set examined, exposed by searches so tests can prove the work is
// independent of the total corpus size once a filter narrows the universe.
type facetCountResult struct {
	counts      map[string]int
	docsScanned int
}

// cacheKey uniquely identifies a facet-count computation within a version.
type cacheKey struct {
	facet     string
	othersKey string
}

// countCache memoizes facet counts for a single snapshot version. Any write
// bumps the version, which invalidates every cached entry.
type countCache struct {
	mu      sync.RWMutex
	version uint64
	entries map[cacheKey]*facetCountResult
}

func newCountCache() *countCache {
	return &countCache{entries: make(map[cacheKey]*facetCountResult)}
}

// get returns a cached result. Every entry belongs to one snapshot version; a
// version mismatch behaves as a cache miss.
func (c *countCache) get(version uint64, facet, othersKey string) (*facetCountResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.version != version {
		return nil, false
	}
	result, ok := c.entries[cacheKey{facet: facet, othersKey: othersKey}]
	return result, ok
}

func (c *countCache) put(version uint64, facet, othersKey string, result *facetCountResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.version != version {
		return
	}
	c.entries[cacheKey{facet: facet, othersKey: othersKey}] = result
}

// invalidate drops every entry belonging to older versions.
func (c *countCache) invalidate(version uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.version == version && len(c.entries) == 0 {
		return
	}
	c.version = version
	c.entries = make(map[cacheKey]*facetCountResult)
}
