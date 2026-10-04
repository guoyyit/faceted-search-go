package faceted

import "testing"

// assertCountsLocked validates one response against the exact live state
// while the caller holds ix.mu read lock. Every facet block is checked,
// including facets that carry a selection.
func assertCountsLocked(tb testing.TB, ix *Index, f Filter, res SearchResult) {
	tb.Helper()
	matched := make(map[DocID]bool, res.Total)
	for id, doc := range ix.docs {
		if matchFilter(doc.Attrs, f) {
			matched[id] = true
		}
	}
	if res.Total != len(matched) {
		tb.Fatalf("total %d, replay %d", res.Total, len(matched))
	}
	for _, id := range res.IDs {
		if !matched[id] {
			tb.Fatalf("page id %s outside result set", id)
		}
	}
	if len(res.Facets) != len(ix.schema.order) {
		tb.Fatalf("got %d facet blocks, want %d", len(res.Facets), len(ix.schema.order))
	}

	for _, fc := range res.Facets {
		sum := 0
		singleValue := true
		shown := make(map[string]bool)
		for _, v := range fc.Values {
			if shown[nodeKey(v.Path)] {
				tb.Fatalf("duplicate candidate %s/%v", fc.Facet, v.Path)
			}
			shown[nodeKey(v.Path)] = true
			if v.Count < 0 || v.Count > res.Total {
				tb.Fatalf("facet %s %v count %d out of [0,%d]", fc.Facet, v.Path, v.Count, res.Total)
			}
			// Exact replay, including the drilled facet itself.
			want := 0
			for id := range matched {
				for _, assigned := range ix.docs[id].Attrs[fc.Facet] {
					if isAncestorOrEqual(v.Path, assigned) {
						want++
						break
					}
				}
			}
			if v.Count != want {
				tb.Fatalf("facet %s %v count %d, replay %d (facet selected: %v)",
					fc.Facet, v.Path, v.Count, want, f[fc.Facet])
			}
			sum += v.Count
			active := false
			for _, sel := range f[fc.Facet] {
				if facetsEqual(sel, v.Path) {
					active = true
				}
			}
			if v.Active != active {
				tb.Fatalf("facet %s %v Active=%v want %v", fc.Facet, v.Path, v.Active, active)
			}
			if v.Count == 0 && !active {
				tb.Fatalf("zero-count non-selected value %s/%v shown", fc.Facet, v.Path)
			}
			if fc.Kind == KindHierarchy {
				for _, other := range fc.Values {
					if isAncestorOrEqual(v.Path, other.Path) && other.Count > v.Count {
						tb.Fatalf("hierarchy node %v count %d < descendant %v count %d",
							v.Path, v.Count, other.Path, other.Count)
					}
				}
			}
		}
		for id := range matched {
			if fc.Kind != KindFlat || len(ix.docs[id].Attrs[fc.Facet]) != 1 {
				singleValue = false
			}
		}
		if singleValue && sum != res.Total {
			tb.Fatalf("facet %s counts sum %d != total %d (selected: %v)",
				fc.Facet, sum, res.Total, f[fc.Facet])
		}
	}
}

// assertCountsOverFullResultSet verifies the new count contract on one
// quiescent response: every facet (drilled facets included) is counted over
// the full filter result set, counts never exceed the total, every page id
// satisfies the filter, and for flat facets on which every matched
// document carries exactly one value, the candidate counts sum to the
// total. Hierarchy counts overlap along ancestor chains, so that sum does
// not apply there.
func assertCountsOverFullResultSet(t *testing.T, ix *Index, f Filter, res SearchResult) {
	t.Helper()
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	assertCountsLocked(t, ix, f, res)
}

// searchAndValidate runs one query and, while still holding the same read
// lock, replays the exact per-document semantics over the live snapshot and
// validates every facet block (drilled facets included). It proves the
// page, total and all counts share one update boundary.
func searchAndValidate(tb testing.TB, ix *Index, q Query) SearchResult {
	tb.Helper()
	f := q.Filter.Clone()
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	res, err := ix.searchLocked(f, q)
	if err != nil {
		tb.Fatalf("search: %v", err)
	}
	assertCountsLocked(tb, ix, f, res)
	return res
}

func TestDrilledFacetCountsOverFullResultSet(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "d1", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"m"}}})
	mustUpsert(t, ix, "d2", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"l"}}})
	mustUpsert(t, ix, "d3", Attributes{"color": {FacetPath{"blue"}}, "size": {FacetPath{"m"}}})
	mustUpsert(t, ix, "d4", Attributes{"color": {FacetPath{"green"}}, "size": {FacetPath{"l"}}})

	f := Filter{"color": {FacetPath{"red"}}}
	res, err := ix.Search(Query{Filter: f, Offset: 0, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Fatalf("total = %d, want 2", res.Total)
	}
	color := facetMap(res)["color"]
	if color["red"].Count != 2 || !color["red"].Active {
		t.Fatalf("selected red = %+v, want count 2 active", color["red"])
	}
	if _, ok := color["blue"]; ok {
		t.Fatal("blue must be hidden over the color=red result set")
	}
	if _, ok := color["green"]; ok {
		t.Fatal("green must be hidden over the color=red result set")
	}
	assertCountsOverFullResultSet(t, ix, f, res)
}

