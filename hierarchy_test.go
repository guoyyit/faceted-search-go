package faceted

import "testing"

// Hierarchy semantics under test (also documented in README.md):
// values assigned to a document roll up, so selecting a parent matches
// every descendant-assigned document. Every facet, including the drilled
// one, is counted over the full result set; displayed nodes show subtree
// union counts; child selection makes ancestors present only the selected
// subtree total; selecting parent and descendant together unions them and
// the ancestor dominates.
func TestHierarchyRollupAndFilter(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "p1", Attributes{"cat": {FacetPath{"electronics", "phones", "smartphone"}}})
	mustUpsert(t, ix, "p2", Attributes{"cat": {FacetPath{"electronics", "phones", "smartphone"}}})
	mustUpsert(t, ix, "p3", Attributes{"cat": {FacetPath{"electronics", "phones", "featurephone"}}})
	mustUpsert(t, ix, "c1", Attributes{"cat": {FacetPath{"electronics", "computers", "laptop"}}})
	mustUpsert(t, ix, "b1", Attributes{"cat": {FacetPath{"books", "novel"}}})

	res, err := ix.Search(Query{
		Filter:       Filter{"cat": {FacetPath{"electronics"}}},
		Limit:        10,
		ReturnCounts: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 4 {
		t.Fatalf("parent rollup total = %d, want 4", res.Total)
	}
	cat := facetMap(res)["cat"]
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
			t.Fatalf("cat[%q] = %d, want %d (all: %+v)", key, cat[key].Count, n, cat)
		}
	}
	if _, shown := cat["books\x1fnovel"]; shown {
		t.Fatal("zero-count unrelated branch should be hidden")
	}
	if !cat["electronics"].Active {
		t.Fatal("selected parent should be active")
	}
}

func TestHierarchyChildSelectionParentPresentation(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "p1", Attributes{"cat": {FacetPath{"electronics", "phones", "smartphone"}}})
	mustUpsert(t, ix, "p2", Attributes{"cat": {FacetPath{"electronics", "phones", "smartphone"}}})
	mustUpsert(t, ix, "p3", Attributes{"cat": {FacetPath{"electronics", "phones", "featurephone"}}})
	mustUpsert(t, ix, "c1", Attributes{"cat": {FacetPath{"electronics", "computers", "laptop"}}})

	res, err := ix.Search(Query{
		Filter:       Filter{"cat": {FacetPath{"electronics", "phones", "smartphone"}}},
		Limit:        10,
		ReturnCounts: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Fatalf("leaf total = %d, want 2", res.Total)
	}
	cat := facetMap(res)["cat"]
	if cat["electronics"].Count != 2 {
		t.Fatalf("electronics count = %d, want 2", cat["electronics"].Count)
	}
	if cat["electronics\x1fphones"].Count != 2 {
		t.Fatalf("phones count = %d, want 2", cat["electronics\x1fphones"].Count)
	}
	if cat["electronics\x1fphones\x1fsmartphone"].Count != 2 {
		t.Fatal("selected leaf count mismatch")
	}
	if !cat["electronics\x1fphones\x1fsmartphone"].Active {
		t.Fatal("selected leaf not active")
	}
	if _, ok := cat["electronics\x1fphones\x1ffeaturephone"]; ok {
		t.Fatal("zero sibling should be hidden")
	}
	if _, ok := cat["electronics\x1fcomputers\x1flaptop"]; ok {
		t.Fatal("zero unrelated branch should be hidden")
	}
}

func TestHierarchyParentSelectionLinkedChildren(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "p1", Attributes{
		"cat":   {FacetPath{"electronics", "phones", "smartphone"}},
		"color": {FacetPath{"red"}},
	})
	mustUpsert(t, ix, "p2", Attributes{
		"cat":   {FacetPath{"electronics", "phones", "featurephone"}},
		"color": {FacetPath{"blue"}},
	})
	mustUpsert(t, ix, "c1", Attributes{
		"cat":   {FacetPath{"electronics", "computers", "laptop"}},
		"color": {FacetPath{"red"}},
	})

	res, err := ix.Search(Query{
		Filter: Filter{
			"color": {FacetPath{"red"}},
			"cat":   {FacetPath{"electronics"}},
		},
		Limit:        10,
		ReturnCounts: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Fatalf("total = %d, want 2", res.Total)
	}
	cat := facetMap(res)["cat"]
	if cat["electronics\x1fphones\x1fsmartphone"].Count != 1 {
		t.Fatalf("smartphone linked count = %d, want 1", cat["electronics\x1fphones\x1fsmartphone"].Count)
	}
	if _, ok := cat["electronics\x1fphones\x1ffeaturephone"]; ok {
		t.Fatal("featurephone must be hidden after linked filter")
	}
	if cat["electronics\x1fcomputers\x1flaptop"].Count != 1 {
		t.Fatal("laptop linked count should be 1")
	}
}

func TestHierarchyParentAndChildUnion(t *testing.T) {
	ix := testIndex(t)
	mustUpsert(t, ix, "p1", Attributes{"cat": {FacetPath{"electronics", "phones", "smartphone"}}})
	mustUpsert(t, ix, "p2", Attributes{"cat": {FacetPath{"electronics", "phones", "featurephone"}}})

	res, err := ix.Search(Query{
		Filter: Filter{"cat": {
			FacetPath{"electronics"},
			FacetPath{"electronics", "phones", "smartphone"},
		}},
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 {
		t.Fatalf("parent+child union total = %d, want 2 (parent dominates)", res.Total)
	}
}
