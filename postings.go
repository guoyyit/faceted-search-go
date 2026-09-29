package faceted

import "sort"

// docSet is an unordered set of document ids.
type docSet map[DocID]struct{}

func (s docSet) add(id DocID) { s[id] = struct{}{} }

func (s docSet) remove(id DocID) { delete(s, id) }

func (s docSet) contains(id DocID) bool {
	_, ok := s[id]
	return ok
}

func (s docSet) len() int {
	return len(s)
}

func (s docSet) clone() docSet {
	cp := make(docSet, len(s))
	for id := range s {
		cp[id] = struct{}{}
	}
	return cp
}

// intersectInto stores members present in both a and b into dst.
func intersectInto(dst, a, b docSet) {
	for id := range a {
		if b.contains(id) {
			dst.add(id)
		}
	}
}

// unionInto adds all members of src to dst.
func unionInto(dst, src docSet) {
	for id := range src {
		dst.add(id)
	}
}

// sortedIDs returns the set members sorted by id.
func (s docSet) sortedIDs() []DocID {
	ids := make([]DocID, 0, len(s))
	for id := range s {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// postingList keeps one facet node's matching documents both as a hash set
// (membership and counting) and as an ordered treap (stable pagination
// without sorting the whole collection).
type postingList struct {
	set  docSet
	tree *idTreap
}

func newPostingList() *postingList {
	return &postingList{set: make(docSet), tree: newIDTreap()}
}

func (p *postingList) add(id DocID) {
	if p.set.contains(id) {
		return
	}
	p.set.add(id)
	p.tree.add(id)
}

func (p *postingList) remove(id DocID) {
	if !p.set.contains(id) {
		return
	}
	p.set.remove(id)
	p.tree.remove(id)
}

func (p *postingList) contains(id DocID) bool { return p.set.contains(id) }

func (p *postingList) len() int { return p.set.len() }
