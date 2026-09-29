package faceted

import "testing"

// filteredEngine builds an index where one rare value matches exactly rareN
// documents out of total.
func filteredEngine(t *testing.T, total, rareN int) *Engine {
	t.Helper()
	engine := NewEngine([]FacetSpec{{Name: "color", Type: FacetFlat}})
	for i := 0; i < total; i++ {
		color := "common"
		if i < rareN {
			color = "rare"
		}
		if _, err := engine.Put(&Document{
			Title: "doc",
			Attrs: map[string]string{"color": color},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return engine
}

func TestCountScanBoundedByResultSet(t *testing.T) {
	const total = 10000
	const rare = 3
	engine := filteredEngine(t, total, rare)
	params := SearchParams{
		Limit:   1,
		Filters: map[string]Filter{"color": NewFilter("color", "rare")},
	}

	cold := engine.Search(params)
	if cold.Total != rare {
		t.Fatalf("total = %d, want %d", cold.Total, rare)
	}
	// Cold work is proportional to the matched set (one facet), not to the
	// whole corpus.
	if cold.DocsScanned > rare*len(engine.specs)+len(engine.specs) {
		t.Fatalf("cold DocsScanned = %d, want at most %d",
			cold.DocsScanned, rare*len(engine.specs)+len(engine.specs))
	}

	// A warm identical query performs no per-document counting at all.
	warm := engine.Search(params)
	if warm.DocsScanned != 0 {
		t.Fatalf("warm DocsScanned = %d, want 0", warm.DocsScanned)
	}

	// Counts stay identical and are independent of the one-document page.
	if counts := flatCounts(cold, "color"); counts["rare"] != rare {
		t.Fatalf("cold rare count = %d", counts["rare"])
	}
	if counts := flatCounts(warm, "color"); counts["rare"] != rare {
		t.Fatalf("warm rare count = %d", counts["rare"])
	}
}

func TestCacheInvalidatedByWrite(t *testing.T) {
	engine := filteredEngine(t, 100, 2)
	params := SearchParams{
		Limit:   10,
		Filters: map[string]Filter{"color": NewFilter("color", "common")},
	}
	first := engine.Search(params)
	if got := flatCounts(first, "color")["common"]; got != 98 {
		t.Fatalf("common before write = %d, want 98", got)
	}
	warm := engine.Search(params)
	if warm.DocsScanned != 0 {
		t.Fatalf("expected cache hit, scanned %d", warm.DocsScanned)
	}

	if _, err := engine.Put(&Document{
		Title: "new",
		Attrs: map[string]string{"color": "common"},
	}); err != nil {
		t.Fatal(err)
	}

	after := engine.Search(params)
	if got := flatCounts(after, "color")["common"]; got != 99 {
		t.Fatalf("common after put = %d, want 99 (stale cache returned)", got)
	}
	// Version bump and cache clearing make this a recomputation, not a hit.
	if after.Version == first.Version {
		t.Fatal("version must advance after write")
	}
	if after.DocsScanned == 0 {
		t.Fatal("stale cache entry survived a write")
	}
}

func TestCacheInvalidatedByUpdateAndDelete(t *testing.T) {
	engine := filteredEngine(t, 100, 2)

	commonParams := SearchParams{
		Limit:   10,
		Filters: map[string]Filter{"color": NewFilter("color", "common")},
	}
	rareParams := SearchParams{
		Limit:   10,
		Filters: map[string]Filter{"color": NewFilter("color", "rare")},
	}

	var rareID DocID
	for _, doc := range engine.Search(SearchParams{Limit: 1000}).Documents {
		if doc.Attrs["color"] == "rare" {
			rareID = doc.ID
			break
		}
	}

	engine.Search(commonParams)
	engine.Search(rareParams)

	if err := engine.Update(rareID, map[string]string{"color": "common"}); err != nil {
		t.Fatal(err)
	}
	updated := engine.Search(rareParams)
	if got := flatCounts(updated, "color")["rare"]; got != 1 {
		t.Fatalf("rare after update = %d, want 1", got)
	}
	updatedCommon := engine.Search(commonParams)
	if got := flatCounts(updatedCommon, "color")["common"]; got != 99 {
		t.Fatalf("common after update = %d, want 99", got)
	}

	if !engine.Delete(rareID) {
		t.Fatal("delete failed")
	}
	afterDelete := engine.Search(rareParams)
	if got := flatCounts(afterDelete, "color")["rare"]; got != 1 {
		t.Fatalf("rare after delete = %d, want 1 (one untouched rare doc)", got)
	}
}

func TestQueryAndFiltersHaveDistinctCacheKeys(t *testing.T) {
	engine := buildTestEngine(t)
	red := engine.Search(SearchParams{
		Limit:   10,
		Filters: map[string]Filter{"color": NewFilter("color", "red")},
	})
	shirt := engine.Search(SearchParams{
		Limit: 10,
		Query: "shirt",
	})
	if red.Total != 2 {
		t.Fatalf("red total = %d, want 2", red.Total)
	}
	if shirt.Total != 2 {
		t.Fatalf("shirt total = %d, want 2", shirt.Total)
	}
	// Re-running the red filter must return the cached red counts, not shirt's.
	redAgain := engine.Search(SearchParams{
		Limit:   10,
		Filters: map[string]Filter{"color": NewFilter("color", "red")},
	})
	if redAgain.Total != 2 {
		t.Fatalf("cached red total = %d, want 2", redAgain.Total)
	}
	for _, bucket := range redAgain.Facets["color"].Buckets {
		if bucket.Value == "red" && bucket.Count != 2 {
			t.Fatalf("red count = %d, want 2", bucket.Count)
		}
	}
}

func TestUnfilteredCountsNeverScanDocuments(t *testing.T) {
	engine := filteredEngine(t, 10000, 5)
	cold := engine.Search(SearchParams{Limit: 1})
	if cold.DocsScanned != 0 {
		t.Fatalf("unfiltered cold DocsScanned = %d, want 0", cold.DocsScanned)
	}
	if got := flatCounts(cold, "color")["common"]; got != 9995 {
		t.Fatalf("common count = %d, want 9995", got)
	}
	warm := engine.Search(SearchParams{Limit: 1})
	if warm.DocsScanned != 0 {
		t.Fatalf("unfiltered warm DocsScanned = %d, want 0", warm.DocsScanned)
	}
}
