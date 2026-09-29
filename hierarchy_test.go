package faceted

import "testing"

func findBucket(buckets []*FacetBucket, value string) *FacetBucket {
	for _, bucket := range buckets {
		if bucket.Value == value {
			return bucket
		}
		if found := findBucket(bucket.Children, value); found != nil {
			return found
		}
	}
	return nil
}

func TestHierarchicalUnfilteredRollUp(t *testing.T) {
	engine := buildTestEngine(t)
	result := engine.Search(SearchParams{Limit: 10})
	tree := treeCounts(result.Facets["cat"].Tree)

	// Parent counts are distinct-document roll-ups: "b" covers two documents.
	want := map[string]int{
		"a":            2,
		"a/red-wear":   1,
		"a/blue-wear":  1,
		"b":            2,
		"b/red-hat":    1,
		"b/shoe":       1,
		"b/shoe/green": 1,
	}
	for value, count := range want {
		if tree[value] != count {
			t.Fatalf("node %q count = %d, want %d (tree=%v)", value, tree[value], count, tree)
		}
	}
}

func TestSelectParentRollsUpChildren(t *testing.T) {
	engine := buildTestEngine(t)
	result := engine.Search(SearchParams{
		Limit:   10,
		Filters: map[string]Filter{"cat": NewFilter("cat", "b")},
	})
	if result.Total != 2 {
		t.Fatalf("selecting parent b total = %d, want 2", result.Total)
	}
	for _, doc := range result.Documents {
		if !hierarchicalMatch(doc.Attrs["cat"], []string{"b"}) {
			t.Fatalf("document %q outside subtree b", doc.Title)
		}
	}

	bucket := findBucket(result.Facets["cat"].Tree, "b")
	if bucket == nil || !bucket.Selected {
		t.Fatalf("parent node b should be marked selected")
	}
	child := findBucket(result.Facets["cat"].Tree, "b/shoe/green")
	if child == nil || child.Count != 1 {
		t.Fatalf("child b/shoe/green count = %+v, want 1", child)
	}
}

func TestSelectChildShowsParentRollUp(t *testing.T) {
	engine := buildTestEngine(t)
	result := engine.Search(SearchParams{
		Limit:   10,
		Filters: map[string]Filter{"cat": NewFilter("cat", "b/shoe/green")},
	})
	if result.Total != 1 {
		t.Fatalf("selecting leaf total = %d, want 1", result.Total)
	}

	// The parent presentation rule: ancestors of a selected descendant remain
	// visible and carry the rolled-up count over the current result set (1).
	// Ancestor nodes are not themselves marked selected.
	tree := result.Facets["cat"].Tree
	parent := findBucket(tree, "b")
	if parent == nil || parent.Count != 1 || parent.Selected {
		t.Fatalf("parent b = %+v, want visible count 1 not selected", parent)
	}
	mid := findBucket(tree, "b/shoe")
	if mid == nil || mid.Count != 1 || mid.Selected {
		t.Fatalf("parent b/shoe = %+v, want visible count 1 not selected", mid)
	}
	leaf := findBucket(tree, "b/shoe/green")
	if leaf == nil || leaf.Count != 1 || !leaf.Selected {
		t.Fatalf("leaf = %+v, want count 1 selected", leaf)
	}
}

func TestSiblingSelectionIsOr(t *testing.T) {
	engine := buildTestEngine(t)
	result := engine.Search(SearchParams{
		Limit: 10,
		Filters: map[string]Filter{
			"cat": NewFilter("cat", "a/red-wear", "b/red-hat"),
		},
	})
	if result.Total != 2 {
		t.Fatalf("sibling OR total = %d, want 2", result.Total)
	}
}

func TestHierarchyCrossFacetAnd(t *testing.T) {
	engine := buildTestEngine(t)
	result := engine.Search(SearchParams{
		Limit: 10,
		Filters: map[string]Filter{
			"cat":   NewFilter("cat", "b"),
			"color": NewFilter("color", "red"),
		},
	})
	if result.Total != 1 {
		t.Fatalf("hierarchy x flat AND total = %d, want 1", result.Total)
	}
	if titles := resultTitles(result); titles[0] != "red hat" {
		t.Fatalf("titles = %v, want [red hat]", titles)
	}
}
