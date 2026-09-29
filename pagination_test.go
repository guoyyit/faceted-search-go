package faceted

import (
	"sort"
	"testing"
)

func TestStablePaginationByID(t *testing.T) {
	ix := testIndex(t)
	const n = 200
	for i := 0; i < n; i++ {
		id := DocID("doc-" + padID(i))
		mustUpsert(t, ix, id, Attributes{"color": {FacetPath{"red"}}})
	}

	want := make([]string, 0, n)
	for i := 0; i < n; i++ {
		want = append(want, "doc-"+padID(i))
	}

	var got []string
	const page = 7
	for off := 0; ; off += page {
		res, err := ix.Search(Query{Filter: Filter{"color": {FacetPath{"red"}}}, Offset: off, Limit: page})
		if err != nil {
			t.Fatal(err)
		}
		if res.Total != n {
			t.Fatalf("total = %d, want %d", res.Total, n)
		}
		if len(res.IDs) == 0 {
			break
		}
		for _, id := range res.IDs {
			got = append(got, string(id))
		}
		if len(got) > n {
			t.Fatal("pagination produced more ids than documents")
		}
	}
	if len(got) != n {
		t.Fatalf("collected %d ids, want %d", len(got), n)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("position %d: got %s want %s", i, got[i], want[i])
		}
	}
}

func TestTieBreakerDeterministic(t *testing.T) {
	ix := testIndex(t)
	for _, id := range []DocID{"z1", "a2", "m3", "b4"} {
		mustUpsert(t, ix, id, Attributes{"size": {FacetPath{"m"}}})
		// Same body on purpose to exercise the deterministic id tiebreaker.
		ix.docs[id] = Document{ID: id, Body: "same", Attrs: ix.docs[id].Attrs}
	}

	var got []string
	for _, pageSize := range []int{2, 3, 5} {
		got = got[:0]
		for off := 0; ; off += pageSize {
			res, err := ix.Search(Query{
				Filter: Filter{"size": {FacetPath{"m"}}},
				Offset: off,
				Limit:  pageSize,
				Sort:   SortByBody,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.IDs) == 0 {
				break
			}
			for _, id := range res.IDs {
				got = append(got, string(id))
			}
		}
		want := []string{"a2", "b4", "m3", "z1"}
		if len(got) != len(want) {
			t.Fatalf("page size %d: got %v", pageSize, got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("page size %d: got %v want %v", pageSize, got, want)
			}
		}
	}

	// Empty filter pagination must also be stable across the whole index.
	res, err := ix.Search(Query{Offset: 0, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	all := make([]string, len(res.IDs))
	for i, id := range res.IDs {
		all[i] = string(id)
	}
	if !sort.StringsAreSorted(all) {
		t.Fatalf("default order not sorted: %v", all)
	}
}

func padID(i int) string {
	switch {
	case i < 10:
		return "00" + itoa(i)
	case i < 100:
		return "0" + itoa(i)
	default:
		return itoa(i)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
