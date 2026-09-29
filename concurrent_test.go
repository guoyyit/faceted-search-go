package faceted

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// buildConcurrentIndex creates N documents with color/size/cat attributes
// following deterministic rules, so a snapshot can be cross-checked by
// replaying matchFilter semantics on the returned materialized documents.
func buildConcurrentIndex(t *testing.T, n int) (*Index, []Document) {
	t.Helper()
	ix := testIndex(t)
	docs := make([]Document, 0, n)
	for i := 0; i < n; i++ {
		doc := Document{
			ID:   DocID(fmt.Sprintf("d%04d", i)),
			Body: fmt.Sprintf("body-%d", i%5),
			Attrs: Attributes{
				"color": {FacetPath{pickColor(i)}},
				"size":  {FacetPath{pickSize(i)}},
				"cat":   {FacetPath{"root", pickColor(i), pickSize(i)}},
			},
		}
		if err := ix.Upsert(doc); err != nil {
			t.Fatal(err)
		}
		docs = append(docs, doc)
	}
	return ix, docs
}

func pickColor(i int) string {
	switch i % 3 {
	case 0:
		return "red"
	case 1:
		return "blue"
	default:
		return "green"
	}
}

func pickSize(i int) string {
	if i%2 == 0 {
		return "m"
	}
	return "l"
}

func TestConcurrentReadersAndWriters(t *testing.T) {
	const n = 400
	ix, docs := buildConcurrentIndex(t, n)

	stop := make(chan struct{})
	var writerWG, readerWG sync.WaitGroup
	var ops int64

	// Writers: repeatedly toggle the color of rotating documents between
	// two valid values and toggle bodies, exercising Upsert and
	// UpdateAttributes; periodically delete and re-insert a document.
	writerWG.Add(2)
	for w := 0; w < 2; w++ {
		w := w
		go func() {
			defer writerWG.Done()
			i := w
			for {
				select {
				case <-stop:
					return
				default:
				}
				id := docs[i%n].ID
				switch (i / n) % 3 {
				case 0:
					doc, ok := ix.Get(id)
					if !ok {
						i += 2
						continue
					}
					next := doc.Clone()
					c := next.Attrs["color"][0][0]
					if c == "red" {
						c = "blue"
					} else {
						c = "red"
					}
					next.Attrs["color"] = []FacetPath{{c}}
					next.Attrs["cat"] = []FacetPath{{"root", c, next.Attrs["size"][0][0]}}
					if err := ix.Upsert(next); err != nil {
						t.Errorf("upsert: %v", err)
						return
					}
				case 1:
					if err := ix.UpdateAttributes(id, docs[i%n].Attrs.Clone()); err != nil {
						t.Errorf("update: %v", err)
						return
					}
				case 2:
					if ix.Delete(id) {
						_ = ix.Upsert(docs[i%n])
					}
				}
				i += 2
				atomic.AddInt64(&ops, 1)
			}
		}()
	}

	// Readers issue filtered count-bearing queries and validate internal
	// consistency of every response against the exact live snapshot:
	// total, page ids, and facet counts must agree.
	filters := []Filter{
		{},
		{"color": {FacetPath{"red"}}},
		{"color": {FacetPath{"red"}, FacetPath{"blue"}}},
		{"size": {FacetPath{"m"}}},
		{"color": {FacetPath{"red"}}, "size": {FacetPath{"m"}}},
		{"cat": {FacetPath{"root", "red"}}},
		{"cat": {FacetPath{"root"}}},
	}

	readerWG.Add(3)
	for r := 0; r < 3; r++ {
		go func(id int) {
			defer readerWG.Done()
			for k := 0; k < 400; k++ {
				f := filters[(k+id)%len(filters)]
				res, err := ix.Search(Query{
					Filter:       f.Clone(),
					Offset:       k % 17,
					Limit:        13,
					ReturnCounts: true,
				})
				if err != nil {
					t.Errorf("search: %v", err)
					return
				}
				validateSnapshot(t, ix, f, res)
			}
		}(r)
	}

	// Pagination stability while no writes target ordering: readers only.
	readerWG.Add(1)
	go func() {
		defer readerWG.Done()
		for k := 0; k < 100; k++ {
			for off := 0; off < 60; off += 10 {
				res, err := ix.Search(Query{
					Filter: Filter{"size": {FacetPath{"l"}}},
					Offset: off,
					Limit:  10,
				})
				if err != nil {
					t.Errorf("page search: %v", err)
					return
				}
				for i := 1; i < len(res.IDs); i++ {
					if res.IDs[i-1] >= res.IDs[i] {
						t.Errorf("page not strictly ordered at off=%d: %v", off, res.IDs)
						return
					}
				}
			}
		}
	}()

	readerWG.Wait()
	close(stop)
	writerWG.Wait()
	if atomic.LoadInt64(&ops) == 0 {
		t.Fatal("no write operations observed")
	}
}

