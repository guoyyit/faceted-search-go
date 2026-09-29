package faceted

import "testing"

func searchWithCounts(t *testing.T, ix *Index, f Filter) SearchResult {
	t.Helper()
	res, err := ix.Search(Query{Filter: f, Limit: 100, ReturnCounts: true})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestCountCacheInvalidation(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "d1", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"m"}}})
	mustUpsert(t, ix, "d2", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"l"}}})

	f := Filter{"color": {FacetPath{"red"}}}
	res := searchWithCounts(t, ix, f)
	if res.Total != 2 {
		t.Fatalf("total = %d, want 2", res.Total)
	}

	// The count vector and the result set must be cached for the frequent
	// combination.
	key := f.cacheKey()
	if _, ok := ix.cache.get(key); !ok {
		t.Fatal("count vector not cached after warm query")
	}
	if _, ok := ix.cache.getSet(key); !ok {
		t.Fatal("result set not cached after warm query")
	}

	// Insert a document that changes counts: every cached entry must be
	// invalidated and the next query must observe fresh numbers.
	mustUpsert(t, ix, "d3", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"m"}}})
	if _, ok := ix.cache.get(key); ok {
		t.Fatal("stale count vector served after write")
	}
	if _, ok := ix.cache.getSet(key); ok {
		t.Fatal("stale result set served after write")
	}
	res = searchWithCounts(t, ix, f)
	if res.Total != 3 {
		t.Fatalf("total after insert = %d, want 3", res.Total)
	}
	if got := facetMap(res)["size"]["m"].Count; got != 2 {
		t.Fatalf("size=m count = %d, want 2", got)
	}

	// Attribute update invalidates as well.
	if err := ix.UpdateAttributes("d3", Attributes{
		"color": {FacetPath{"blue"}},
		"size":  {FacetPath{"l"}},
	}); err != nil {
		t.Fatal(err)
	}
	res = searchWithCounts(t, ix, f)
	if res.Total != 2 {
		t.Fatalf("total after attribute update = %d, want 2", res.Total)
	}
	if got := facetMap(res)["size"]["l"].Count; got != 1 {
		t.Fatalf("size=l count = %d, want 1", got)
	}

	// Delete invalidates as well.
	ix.Delete("d1")
	res = searchWithCounts(t, ix, f)
	if res.Total != 1 {
		t.Fatalf("total after delete = %d, want 1", res.Total)
	}
}

func TestCacheKeyStable(t *testing.T) {
	a := Filter{
		"color": {FacetPath{"red"}, FacetPath{"blue"}},
		"size":  {FacetPath{"m"}},
	}
	b := Filter{
		"size":  {FacetPath{"m"}},
		"color": {FacetPath{"blue"}, FacetPath{"red"}},
	}
	if a.cacheKey() != b.cacheKey() {
		t.Fatalf("canonical keys differ:\n%q\n%q", a.cacheKey(), b.cacheKey())
	}

	c := Filter{"color": {FacetPath{"red"}}}
	if a.cacheKey() == c.cacheKey() {
		t.Fatal("distinct filters share a cache key")
	}
}

// TestWarmQueryScaling checks the requirement that a count-bearing query
// for a selective combination must not scan the whole collection on the
// warm path. After the first query populates the cache, growing the index
// with unrelated documents must not add work proportional to that growth.
// We assert this structurally through the cached result sets rather than
// relying on wall-clock benchmarks.
func TestWarmQueryDoesNotTouchWholeCollection(t *testing.T) {
	ix := testIndex(t)
	for i := 0; i < 500; i++ {
		id := DocID("bulk-" + padID(i/10) + "-" + string(rune('a'+i%10)))
		mustUpsert(t, ix, id, Attributes{"color": {FacetPath{"green"}}})
	}
	mustUpsert(t, ix, "rare-1", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"m"}}})
	mustUpsert(t, ix, "rare-2", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"l"}}})

	f := Filter{"color": {FacetPath{"red"}}}
	searchWithCounts(t, ix, f)
	rs, ok := ix.cache.getSet(f.cacheKey())
	if !ok {
		t.Fatal("expected cached result set")
	}
	if rs.set.len() != 2 {
		t.Fatalf("cached set size = %d, want 2", rs.set.len())
	}
	if rs.tree.len() != 2 {
		t.Fatalf("cached tree size = %d, want 2", rs.tree.len())
	}

	// Adding lots of unrelated documents invalidates the cache (epoch),
	// but a re-warmed single-value query still builds a tree over only the
	// matched posting list, not the whole collection.
	for i := 0; i < 500; i++ {
		id := DocID("more-" + padID(i/10) + "-" + string(rune('a'+i%10)))
		mustUpsert(t, ix, id, Attributes{"color": {FacetPath{"green"}}})
	}
	searchWithCounts(t, ix, f)
	rs, ok = ix.cache.getSet(f.cacheKey())
	if !ok || rs.tree.len() != 2 {
		t.Fatalf("rebuilt set wrong: ok=%v size=%d", ok, rs.tree.len())
	}
}
