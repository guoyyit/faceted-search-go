package faceted

import "testing"

func flatCounts(result SearchResult, facet string) map[string]int {
	counts := map[string]int{}
	for _, bucket := range result.Facets[facet].Buckets {
		counts[bucket.Value] = bucket.Count
	}
	return counts
}

func resultTitles(result SearchResult) []string {
	titles := make([]string, 0, len(result.Documents))
	for _, doc := range result.Documents {
		titles = append(titles, doc.Title)
	}
	return titles
}

func TestMultiSelectWithinFacetIsOr(t *testing.T) {
	engine := buildTestEngine(t)
	result := engine.Search(SearchParams{
		Limit:   10,
		Filters: map[string]Filter{"color": NewFilter("color", "red", "blue")},
	})
	if result.Total != 3 {
		t.Fatalf("total = %d, want 3", result.Total)
	}
	for _, doc := range result.Documents {
		if color := doc.Attrs["color"]; color != "red" && color != "blue" {
			t.Fatalf("document %q leaked through OR filter", doc.Title)
		}
	}

	// Counts are taken over the current result set itself: green is absent
	// while the two selected values remain.
	colorCounts := flatCounts(result, "color")
	if colorCounts["red"] != 2 || colorCounts["blue"] != 1 {
		t.Fatalf("selected facet counts = %v", colorCounts)
	}
	if _, present := colorCounts["green"]; present {
		t.Fatalf("green must not appear in a red+blue base set: %v", colorCounts)
	}
}

func TestDifferentFacetsAreAnd(t *testing.T) {
	engine := buildTestEngine(t)
	result := engine.Search(SearchParams{
		Limit: 10,
		Filters: map[string]Filter{
			"color": NewFilter("color", "red"),
			"size":  NewFilter("size", "l"),
		},
	})
	if result.Total != 1 {
		t.Fatalf("total = %d, want 1", result.Total)
	}
	if titles := resultTitles(result); len(titles) != 1 || titles[0] != "red hat" {
		t.Fatalf("titles = %v, want [red hat]", titles)
	}

	// Other facets are counted over documents satisfying every selection, so
	// size shows only l and color shows only red.
	sizeCounts := flatCounts(result, "size")
	if len(sizeCounts) != 1 || sizeCounts["l"] != 1 {
		t.Fatalf("size counts = %v, want only l:1", sizeCounts)
	}
	colorCounts := flatCounts(result, "color")
	if len(colorCounts) != 1 || colorCounts["red"] != 1 {
		t.Fatalf("color counts = %v, want only red:1", colorCounts)
	}
}

func TestCountsMatchPerDocumentEvaluation(t *testing.T) {
	engine := buildTestEngine(t)
	params := SearchParams{
		Limit: 10,
		Filters: map[string]Filter{
			"size": NewFilter("size", "m"),
		},
	}
	result := engine.Search(params)

	// Independent reference implementation: evaluate every document directly.
	referenceColor := map[string]int{}
	referenceCat := map[string]int{}
	matching := 0
	for _, doc := range engine.Search(SearchParams{Limit: 1000}).Documents {
		if doc.Attrs["size"] != "m" {
			continue
		}
		matching++
		referenceColor[doc.Attrs["color"]]++
		for _, ancestor := range ancestorsOf(doc.Attrs["cat"]) {
			referenceCat[ancestor]++
		}
	}
	if result.Total != matching || matching != 2 {
		t.Fatalf("total = %d, reference = %d", result.Total, matching)
	}
	colorCounts := flatCounts(result, "color")
	for value, want := range referenceColor {
		if colorCounts[value] != want {
			t.Fatalf("color %s = %d, want %d", value, colorCounts[value], want)
		}
	}
	tree := treeCounts(result.Facets["cat"].Tree)
	for value, want := range referenceCat {
		if tree[value] != want {
			t.Fatalf("cat %s = %d, want %d", value, tree[value], want)
		}
	}
}

func treeCounts(buckets []*FacetBucket) map[string]int {
	counts := map[string]int{}
	var walk func([]*FacetBucket)
	walk = func(nodes []*FacetBucket) {
		for _, node := range nodes {
			counts[node.Value] = node.Count
			walk(node.Children)
		}
	}
	walk(buckets)
	return counts
}

func TestCountsUnaffectedByPaging(t *testing.T) {
	engine := buildTestEngine(t)
	filters := map[string]Filter{"size": NewFilter("size", "m")}
	full := engine.Search(SearchParams{Limit: 10, Filters: filters})
	page := engine.Search(SearchParams{Offset: 0, Limit: 1, Filters: filters})
	if len(page.Documents) != 1 {
		t.Fatalf("page size = %d, want 1", len(page.Documents))
	}
	if page.Total != full.Total {
		t.Fatalf("paged total = %d, full total = %d", page.Total, full.Total)
	}
	for facet := range full.Facets {
		fullCounts := flatCounts(full, facet)
		pageCounts := flatCounts(page, facet)
		if len(pageCounts) != len(fullCounts) {
			t.Fatalf("facet %s counts changed under paging", facet)
		}
	}
}