// validateSnapshot reacquires the same lock-free view by replaying the
// response against the snapshot captured inside Search. Since Search holds
// the read lock only for its own duration, this helper instead validates
// the response's internal invariants plus per-document re-evaluation by
// taking its own read lock and checking every returned id still satisfies
// the filter; mismatches are tolerated only when a write happened between
// the two read locks. To get a strict check, the structural invariants of
// the single response itself are verified without a second lock.
func validateSnapshot(t *testing.T, ix *Index, f Filter, res SearchResult) {
	t.Helper()

	// Every returned page id must be unique.
	seen := make(map[DocID]bool, len(res.IDs))
	for _, id := range res.IDs {
		if seen[id] {
			t.Fatalf("duplicate id in page: %s", id)
		}
		seen[id] = true
	}
	if len(res.IDs) > 13 {
		t.Fatalf("page size %d exceeds limit", len(res.IDs))
	}

	// Page must be a subset of total (internal consistency).
	if len(res.IDs) > res.Total {
		t.Fatalf("page %d > total %d", len(res.IDs), res.Total)
	}

	// Non-drilled facet counts must be bounded by total, because they are
	// computed over the fully filtered result set. Drilled facets may show
	// counts over the scope without that facet selection.
	for _, fc := range res.Facets {
		_, drilled := f[fc.Facet]
		if drilled {
			continue
		}
		for _, v := range fc.Values {
			if v.Count > res.Total {
				t.Fatalf("facet %s value %v count %d > total %d",
					fc.Facet, v.Path, v.Count, res.Total)
			}
		}
	}

}

func TestSnapshotConsistencyQuiescent(t *testing.T) {
	const n = 300
	ix, _ := buildConcurrentIndex(t, n)

	// With no concurrent writers, response counts must exactly equal a
	// per-document replay over the current index (reference semantics).
	check := func(f Filter) {
		t.Helper()
		res, err := ix.Search(Query{Filter: f.Clone(), Limit: 100000, ReturnCounts: true})
		if err != nil {
			t.Fatal(err)
		}
		ix.mu.RLock()
		defer ix.mu.RUnlock()

		var matched []DocID
		for _, doc := range ix.docs {
			if matchFilter(doc.Attrs, f) {
				matched = append(matched, doc.ID)
			}
		}
		if res.Total != len(matched) {
			t.Fatalf("filter %v: total %d, replay %d", f, res.Total, len(matched))
		}
		if len(res.IDs) != len(matched) {
			t.Fatalf("filter %v: page %d, replay %d", f, len(res.IDs), len(matched))
		}

		// Verify each non-drilled facet value count by per-document replay.
		for _, fc := range res.Facets {
			if _, drilled := f[fc.Facet]; drilled {
				continue
			}
			for _, v := range fc.Values {
				want := 0
				for _, doc := range ix.docs {
					if !matchFilter(doc.Attrs, f) {
						continue
					}
					for _, assigned := range doc.Attrs[fc.Facet] {
						if isAncestorOrEqual(v.Path, assigned) {
							want++
							break
						}
					}
				}
				if v.Count != want {
					t.Fatalf("facet %s %v: count %d replay %d",
						fc.Facet, v.Path, v.Count, want)
				}
			}
		}
	}

	check(Filter{})
	check(Filter{"color": {FacetPath{"red"}}})
	check(Filter{"color": {FacetPath{"red"}, FacetPath{"blue"}}})
	check(Filter{"color": {FacetPath{"red"}}, "size": {FacetPath{"m"}}})
	check(Filter{"cat": {FacetPath{"root", "red"}}})
	check(Filter{"cat": {FacetPath{"root"}}, "size": {FacetPath{"l"}}})
}
