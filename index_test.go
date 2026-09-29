package faceted

import (
	"sort"
	"testing"
)

type fataler interface {
	Helper()
	Fatal(args ...any)
	Fatalf(format string, args ...any)
}

func testIndex(t fataler) *Index {
	t.Helper()
	s := NewSchema()
	if err := s.AddFlat("color"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddFlat("size"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHierarchy("cat"); err != nil {
		t.Fatal(err)
	}
	return NewIndex(s)
}

func mustUpsert(t fataler, ix *Index, id DocID, attrs Attributes) {
	t.Helper()
	if err := ix.Upsert(Document{ID: id, Body: string(id), Attrs: attrs}); err != nil {
		t.Fatalf("upsert %s: %v", id, err)
	}
}

func facetMap(res SearchResult) map[string]map[string]FacetValueCount {
	out := make(map[string]map[string]FacetValueCount)
	for _, fc := range res.Facets {
		m := make(map[string]FacetValueCount)
		for _, v := range fc.Values {
			m[nodeKey(v.Path)] = v
		}
		out[fc.Facet] = m
	}
	return out
}

func resultIDs(res SearchResult) map[DocID]bool {
	m := make(map[DocID]bool, res.Total)
	for _, id := range res.IDs {
		m[id] = true
	}
	return m
}

func TestUpsertDeleteUpdate(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "d1", Attributes{
		"color": {FacetPath{"red"}},
		"size":  {FacetPath{"m"}},
	})
	if ix.Len() != 1 {
		t.Fatalf("len = %d", ix.Len())
	}

	if err := ix.UpdateAttributes("d1", Attributes{"color": {FacetPath{"blue"}}}); err != nil {
		t.Fatal(err)
	}
	res, err := ix.Search(Query{
		Filter:       Filter{"color": {FacetPath{"red"}}},
		Limit:        10,
		ReturnCounts: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 0 {
		t.Fatalf("red count after update = %d, want 0", res.Total)
	}

	if !ix.Delete("d1") {
		t.Fatal("delete returned false")
	}
	if ix.Len() != 0 || ix.Delete("d1") {
		t.Fatal("delete state wrong")
	}
	if err := ix.UpdateAttributes("missing", nil); err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestValidation(t *testing.T) {
	ix := testIndex(t)
	if err := ix.Upsert(Document{ID: "", Attrs: nil}); err == nil {
		t.Fatal("empty id accepted")
	}
	if err := ix.Upsert(Document{
		ID:    "x",
		Attrs: Attributes{"nope": {FacetPath{"v"}}},
	}); err == nil {
		t.Fatal("unknown facet accepted")
	}
	if err := ix.Upsert(Document{
		ID:    "x",
		Attrs: Attributes{"color": {FacetPath{"a", "b"}}},
	}); err == nil {
		t.Fatal("multi-component path accepted for flat facet")
	}
	if _, err := ix.Search(Query{Filter: Filter{"nope": {FacetPath{"v"}}}}); err == nil {
		t.Fatal("unknown filter facet accepted")
	}
}

func TestMultiSelectORAndCrossFacetAND(t *testing.T) {
	ix := testIndex(t)
	// d1 red/m, d2 blue/m, d3 red/l, d4 green/l
	mustUpsert(t, ix, "d1", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"m"}}})
	mustUpsert(t, ix, "d2", Attributes{"color": {FacetPath{"blue"}}, "size": {FacetPath{"m"}}})
	mustUpsert(t, ix, "d3", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"l"}}})
	mustUpsert(t, ix, "d4", Attributes{"color": {FacetPath{"green"}}, "size": {FacetPath{"l"}}})

	res, err := ix.Search(Query{
		Filter:       Filter{"color": {FacetPath{"red"}, FacetPath{"blue"}}},
		Limit:        10,
		ReturnCounts: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 3 {
		t.Fatalf("OR total = %d, want 3", res.Total)
	}
	fm := facetMap(res)
	// size facet counts must be linked to the red/blue result: m->2, l->1.
	if fm["size"]["m"].Count != 2 || fm["size"]["l"].Count != 1 {
		t.Fatalf("linked size counts = %+v", fm["size"])
	}
	// The drilled facet still shows alternative counts over other facets'
	// selection, so green is absent (0) while red/blue are active.
	if fm["color"]["red"].Count != 2 || !fm["color"]["red"].Active {
		t.Fatalf("red count = %+v", fm["color"]["red"])
	}
	if fm["color"]["blue"].Count != 1 || !fm["color"]["blue"].Active {
		t.Fatalf("blue count = %+v", fm["color"]["blue"])
	}

	res, err = ix.Search(Query{
		Filter: Filter{
			"color": {FacetPath{"red"}, FacetPath{"blue"}},
			"size":  {FacetPath{"m"}},
		},
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Fatalf("cross-facet AND total = %d, want 2", res.Total)
	}

	// Cross-check against the per-document reference semantics.
	for _, id := range []DocID{"d1", "d2", "d3", "d4"} {
		doc := ix.docs[id]
		got := resultIDs(res)[id]
		want := matchFilter(doc.Attrs, Filter{
			"color": {FacetPath{"red"}, FacetPath{"blue"}},
			"size":  {FacetPath{"m"}},
		})
		if got != want {
			t.Fatalf("doc %s mismatch: got %v want %v", id, got, want)
		}
	}
}

func TestCountsMatchFullResultSet(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "d1", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"m"}}})
	mustUpsert(t, ix, "d2", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"l"}}})
	mustUpsert(t, ix, "d3", Attributes{"color": {FacetPath{"blue"}}, "size": {FacetPath{"m"}}})

	// Counts must be identical regardless of the page window.
	queries := []Query{
		{Filter: Filter{"color": {FacetPath{"red"}}}, Offset: 0, Limit: 1, ReturnCounts: true},
		{Filter: Filter{"color": {FacetPath{"red"}}}, Offset: 1, Limit: 1, ReturnCounts: true},
		{Filter: Filter{"color": {FacetPath{"red"}}}, Offset: 0, Limit: 10, ReturnCounts: true},
	}
	var first map[string]map[string]int
	for i, q := range queries {
		res, err := ix.Search(q)
		if err != nil {
			t.Fatal(err)
		}
		snap := make(map[string]map[string]int)
		for _, fc := range res.Facets {
			m := map[string]int{}
			for _, v := range fc.Values {
				m[nodeKey(v.Path)] = v.Count
			}
			snap[fc.Facet] = m
		}
		if i == 0 {
			first = snap
		} else if !mapsEqual(first, snap) {
			t.Fatalf("counts differ by page: %+v vs %+v", first, snap)
		}
	}

	// Linked size counts over red result: m->1, l->1.
	if first["size"]["m"] != 1 || first["size"]["l"] != 1 {
		t.Fatalf("linked counts = %+v", first["size"])
	}
}

func mapsEqual(a, b map[string]map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || len(va) != len(vb) {
			return false
		}
		for kk, x := range va {
			if vb[kk] != x {
				return false
			}
		}
	}
	return true
}

func TestMultiValuedDocCountedOnce(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "d1", Attributes{
		"color": {FacetPath{"red"}, FacetPath{"blue"}},
		"size":  {FacetPath{"m"}},
	})
	mustUpsert(t, ix, "d2", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"m"}}})

	res, err := ix.Search(Query{
		Filter:       Filter{"color": {FacetPath{"red"}, FacetPath{"blue"}}},
		Limit:        10,
		ReturnCounts: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Fatalf("total = %d, want 2 (no double count)", res.Total)
	}
	ids := resultIDs(res)
	if len(ids) != 2 || !ids["d1"] || !ids["d2"] {
		t.Fatalf("ids = %+v", ids)
	}
	if facetMap(res)["size"]["m"].Count != 2 {
		t.Fatal("multi-valued doc counted twice in linked counts")
	}
	_ = sort.Strings // keep import stable across edits
}
