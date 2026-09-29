package faceted

import "sort"

// snapshot is an immutable point-in-time view of the whole index. Readers
// never mutate it: writers produce a cloned snapshot and atomically swap it.
type snapshot struct {
	version uint64

	// docs maps DocID to the stored document.
	docs map[DocID]*Document

	// text maps a normalized token to the sorted DocIDs containing it.
	text map[string][]DocID

	// postings maps facet name -> attribute value -> sorted matching DocIDs.
	// For hierarchical facets each document also appears at every ancestor
	// value so that parent selections roll up descendant documents.
	postings map[string]map[string][]DocID

	// order lists every live DocID in ascending DocID order; it is the stable
	// tie-breaker for paging.
	order []DocID
}

func newSnapshot() *snapshot {
	return &snapshot{
		docs:     make(map[DocID]*Document),
		text:     make(map[string][]DocID),
		postings: make(map[string]map[string][]DocID),
	}
}

// clone returns a shallow, independently mutable copy of the snapshot.
func (s *snapshot) clone() *snapshot {
	docs := make(map[DocID]*Document, len(s.docs))
	for id, doc := range s.docs {
		docs[id] = doc
	}
	text := make(map[string][]DocID, len(s.text))
	for token, list := range s.text {
		text[token] = append([]DocID(nil), list...)
	}
	postings := make(map[string]map[string][]DocID, len(s.postings))
	for facet, values := range s.postings {
		copied := make(map[string][]DocID, len(values))
		for value, list := range values {
			copied[value] = append([]DocID(nil), list...)
		}
		postings[facet] = copied
	}
	return &snapshot{
		version:  s.version,
		docs:     docs,
		text:     text,
		postings: postings,
		order:    append([]DocID(nil), s.order...),
	}
}

// insertSortedID inserts id into an ascending DocID slice.
func insertSortedID(list []DocID, id DocID) []DocID {
	position := sort.Search(len(list), func(i int) bool {
		return list[i] >= id
	})
	if position < len(list) && list[position] == id {
		return list
	}
	list = append(list, 0)
	copy(list[position+1:], list[position:])
	list[position] = id
	return list
}

// removeSortedID deletes id from an ascending DocID slice.
func removeSortedID(list []DocID, id DocID) []DocID {
	position := sort.Search(len(list), func(i int) bool {
		return list[i] >= id
	})
	if position >= len(list) || list[position] != id {
		return list
	}
	return append(list[:position], list[position+1:]...)
}

// intersectSorted returns the intersection of two ascending DocID slices.
func intersectSorted(a, b []DocID) []DocID {
	result := make([]DocID, 0, min(len(a), len(b)))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			result = append(result, a[i])
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return result
}

// unionSorted returns the union of two ascending DocID slices with duplicates
// removed. It is used to combine the OR-selected values of one facet.
func unionSorted(a, b []DocID) []DocID {
	result := make([]DocID, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		var next DocID
		switch {
		case i >= len(a):
			next = b[j]
		case j >= len(b):
			next = a[i]
		case a[i] < b[j]:
			next = a[i]
		case a[i] > b[j]:
			next = b[j]
		default:
			next = a[i]
		}
		if len(result) == 0 || result[len(result)-1] != next {
			result = append(result, next)
		}
		if i < len(a) && a[i] == next {
			i++
		}
		if j < len(b) && b[j] == next {
			j++
		}
	}
	return result
}

// containsID reports whether an ascending DocID slice holds id.
func containsID(list []DocID, id DocID) bool {
	position := sort.Search(len(list), func(i int) bool {
		return list[i] >= id
	})
	return position < len(list) && list[position] == id
}