func TestDefaultQueryReturnsCountsWithoutSwitch(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "d1", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"m"}}})
	mustUpsert(t, ix, "d2", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"l"}}})

	// No ReturnCounts, no explicit switch: page, total and counts together.
	res, err := ix.Search(Query{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Fatalf("total = %d, want 2", res.Total)
	}
	if len(res.IDs) != 1 {
		t.Fatalf("page size = %d, want 1", len(res.IDs))
	}
	if len(res.Facets) != 3 {
		t.Fatalf("default query returned %d facet blocks, want 3", len(res.Facets))
	}
	color := facetMap(res)["color"]
	if color["red"].Count != 2 {
		t.Fatalf("default red count = %d, want 2", color["red"].Count)
	}
}

func TestCountsIndependentOfPageWindow(t *testing.T) {
	ix := testIndex(t)
	for i := 0; i < 12; i++ {
		mustUpsert(t, ix, DocID("d"+itoa(i)), Attributes{
			"color": {FacetPath{pickColor(i)}},
			"size":  {FacetPath{pickSize(i)}},
		})
	}
	f := Filter{}
	var first map[string]map[string]int
	for k, q := range []Query{
		{Filter: f, Offset: 0, Limit: 1},
		{Filter: f, Offset: 5, Limit: 3},
		{Filter: f, Offset: 0, Limit: 100},
	} {
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
		if k == 0 {
			first = snap
		} else if !mapsEqual(first, snap) {
			t.Fatalf("counts differ by window: %+v vs %+v", first, snap)
		}
	}
}

