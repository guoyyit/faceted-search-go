package faceted

import (
	"errors"
	"sort"
)

// SortField selects the pagination ordering.
type SortField int

const (
	// SortByID orders ascending by document id (fully deterministic).
	SortByID SortField = iota
	// SortByBody orders by the Body field, with id as tiebreaker.
	SortByBody
)

// Query is a faceted search request.
type Query struct {
	Filter Filter
	Offset int
	Limit  int
	Sort   SortField

	// ReturnCounts is kept for source compatibility. Facet counts are
	// always returned by default alongside the page and the total from a
	// single consistent snapshot, so this field is now a no-op.
	ReturnCounts bool
}

// FacetValueCount is one candidate value with its linked count.
type FacetValueCount struct {
	Path   FacetPath
	Count  int
	Active bool
	Depth  int
}

// FacetCounts holds all candidate values of one facet.
type FacetCounts struct {
	Facet  string
	Kind   FacetKind
	Values []FacetValueCount
}

// SearchResult is one atomic query response.
type SearchResult struct {
	Total  int
	IDs    []DocID
	Facets []FacetCounts
}

// Search executes a query against one consistent snapshot. The page, the
// total and every facet count are always returned together and share the
// same update boundary.
func (ix *Index) Search(q Query) (SearchResult, error) {
	if q.Offset < 0 {
		return SearchResult{}, errors.New("offset must not be negative")
	}
	if q.Limit < 0 {
		return SearchResult{}, errors.New("limit must not be negative")
	}
	f := q.Filter.Clone()
	if err := ix.validateFilter(f); err != nil {
		return SearchResult{}, err
	}

	// The read lock is held for the whole response, so the page, the
	// total and every facet count share one atomic snapshot.
	ix.mu.RLock()
	defer ix.mu.RUnlock()

	rs := ix.resolveSet(f)
	res := SearchResult{Total: ix.baseSize(rs)}
	res.IDs = ix.paginate(rs, q)
	res.Facets = ix.linkedCounts(f, rs)
	return res, nil
}

