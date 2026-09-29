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
