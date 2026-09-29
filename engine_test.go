package faceted

import "testing"

func buildTestEngine(t *testing.T) *Engine {
	t.Helper()
	engine := NewEngine([]FacetSpec{
		{Name: "color", Type: FacetFlat},
		{Name: "size", Type: FacetFlat},
		{Name: "cat", Type: FacetHierarchical},
	})
	docs := []*Document{
		{Title: "red shirt", Attrs: map[string]string{"color": "red", "size": "m", "cat": "a/red-wear"}},
		{Title: "blue shirt", Attrs: map[string]string{"color": "blue", "size": "m", "cat": "a/blue-wear"}},
		{Title: "red hat", Attrs: map[string]string{"color": "red", "size": "l", "cat": "b/red-hat"}},
		{Title: "green shoe", Attrs: map[string]string{"color": "green", "size": "l", "cat": "b/shoe/green"}},
	}
	for _, doc := range docs {
		if _, err := engine.Put(doc); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	return engine
}

func TestSmokeCounts(t *testing.T) {
	engine := buildTestEngine(t)
	result := engine.Search(SearchParams{Limit: 10})

	if result.Total != 4 {
		t.Fatalf("total = %d, want 4", result.Total)
	}
	color := result.Facets["color"]
	got := map[string]int{}
	for _, bucket := range color.Buckets {
		got[bucket.Value] = bucket.Count
	}
	if got["red"] != 2 || got["blue"] != 1 || got["green"] != 1 {
		t.Fatalf("unfiltered color counts = %v", got)
	}
}
