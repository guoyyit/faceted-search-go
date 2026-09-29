package faceted

import "testing"

func TestStablePagingHasNoGapsOrDuplicates(t *testing.T) {
	engine := NewEngine([]FacetSpec{{Name: "k", Type: FacetFlat}})
	const total = 47
	allIDs := make(map[DocID]bool, total)
	for i := 0; i < total; i++ {
		// Every document carries the same facet value and identical title, so
		// only the deterministic DocID tie-break can define page order.
		id, err := engine.Put(&Document{
			Title: "same title",
			Attrs: map[string]string{"k": "v"},
		})
		if err != nil {
			t.Fatal(err)
		}
		allIDs[id] = true
	}

	params := SearchParams{
		Limit:   10,
		Filters: map[string]Filter{"k": NewFilter("k", "v")},
	}
	seen := make(map[DocID]bool, total)
	var previous DocID
	for offset := 0; offset < total; offset += 10 {
		params.Offset = offset
		page := engine.Search(params)
		if page.Total != total {
			t.Fatalf("page at %d total = %d, want %d", offset, page.Total, total)
		}
		for _, doc := range page.Documents {
			if seen[doc.ID] {
				t.Fatalf("document %d repeated across pages", doc.ID)
			}
			if doc.ID <= previous && offset > 0 {
				t.Fatalf("page order not ascending at id %d", doc.ID)
			}
			seen[doc.ID] = true
			previous = doc.ID
		}
	}
	if len(seen) != total {
		t.Fatalf("paged %d unique documents, want %d", len(seen), total)
	}
}

func TestPagingSameParamsIsDeterministic(t *testing.T) {
	engine := NewEngine([]FacetSpec{{Name: "k", Type: FacetFlat}})
	for i := 0; i < 25; i++ {
		if _, err := engine.Put(&Document{
			Title: "same title",
			Attrs: map[string]string{"k": "v"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	params := SearchParams{Offset: 5, Limit: 7}
	first := engine.Search(params)
	second := engine.Search(params)
	if len(first.Documents) != len(second.Documents) {
		t.Fatalf("repeated pages differ in length")
	}
	for i := range first.Documents {
		if first.Documents[i].ID != second.Documents[i].ID {
			t.Fatalf("page order nondeterministic at position %d", i)
		}
	}
}

func TestPagingPastEnd(t *testing.T) {
	engine := buildTestEngine(t)
	result := engine.Search(SearchParams{Offset: 100, Limit: 10})
	if result.Total != 4 || len(result.Documents) != 0 {
		t.Fatalf("past-end page total=%d len=%d", result.Total, len(result.Documents))
	}
}
