package faceted

import (
	"fmt"
	"testing"
)

func benchmarkSelectiveWarmQuery(b *testing.B, n int) {
	ix := testIndex(b)
	for i := 0; i < n; i++ {
		id := DocID(fmt.Sprintf("d%08d", i))
		color := "green"
		if i == n-1 || i == n-2 {
			color = "red"
		}
		if err := ix.Upsert(Document{
			ID:    id,
			Body:  string(id),
			Attrs: Attributes{"color": {FacetPath{color}}, "size": {FacetPath{pickSize(i)}}},
		}); err != nil {
			b.Fatal(err)
		}
	}
	f := Filter{"color": {FacetPath{"red"}}}
	q := Query{Filter: f, Offset: 0, Limit: 10, ReturnCounts: true}
	// Warm the caches once.
	if _, err := ix.Search(q); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		res, err := ix.Search(q)
		if err != nil {
			b.Fatal(err)
		}
		if res.Total != 2 {
			b.Fatalf("total = %d, want 2", res.Total)
		}
	}
}

func BenchmarkSelectiveWarmQuery_10k(b *testing.B) {
	benchmarkSelectiveWarmQuery(b, 10_000)
}

func BenchmarkSelectiveWarmQuery_100k(b *testing.B) {
	benchmarkSelectiveWarmQuery(b, 100_000)
}

func benchmarkDefaultWarmQuery(b *testing.B, n int) {
	ix := testIndex(b)
	for i := 0; i < n; i++ {
		id := DocID(fmt.Sprintf("d%08d", i))
		if err := ix.Upsert(Document{
			ID:    id,
			Body:  string(id),
			Attrs: Attributes{"color": {FacetPath{pickColor(i)}}, "size": {FacetPath{pickSize(i)}}},
		}); err != nil {
			b.Fatal(err)
		}
	}
	// No filter and no explicit switch: the default response must still
	// return counts. Counts come from the cached vector on the warm path.
	q := Query{Offset: 0, Limit: 10}
	res, err := ix.Search(q)
	if err != nil {
		b.Fatal(err)
	}
	if len(res.Facets) == 0 {
		b.Fatal("default query must return facet counts")
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		res, err := ix.Search(q)
		if err != nil {
			b.Fatal(err)
		}
		if res.Total != n {
			b.Fatalf("total = %d, want %d", res.Total, n)
		}
	}
}

func BenchmarkDefaultWarmQuery_10k(b *testing.B) {
	benchmarkDefaultWarmQuery(b, 10_000)
}

func BenchmarkDefaultWarmQuery_100k(b *testing.B) {
	benchmarkDefaultWarmQuery(b, 100_000)
}
