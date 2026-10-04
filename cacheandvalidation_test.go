package faceted

import "testing"

func TestEmptyFilterCountsCached(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "d1", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"m"}}})
	mustUpsert(t, ix, "d2", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"l"}}})

	res, err := ix.Search(Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Fatalf("total = %d, want 2", res.Total)
	}
	// The empty-filter count vector is cached under the empty key.
	if e, ok := ix.cache.get(""); !ok {
		t.Fatal("default query count vector not cached")
	} else if got := e.facetCounts["color"][nodeKey(FacetPath{"red"})]; got != 2 {
		t.Fatalf("cached red count = %d, want 2", got)
	}
}

func TestCacheInvalidationAcrossWriteKinds(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "d1", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"m"}}})
	mustUpsert(t, ix, "d2", Attributes{"color": {FacetPath{"blue"}}, "size": {FacetPath{"m"}}})

	filters := []Filter{
		{},
		{"color": {FacetPath{"red"}}},
		{"color": {FacetPath{"red"}}, "size": {FacetPath{"m"}}},
	}
	for _, f := range filters {
		if _, err := ix.Search(Query{Filter: f, Limit: 10}); err != nil {
			t.Fatal(err)
		}
		key := f.cacheKey()
		if _, ok := ix.cache.get(key); !ok {
			t.Fatalf("count vector for %q not warmed", key)
		}
	}

	// Upsert invalidates every vector, including empty filter.
	mustUpsert(t, ix, "d3", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"l"}}})
	for _, f := range filters {
		if _, ok := ix.cache.get(f.cacheKey()); ok {
			t.Fatalf("stale vector %q served after upsert", f.cacheKey())
		}
	}

	// Re-warm, then attribute update invalidates and new counts appear.
	res, err := ix.Search(Query{Filter: Filter{"color": {FacetPath{"red"}}}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Fatalf("total = %d, want 2", res.Total)
	}
	if err := ix.UpdateAttributes("d3", Attributes{
		"color": {FacetPath{"red"}},
		"size":  {FacetPath{"m"}},
	}); err != nil {
		t.Fatal(err)
	}
	res, err = ix.Search(Query{Filter: Filter{"color": {FacetPath{"red"}}}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Fatalf("total after update = %d, want 2", res.Total)
	}
	if got := facetMap(res)["size"]["m"].Count; got != 2 {
		t.Fatalf("size m = %d, want 2", got)
	}

	// Delete invalidates as well.
	ix.Delete("d3")
	res, err = ix.Search(Query{Filter: Filter{"color": {FacetPath{"red"}}}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 {
		t.Fatalf("total after delete = %d, want 1", res.Total)
	}
	assertCountsOverFullResultSet(t, ix, Filter{"color": {FacetPath{"red"}}}, res)
}

func TestInvalidRequestsRejected(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "d1", Attributes{"color": {FacetPath{"red"}}})

	bad := []Query{
		{Filter: Filter{"unknown": {FacetPath{"v"}}}, Limit: 10},
		{Filter: Filter{"color": {FacetPath{}}}, Limit: 10},
		{Filter: Filter{"color": {FacetPath{"a", "b"}}}, Limit: 10},
		{Offset: -1, Limit: 10},
		{Offset: 0, Limit: -1},
	}
	for i, q := range bad {
		if _, err := ix.Search(q); err == nil {
			t.Fatalf("bad query #%d accepted: %+v", i, q)
		}
	}

	// A rejected request must not corrupt state or other requests.
	res, err := ix.Search(Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 || len(res.Facets) != 3 {
		t.Fatalf("state after rejected requests: total %d facets %d", res.Total, len(res.Facets))
	}
}
