package faceted

import "testing"

func TestPutAssignsIncrementalIDs(t *testing.T) {
	engine := NewEngine(nil)
	id1, err := engine.Put(&Document{Title: "one"})
	if err != nil || id1 != 1 {
		t.Fatalf("first id = %d err=%v, want 1", id1, err)
	}
	id2, _ := engine.Put(&Document{Title: "two"})
	if id2 != 2 {
		t.Fatalf("second id = %d, want 2", id2)
	}
}

func TestDelete(t *testing.T) {
	engine := buildTestEngine(t)
	before := engine.Search(SearchParams{Limit: 100})
	if before.Total != 4 {
		t.Fatalf("before total = %d", before.Total)
	}
	id := before.Documents[0].ID
	if !engine.Delete(id) {
		t.Fatal("delete returned false for existing doc")
	}
	if engine.Delete(id) {
		t.Fatal("second delete returned true")
	}
	after := engine.Search(SearchParams{Limit: 100})
	if after.Total != 3 {
		t.Fatalf("after total = %d, want 3", after.Total)
	}
	for _, doc := range after.Documents {
		if doc.ID == id {
			t.Fatal("deleted document still returned")
		}
	}
	if counts := flatCounts(after, "color"); counts["red"] != 1 {
		t.Fatalf("red count after delete = %d, want 1", counts["red"])
	}
}

func TestUpdateAttrsMovesFacetPostings(t *testing.T) {
	engine := buildTestEngine(t)
	result := engine.Search(SearchParams{Limit: 100})
	var redID DocID
	for _, doc := range result.Documents {
		if doc.Title == "red shirt" {
			redID = doc.ID
		}
	}

	if err := engine.Update(redID, map[string]string{
		"color": "green",
		"size":  "s",
		"cat":   "a/green-shirt",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}

	after := engine.Search(SearchParams{
		Limit:   100,
		Filters: map[string]Filter{"color": NewFilter("color", "red")},
	})
	if after.Total != 1 {
		t.Fatalf("red matches after update = %d, want 1 (red hat only)", after.Total)
	}
	green := engine.Search(SearchParams{
		Limit:   100,
		Filters: map[string]Filter{"color": NewFilter("color", "green")},
	})
	if green.Total != 2 {
		t.Fatalf("green matches after update = %d, want 2", green.Total)
	}

	// Text fields are untouched by an attribute update.
	shirt := engine.Search(SearchParams{
		Limit:   100,
		Query:   "shirt",
		Filters: map[string]Filter{"color": NewFilter("color", "green")},
	})
	if shirt.Total != 1 || shirt.Documents[0].Title != "red shirt" {
		t.Fatalf("updated doc text search = %+v", shirt.Documents)
	}

	// Old hierarchical values must no longer carry the document.
	oldA := engine.Search(SearchParams{
		Limit:   100,
		Filters: map[string]Filter{"cat": NewFilter("cat", "a/red-wear")},
	})
	if oldA.Total != 0 {
		t.Fatalf("old hierarchy node matches = %d, want 0", oldA.Total)
	}
}

func TestUpdateMissingDocument(t *testing.T) {
	engine := buildTestEngine(t)
	if err := engine.Update(999, map[string]string{"color": "x"}); err == nil {
		t.Fatal("update of missing document must error")
	}
}

func TestPutReplacesDocument(t *testing.T) {
	engine := buildTestEngine(t)
	result := engine.Search(SearchParams{Limit: 100})
	original := result.Documents[0]

	replacement := &Document{
		ID:    original.ID,
		Title: "totally new content",
		Body:  "replacement",
		Attrs: map[string]string{"color": "black", "size": "xs", "cat": "z/new"},
	}
	id, err := engine.Put(replacement)
	if err != nil || id != original.ID {
		t.Fatalf("replace id = %d err = %v, want %d", id, err, original.ID)
	}
	after := engine.Search(SearchParams{Limit: 100, Query: "replacement"})
	if after.Total != 1 {
		t.Fatalf("replacement not searchable, total = %d", after.Total)
	}
	if after.Version == result.Version {
		t.Fatal("version must advance after replacement")
	}
}
