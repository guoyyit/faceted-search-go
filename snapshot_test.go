package faceted

import "testing"

func TestCountsConsistentWithFullResultSet(t *testing.T) {
	engine := buildTestEngine(t)
	cases := []SearchParams{
		{Limit: 1},
		{
			Limit:   1,
			Filters: map[string]Filter{"size": NewFilter("size", "m")},
		},
		{
			Limit:   2,
			Filters: map[string]Filter{"color": NewFilter("color", "red", "blue")},
		},
		{
			Limit: 1,
			Filters: map[string]Filter{
				"cat":  NewFilter("cat", "b"),
				"size": NewFilter("size", "l"),
			},
		},
	}

	for index, params := range cases {
		// Request all matching documents to compare counts against the exact
		// documents the engine itself returns under the same filters.
		fullParams := params
		fullParams.Limit = 1000
		full := engine.Search(fullParams)

		// Paged response uses the same counts.
		paged := engine.Search(params)
		if paged.Version != full.Version {
			t.Fatalf("case %d: version mismatch", index)
		}
		if paged.Total != full.Total {
			t.Fatalf("case %d: paged total %d != full total %d",
				index, paged.Total, full.Total)
		}

		// Flat facet counts must sum to the full result set.
		colorCounts := flatCounts(full, "color")
		sum := 0
		for _, count := range colorCounts {
			sum += count
		}
		if sum != full.Total {
			t.Fatalf("case %d: color counts sum %d != total %d",
				index, sum, full.Total)
		}

		// Root hierarchy nodes partition the full set too.
		rootSum := 0
		for _, root := range full.Facets["cat"].Tree {
			rootSum += root.Count
		}
		if rootSum != full.Total {
			t.Fatalf("case %d: hierarchy root sum %d != total %d",
				index, rootSum, full.Total)
		}

		// Every returned document satisfies every filter.
		for _, doc := range full.Documents {
			for name, filter := range params.Filters {
				value := doc.Attrs[name]
				facet := full.Facets[name]
				ok := false
				if facet != nil && facet.Type == FacetHierarchical {
					ok = hierarchicalMatch(value, filter.Values())
				} else {
					for _, selected := range filter.Values() {
						if value == selected {
							ok = true
						}
					}
				}
				if !ok {
					t.Fatalf("case %d: doc %q attr %s=%q violates filter",
						index, doc.Title, name, value)
				}
			}
		}
	}
}