// matchFilter evaluates a document's attributes against a filter.
// It is the per-document reference implementation: selected values in one
// facet combine with OR, facets combine with AND; a hierarchical selection
// matches the node itself and every descendant-assigned document.
func matchFilter(attrs Attributes, f Filter) bool {
	for facet, selected := range f {
		if len(selected) == 0 {
			continue
		}
		values := attrs[facet]
		matched := false
		for _, sel := range selected {
			for _, v := range values {
				if isAncestorOrEqual(sel, v) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// resultSet pairs the membership view with an ordered treap used for
// stable pagination without touching the whole collection.
type resultSet struct {
	set  docSet
	tree *idTreap
}

// validateFilter must be called without the lock held.
func (ix *Index) validateFilter(f Filter) error {
	for facet, paths := range f {
		if len(paths) == 0 {
			continue
		}
		def, ok := ix.schema.Get(facet)
		if !ok {
			return errors.New("unknown facet in filter: " + facet)
		}
		for _, p := range paths {
			if len(p) == 0 {
				return errors.New("empty facet path in filter: " + facet)
			}
			if def.Kind == KindFlat && len(p) != 1 {
				return errors.New("flat facet filter expects single-component paths: " + facet)
			}
		}
	}
	return nil
}

// filterUnions builds, per selected facet, the OR-union of the posting sets
// of the selected nodes. Ancestor postings already roll up descendants.
func (ix *Index) filterUnions(f Filter) map[string]*resultSet {
	unions := make(map[string]*resultSet, len(f))
	for facet, paths := range f {
		if len(paths) == 0 {
			continue
		}
		nodes := ix.postings[facet]
		nonEmpty := make([]*postingList, 0, len(paths))
		for _, p := range paths {
			if list := nodes[nodeKey(p)]; list != nil && list.len() > 0 {
				nonEmpty = append(nonEmpty, list)
			}
		}
		if len(nonEmpty) == 0 {
			unions[facet] = &resultSet{set: docSet{}, tree: newIDTreap()}
			continue
		}
		// Start from the smallest input so the merge cost stays close to
		// the selected value count rather than the collection size.
		sort.Slice(nonEmpty, func(i, j int) bool {
			return nonEmpty[i].len() < nonEmpty[j].len()
		})
		tree := nonEmpty[0].tree
		set := nonEmpty[0].set.clone()
		for _, list := range nonEmpty[1:] {
			tree = unionTreaps(tree, list.tree)
			unionInto(set, list.set)
		}
		unions[facet] = &resultSet{set: set, tree: tree}
	}
	return unions
}

// universe returns the ordered result set for an empty filter: the whole
// live collection treap, shared read-only while the read lock is held.
func (ix *Index) universe() *resultSet {
	return &resultSet{set: nil, tree: ix.ordered}
}

// baseSet computes documents satisfying every selected facet (AND across
// facets, OR inside a facet).
func (ix *Index) baseSet(f Filter, unions map[string]*resultSet) *resultSet {
	if f.Empty() {
		return ix.universe()
	}
	names := make([]string, 0, len(unions))
	for name, u := range unions {
		if u.set.len() == 0 {
			return &resultSet{set: docSet{}, tree: newIDTreap()}
		}
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return unions[names[i]].set.len() < unions[names[j]].set.len()
	})

	first := unions[names[0]]
	tree := first.tree
	set := first.set
	for _, name := range names[1:] {
		other := unions[name]
		tree = intersectTreaps(tree, other.tree)
		next := make(docSet)
		intersectInto(next, set, other.set)
		set = next
		if set.len() == 0 {
			return &resultSet{set: docSet{}, tree: newIDTreap()}
		}
	}
	return &resultSet{set: set, tree: tree}
}

// resolveSet returns the filtered set, reusing the entry cache. Only
// non-empty filters are cached; the empty filter reads ix.ordered directly.
func (ix *Index) resolveSet(f Filter) *resultSet {
	if f.Empty() {
		return ix.universe()
	}
	key := f.cacheKey()
	if rs, ok := ix.cache.getSet(key); ok {
		return rs
	}
	unions := ix.filterUnions(f)
	rs := ix.baseSet(f, unions)
	if rs.set.len() > 0 {
		ix.cache.putSet(key, rs)
	}
	return rs
}

func (ix *Index) baseSize(rs *resultSet) int {
	if rs.set == nil {
		return ix.ordered.len()
	}
	return rs.set.len()
}

func (ix *Index) paginate(rs *resultSet, q Query) []DocID {
	if q.Limit == 0 {
		return []DocID{}
	}
	if q.Sort == SortByBody {
		ids := rs.set.sortedIDs()
		if rs.set == nil {
			ids = ix.ordered.page(0, ix.ordered.len())
		}
		sort.SliceStable(ids, func(i, j int) bool {
			a, b := ix.docs[ids[i]], ix.docs[ids[j]]
			if a.Body != b.Body {
				return a.Body < b.Body
			}
			return a.ID < b.ID
		})
		end := q.Offset + q.Limit
		if q.Offset >= len(ids) {
			return []DocID{}
		}
		if end > len(ids) {
			end = len(ids)
		}
		return ids[q.Offset:end]
	}

	keep := func(id DocID) bool { return true }
	if rs.set != nil {
		keep = rs.set.contains
	}
	out := make([]DocID, 0, q.Limit)
	out, _ = inorder(rs.tree.root, keep, q.Offset, q.Limit, out)
	return out
}

// linkedCounts computes candidate counts for every schema facet. Every
// facet, including the facet currently being drilled, is counted over the
// full result set obtained from the complete filter, so the counts agree
// with the per-document reference semantics and never exceed the total.
// Hierarchy presentation rules are documented in README.md.
func (ix *Index) linkedCounts(f Filter, rs *resultSet) []FacetCounts {
	entry := ix.countsForFilter(f, rs)
	out := make([]FacetCounts, 0, len(ix.schema.order))
	for _, facet := range ix.schema.order {
		def := ix.schema.facets[facet]
		values := ix.facetValueCounts(def, entry.facetCounts[facet], ix.postings[facet], f[facet])
		out = append(out, FacetCounts{Facet: facet, Kind: def.Kind, Values: values})
	}
	return out
}

// countsForFilter returns the full vector of candidate counts over the
// filter's result set, memoized by canonical filter key. A write bumps the
// cache epoch, so the vector can never be stale.
func (ix *Index) countsForFilter(f Filter, rs *resultSet) *countEntry {
	key := f.cacheKey()
	if e, ok := ix.cache.get(key); ok {
		return e
	}
	e := &countEntry{
		facetCounts: make(map[string]map[string]int, len(ix.schema.order)),
	}
	for _, facet := range ix.schema.order {
		nodes := ix.postings[facet]
		m := make(map[string]int, len(nodes))
		for key, list := range nodes {
			m[key] = countIntersect(rs, list)
		}
		e.facetCounts[facet] = m
	}
	ix.cache.put(key, e)
	return e
}

// facetValueCounts builds the visible candidate list for one facet. Each
// value's count is the number of documents in the result set that carry it
// (flat facets) or that are assigned to it or any descendant (hierarchy).
// Zero-count unselected values are hidden; selected values are always
// returned even when their count is zero. For hierarchy facets the
// ancestors of a selected node are also shown so the selection path stays
// visible.
func (ix *Index) facetValueCounts(def FacetDef, counts map[string]int, nodes map[string]*postingList, selected []FacetPath) []FacetValueCount {
	paths := knownPaths(nodes)
	for _, sel := range selected {
		if nodes[nodeKey(sel)] == nil {
			paths = append(paths, append(FacetPath(nil), sel...))
		}
	}
	sortFacetPaths(paths)

	values := make([]FacetValueCount, 0, len(paths))
	for _, p := range paths {
		count := counts[nodeKey(p)]
		active := false
		ancestorOfSel := false
		for _, sel := range selected {
			if facetsEqual(sel, p) {
				active = true
			}
			if def.Kind == KindHierarchy && isAncestorOrEqual(p, sel) {
				ancestorOfSel = true
			}
		}
		if count == 0 && !active && !ancestorOfSel {
			continue
		}
		values = append(values, FacetValueCount{
			Path:   p,
			Count:  count,
			Active: active,
			Depth:  len(p) - 1,
		})
	}
	return values
}

// countIntersect reports how many documents in scope also belong to list.
// A nil list contributes nothing.
func countIntersect(scope *resultSet, list *postingList) int {
	if list == nil {
		return 0
	}
	if scope.set == nil {
		return list.len()
	}
	if scope.set.len() <= list.len() {
		n := 0
		for id := range scope.set {
			if list.contains(id) {
				n++
			}
		}
		return n
	}
	n := 0
	for id := range list.set {
		if scope.set.contains(id) {
			n++
		}
	}
	return n
}

func knownPaths(nodes map[string]*postingList) []FacetPath {
	out := make([]FacetPath, 0, len(nodes))
	for key := range nodes {
		out = append(out, splitNodeKey(key))
	}
	return out
}

func sortFacetPaths(paths []FacetPath) {
	sort.Slice(paths, func(i, j int) bool {
		a, b := paths[i], paths[j]
		n := len(a)
		if len(b) < n {
			n = len(b)
		}
		for k := 0; k < n; k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return len(a) < len(b)
	})
}