func TestMultiSelectCountsOverFullResultSet(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "d1", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"m"}}})
	mustUpsert(t, ix, "d2", Attributes{"color": {FacetPath{"blue"}}, "size": {FacetPath{"m"}}})
	mustUpsert(t, ix, "d3", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"l"}}})
	mustUpsert(t, ix, "d4", Attributes{"color": {FacetPath{"green"}}, "size": {FacetPath{"l"}}})

	// Same-facet OR: color in {red,blue} -> 3 docs; the drilled facet
	// itself sums to 3 over that result set, green is hidden.
	f := Filter{"color": {FacetPath{"red"}, FacetPath{"blue"}}}
	res, err := ix.Search(Query{Filter: f, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 3 {
		t.Fatalf("total = %d, want 3", res.Total)
	}
	color := facetMap(res)["color"]
	if color["red"].Count != 2 || color["blue"].Count != 1 {
		t.Fatalf("drilled color counts = red %d blue %d", color["red"].Count, color["blue"].Count)
	}
	if !color["red"].Active || !color["blue"].Active {
		t.Fatal("selected values must be active")
	}
	if _, ok := color["green"]; ok {
		t.Fatal("green must be hidden")
	}
	assertCountsOverFullResultSet(t, ix, f, res)

	// Cross-facet AND plus same-facet OR.
	f = Filter{
		"color": {FacetPath{"red"}, FacetPath{"blue"}},
		"size":  {FacetPath{"m"}},
	}
	res, err = ix.Search(Query{Filter: f, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Fatalf("cross-facet total = %d, want 2", res.Total)
	}
	assertCountsOverFullResultSet(t, ix, f, res)
}

func TestMultiValueDocCountedOnceOverFullSet(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "d1", Attributes{
		"color": {FacetPath{"red"}, FacetPath{"blue"}},
		"size":  {FacetPath{"m"}},
	})
	mustUpsert(t, ix, "d2", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"m"}}})
	mustUpsert(t, ix, "d3", Attributes{"color": {FacetPath{"blue"}}, "size": {FacetPath{"l"}}})

	f := Filter{"color": {FacetPath{"red"}}}
	res, err := ix.Search(Query{Filter: f, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Fatalf("total = %d, want 2", res.Total)
	}
	color := facetMap(res)["color"]
	// d1 carries red and blue; over the red result set blue counts d1 once.
	if color["red"].Count != 2 || color["blue"].Count != 1 {
		t.Fatalf("color counts = red %d blue %d", color["red"].Count, color["blue"].Count)
	}
	if color["blue"].Active {
		t.Fatal("blue is not selected")
	}
	// size: d1 and d2 are both m, deduped within the candidate value.
	size := facetMap(res)["size"]
	if size["m"].Count != 2 {
		t.Fatalf("size m = %d, want 2", size["m"].Count)
	}
	assertCountsOverFullResultSet(t, ix, f, res)

	// Selecting both red and blue: each candidate counts docs containing
	// it within the full (3-doc) result set; no candidate exceeds total.
	f = Filter{"color": {FacetPath{"red"}, FacetPath{"blue"}}}
	res, err = ix.Search(Query{Filter: f, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 3 {
		t.Fatalf("total = %d, want 3", res.Total)
	}
	color = facetMap(res)["color"]
	if color["red"].Count != 2 || color["blue"].Count != 2 {
		t.Fatalf("color counts = red %d blue %d", color["red"].Count, color["blue"].Count)
	}
}

func TestHierarchyCountsOverFullResultSet(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "p1", Attributes{"cat": {FacetPath{"electronics", "phones", "smartphone"}}})
	mustUpsert(t, ix, "p2", Attributes{"cat": {FacetPath{"electronics", "phones", "smartphone"}}})
	mustUpsert(t, ix, "p3", Attributes{"cat": {FacetPath{"electronics", "phones", "featurephone"}}})
	mustUpsert(t, ix, "c1", Attributes{"cat": {FacetPath{"electronics", "computers", "laptop"}}})
	mustUpsert(t, ix, "b1", Attributes{"cat": {FacetPath{"books", "novel"}}})

	// Leaf selection: ancestors show only the selected subtree share.
	f := Filter{"cat": {FacetPath{"electronics", "phones", "smartphone"}}}
	res, err := ix.Search(Query{Filter: f, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	cat := facetMap(res)["cat"]
	if cat["electronics"].Count != 2 || cat["electronics\x1fphones"].Count != 2 ||
		cat["electronics\x1fphones\x1fsmartphone"].Count != 2 {
		t.Fatalf("child-selected ancestor counts = %+v", cat)
	}
	if _, ok := cat["electronics\x1fphones\x1ffeaturephone"]; ok {
		t.Fatal("zero sibling must be hidden")
	}
	if _, ok := cat["books\x1fnovel"]; ok {
		t.Fatal("unrelated branch must be hidden")
	}
	assertCountsOverFullResultSet(t, ix, f, res)

	// Parent selection: every non-empty child subtree shows its own count.
	f = Filter{"cat": {FacetPath{"electronics"}}}
	res, err = ix.Search(Query{Filter: f, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	cat = facetMap(res)["cat"]
	want := map[string]int{
		"electronics":                           4,
		"electronics\x1fphones":                 3,
		"electronics\x1fphones\x1fsmartphone":   2,
		"electronics\x1fphones\x1ffeaturephone": 1,
		"electronics\x1fcomputers":              1,
		"electronics\x1fcomputers\x1flaptop":    1,
	}
	for key, n := range want {
		if cat[key].Count != n {
			t.Fatalf("cat[%q] = %d, want %d", key, cat[key].Count, n)
		}
	}
	assertCountsOverFullResultSet(t, ix, f, res)

	// Disjoint subtree multi-select.
	f = Filter{"cat": {
		FacetPath{"electronics", "phones", "smartphone"},
		FacetPath{"books"},
	}}
	res, err = ix.Search(Query{Filter: f, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 3 {
		t.Fatalf("disjoint subtree total = %d, want 3", res.Total)
	}
	cat = facetMap(res)["cat"]
	if cat["electronics"].Count != 2 || cat["books"].Count != 1 ||
		cat["books\x1fnovel"].Count != 1 {
		t.Fatalf("multi-select hierarchy counts = %+v", cat)
	}
	assertCountsOverFullResultSet(t, ix, f, res)
}

func TestSelectedValueWithZeroCountStillReturned(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "d1", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"m"}}})
	mustUpsert(t, ix, "d2", Attributes{"color": {FacetPath{"red"}}, "size": {FacetPath{"l"}}})

	// Selecting red and size=m yields one doc; size=l is not selected and
	// must be hidden, while the stale extra selection color=blue stays
	// visible, count 0, marked active.
	f := Filter{
		"color": {FacetPath{"red"}, FacetPath{"blue"}},
		"size":  {FacetPath{"m"}},
	}
	res, err := ix.Search(Query{Filter: f, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 {
		t.Fatalf("total = %d, want 1", res.Total)
	}
	color := facetMap(res)["color"]
	if color["blue"].Count != 0 || !color["blue"].Active {
		t.Fatalf("stale blue selection = %+v, want count 0 active", color["blue"])
	}
	if _, ok := facetMap(res)["size"]["l"]; ok {
		t.Fatal("non-selected zero value must be hidden")
	}

	// A fully unmatched selection still returns the active zero candidate.
	f = Filter{"color": {FacetPath{"black"}}}
	res, err = ix.Search(Query{Filter: f, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 0 {
		t.Fatalf("total = %d, want 0", res.Total)
	}
	black := facetMap(res)["color"]["black"]
	if black.Count != 0 || !black.Active {
		t.Fatalf("black = %+v, want count 0 active", black)
	}

	// Hierarchy stale selection on an unknown node.
	f = Filter{"cat": {FacetPath{"nope", "leaf"}}}
	res, err = ix.Search(Query{Filter: f, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	cat := facetMap(res)["cat"]
	if v := cat["nope\x1fleaf"]; v.Count != 0 || !v.Active {
		t.Fatalf("unknown cat leaf = %+v, want count 0 active", v)
	}
	if _, ok := cat["nope"]; ok {
		t.Fatal("zero-count ancestor of a stale selection is not selected and must be hidden")
	}
}
