package faceted

import (
	"math/rand"
	"sort"
	"testing"
)

func TestTreapInsertDeletePage(t *testing.T) {
	tr := newIDTreap()
	n := 500
	ids := make([]DocID, 0, n)
	for i := 0; i < n; i++ {
		id := DocID("k" + padID(i))
		ids = append(ids, id)
	}
	rng := rand.New(rand.NewSource(1))
	rng.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })

	for _, id := range ids {
		tr.add(id)
		tr.add(id) // duplicates ignored
	}
	if tr.len() != n {
		t.Fatalf("len = %d, want %d", tr.len(), n)
	}

	var got []DocID
	for off := 0; ; off += 37 {
		page := tr.page(off, 37)
		if len(page) == 0 {
			break
		}
		got = append(got, page...)
	}
	want := append([]DocID(nil), ids...)
	sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
	if len(got) != len(want) {
		t.Fatalf("paged %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("at %d got %s want %s", i, got[i], want[i])
		}
	}

	// Delete half, including non-existent keys.
	for i := 0; i < n; i += 2 {
		tr.remove(want[i])
		tr.remove(want[i])
	}
	if tr.len() != n/2 {
		t.Fatalf("len after delete = %d, want %d", tr.len(), n/2)
	}
	page := tr.page(0, n)
	if len(page) != n/2 {
		t.Fatalf("remaining page = %d", len(page))
	}
	for i := 1; i < len(page); i++ {
		if page[i-1] >= page[i] {
			t.Fatal("remaining keys not ordered")
		}
	}
}

func TestTreapUnionIntersect(t *testing.T) {
	a := newIDTreap()
	b := newIDTreap()
	for _, id := range []DocID{"1", "2", "3", "4", "5"} {
		a.add(id)
	}
	for _, id := range []DocID{"4", "5", "6", "7"} {
		b.add(id)
	}

	u := unionTreaps(a, b)
	if u.len() != 7 {
		t.Fatalf("union size = %d, want 7", u.len())
	}
	up := u.page(0, 100)
	wantU := []DocID{"1", "2", "3", "4", "5", "6", "7"}
	for i := range wantU {
		if up[i] != wantU[i] {
			t.Fatalf("union[%d] = %s, want %s", i, up[i], wantU[i])
		}
	}
	// Inputs must stay intact (they may be shared cached postings).
	if a.len() != 5 || b.len() != 4 {
		t.Fatalf("union mutated inputs: a=%d b=%d", a.len(), b.len())
	}

	inter := intersectTreaps(a, b)
	if inter.len() != 2 {
		t.Fatalf("intersection size = %d, want 2", inter.len())
	}
	ip := inter.page(0, 100)
	if ip[0] != "4" || ip[1] != "5" {
		t.Fatalf("intersection = %v", ip)
	}
	if a.len() != 5 || b.len() != 4 {
		t.Fatal("intersection mutated inputs")
	}
}
