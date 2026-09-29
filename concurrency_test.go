package faceted

import (
	"fmt"
	"sync"
	"testing"
)

func TestConcurrentReadersAndWriters(t *testing.T) {
	engine := NewEngine([]FacetSpec{
		{Name: "color", Type: FacetFlat},
		{Name: "cat", Type: FacetHierarchical},
	})

	const seed = 100
	for i := 0; i < seed; i++ {
		color := "c" + fmt.Sprint(i%4)
		if _, err := engine.Put(&Document{
			Title: fmt.Sprintf("doc %d", i),
			Attrs: map[string]string{
				"color": color,
				"cat":   "root/" + color + "/n" + fmt.Sprint(i%7),
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	stop := make(chan struct{})
	readerErrs := make(chan error, 16)
	var writers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		writers.Add(1)
		go func(worker int) {
			defer writers.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				switch i % 3 {
				case 0:
					engine.Put(&Document{
						Title: fmt.Sprintf("w%d-%d", worker, i),
						Attrs: map[string]string{
							"color": []string{"red", "blue", "green"}[i%3],
							"cat":   "root/" + []string{"red", "blue", "green"}[i%3] + "/x",
						},
					})
				case 1:
					docID := DocID(1 + (worker+i)%seed)
					engine.Update(docID, map[string]string{
						"color": []string{"red", "blue", "green", "yellow"}[i%4],
						"cat":   "root/" + []string{"red", "blue", "green", "yellow"}[i%4] + "/u",
					})
				case 2:
					engine.Delete(DocID(1 + (worker*7+i)%seed))
				}
			}
		}(worker)
	}

	var readers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		readers.Add(1)
		go func(worker int) {
			defer readers.Done()
			fail := func(format string, args ...any) {
				select {
				case readerErrs <- fmt.Errorf(format, args...):
				default:
				}
			}
			for i := 0; i < 500; i++ {
				filters := map[string]Filter{}
				if i%2 == 0 {
					filters["color"] = NewFilter("color", []string{"red", "blue"}[i%2])
				}
				result := engine.Search(SearchParams{
					Query:   "",
					Offset:  i % 20,
					Limit:   5,
					Filters: filters,
				})

				// Within one response, counts and the version cannot disagree:
				// a filter hit count must never exceed the reported total.
				for _, facetResult := range result.Facets {
					for _, bucket := range facetResult.Buckets {
						if bucket.Count > result.Total {
							fail("bucket %s count %d > total %d",
								bucket.Value, bucket.Count, result.Total)
						}
					}
					var walk func([]*FacetBucket)
					walk = func(nodes []*FacetBucket) {
						for _, node := range nodes {
							if node.Count > result.Total {
								fail("hierarchy node %s count %d > total %d",
									node.Value, node.Count, result.Total)
							}
							walk(node.Children)
						}
					}
					walk(facetResult.Tree)
				}

				// Every returned hit must itself satisfy the requested filter:
				// no mixed snapshot state can leak through.
				for _, doc := range result.Documents {
					for _, filter := range filters {
						value := doc.Attrs[filter.Facet()]
						facet := result.Facets[filter.Facet()]
						matched := false
						if facet != nil && facet.Type == FacetHierarchical {
							matched = hierarchicalMatch(value, filter.Values())
						} else {
							for _, selected := range filter.Values() {
								if value == selected {
									matched = true
								}
							}
						}
						if !matched {
							fail("doc %+v violates filter %s=%v",
								doc.Attrs, filter.Facet(), filter.Values())
						}
					}
				}
			}
		}(worker)
	}

	readers.Wait()
	close(stop)
	writers.Wait()
	close(readerErrs)
	for err := range readerErrs {
		t.Fatal(err)
	}

	// After quiescence the index must still return self-consistent responses.
	final := engine.Search(SearchParams{Limit: 1000})
	sum := 0
	for _, bucket := range final.Facets["color"].Buckets {
		sum += bucket.Count
	}
	if sum != final.Total {
		t.Fatalf("final color counts sum %d != total %d", sum, final.Total)
	}
}
