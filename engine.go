package faceted

import (
	"sort"
	"sync"
)

// Engine is a concurrency-safe faceted-search index.
type Engine struct {
	mu     sync.RWMutex
	snap   *snapshot
	specs  map[string]FacetSpec
	nextID DocID
	cache  *countCache
}

// NewEngine creates an Engine with the given facet declarations.
func NewEngine(specs []FacetSpec) *Engine {
	specByName := make(map[string]FacetSpec, len(specs))
	for _, spec := range specs {
		specByName[spec.Name] = spec
	}
	snap := newSnapshot()
	cache := newCountCache()
	cache.version = snap.version
	return &Engine{
		snap:  snap,
		specs: specByName,
		cache: cache,
	}
}

// Put inserts a document (assigning an ID when ID is zero) or replaces the
// stored document with the same ID.
func (e *Engine) Put(doc *Document) (DocID, error) {
	if doc == nil {
		return 0, errNilDocument
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	id := doc.ID
	if id == 0 {
		e.nextID++
		id = e.nextID
	} else if id > e.nextID {
		e.nextID = id
	}

	stored := cloneDocument(doc, id)
	next := e.snap.clone()
	if old, ok := next.docs[id]; ok {
		e.removeIndexed(next, old)
	}
	e.addIndexed(next, stored)
	next.docs[id] = stored
	if _, ok := e.snap.docs[id]; !ok {
		next.order = insertSortedID(next.order, id)
	}
	e.publish(next)
	return id, nil
}

// Update replaces the structured attributes of a stored document.
func (e *Engine) Update(id DocID, attrs map[string]string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	old, ok := e.snap.docs[id]
	if !ok {
		return errDocumentNotFound
	}

	updated := &Document{
		ID:    old.ID,
		Title: old.Title,
		Body:  old.Body,
		Attrs: copyAttrs(attrs),
	}
	next := e.snap.clone()
	e.removeIndexed(next, old)
	e.addIndexed(next, updated)
	next.docs[id] = updated
	e.publish(next)
	return nil
}

// Delete removes a document. It reports whether the ID existed.
func (e *Engine) Delete(id DocID) bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	old, ok := e.snap.docs[id]
	if !ok {
		return false
	}
	next := e.snap.clone()
	e.removeIndexed(next, old)
	delete(next.docs, id)
	next.order = removeSortedID(next.order, id)
	e.publish(next)
	return true
}

// Search returns one page of matching documents together with facet counts.
func (e *Engine) Search(params SearchParams) SearchResult {
	e.mu.RLock()
	snap := e.snap
	e.mu.RUnlock()

	matched, restricted := e.matchSet(snap, params)
	if !restricted {
		matched = snap.order
	}
	total := len(matched)

	facets := make(map[string]*FacetCounts, len(e.specs))
	docsScanned := 0
	countKey := fullCountKey(params)
	for _, spec := range sortedSpecs(e.specs) {
		// Counts and hits are derived from the same snap, so a response can
		// never mix versions. Every facet is counted over the same matched
		// set, and the key covers the query and all filters.
		cached, hit := e.cache.get(snap.version, spec.Name, countKey)
		if !hit {
			cached = e.countFacet(snap, spec.Name, matched, restricted)
			e.cache.put(snap.version, spec.Name, countKey, cached)
			docsScanned += cached.docsScanned
		}
		facets[spec.Name] = e.buildFacetCounts(spec, cached, params.Filters[spec.Name])
	}

	offset, limit := pageWindow(total, params.Offset, params.Limit)
	page := make([]*Document, 0, limit-offset)
	for _, id := range matched[offset:limit] {
		page = append(page, snap.docs[id])
	}

	return SearchResult{
		Version:     snap.version,
		Total:       total,
		Offset:      offset,
		Limit:       limit,
		Documents:   page,
		Facets:      facets,
		DocsScanned: docsScanned,
	}
}

// buildFacetCounts turns raw counts into the response shape: sorted flat
// buckets or a hierarchical tree.
func (e *Engine) buildFacetCounts(spec FacetSpec, result *facetCountResult, selected Filter) *FacetCounts {
	selectedSet := make(map[string]bool, len(selected.Values()))
	for _, value := range selected.Values() {
		selectedSet[value] = true
	}
	counts := &FacetCounts{Name: spec.Name, Type: spec.Type}

	if spec.Type == FacetHierarchical {
		counts.Tree = buildHierarchyTree(result.counts, selectedSet)
		return counts
	}

	values := make([]string, 0, len(result.counts))
	for value := range result.counts {
		values = append(values, value)
	}
	sort.Strings(values)
	counts.Buckets = make([]FacetValue, 0, len(values))
	for _, value := range values {
		counts.Buckets = append(counts.Buckets, FacetValue{
			Value:    value,
			Count:    result.counts[value],
			Selected: selectedSet[value],
		})
	}
	return counts
}

func sortedSpecs(specs map[string]FacetSpec) []FacetSpec {
	names := make([]string, 0, len(specs))
	for name := range specs {
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := make([]FacetSpec, 0, len(names))
	for _, name := range names {
		ordered = append(ordered, specs[name])
	}
	return ordered
}

// SnapshotVersion returns the current update-boundary version.
func (e *Engine) SnapshotVersion() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.snap.version
}

// publish commits a freshly built snapshot as the new update boundary and
// invalidates every count computed against earlier boundaries.
func (e *Engine) publish(next *snapshot) {
	next.version = e.snap.version + 1
	e.snap = next
	e.cache.invalidate(next.version)
}

// addIndexed inserts the document into every text posting and every facet
// posting. For hierarchical facets the document is posted at the value itself
// and at each ancestor, which is what makes parent selections roll up.
func (e *Engine) addIndexed(snap *snapshot, doc *Document) {
	for token := range docTokens(doc.Title, doc.Body) {
		snap.text[token] = insertSortedID(snap.text[token], doc.ID)
	}
	for name, spec := range e.specs {
		value, ok := doc.Attrs[name]
		if !ok || value == "" {
			continue
		}
		values := []string{value}
		if spec.Type == FacetHierarchical {
			values = ancestorsOf(value)
		}
		buckets := ensureFacet(snap.postings, name)
		for _, node := range values {
			buckets[node] = insertSortedID(buckets[node], doc.ID)
		}
	}
}

// removeIndexed reverses addIndexed. Only posting lists that actually carry the
// document are touched, so update cost scales with the document's own facets
// rather than with the corpus size.
func (e *Engine) removeIndexed(snap *snapshot, doc *Document) {
	for token := range docTokens(doc.Title, doc.Body) {
		snap.text[token] = removeSortedID(snap.text[token], doc.ID)
	}
	for name, spec := range e.specs {
		value, ok := doc.Attrs[name]
		if !ok || value == "" {
			continue
		}
		values := []string{value}
		if spec.Type == FacetHierarchical {
			values = ancestorsOf(value)
		}
		buckets, ok := snap.postings[name]
		if !ok {
			continue
		}
		for _, node := range values {
			buckets[node] = removeSortedID(buckets[node], doc.ID)
		}
	}
}

func ensureFacet(postings map[string]map[string][]DocID, name string) map[string][]DocID {
	buckets, ok := postings[name]
	if !ok {
		buckets = make(map[string][]DocID)
		postings[name] = buckets
	}
	return buckets
}

func cloneDocument(doc *Document, id DocID) *Document {
	return &Document{
		ID:    id,
		Title: doc.Title,
		Body:  doc.Body,
		Attrs: copyAttrs(doc.Attrs),
	}
}

func copyAttrs(attrs map[string]string) map[string]string {
	copied := make(map[string]string, len(attrs))
	for key, value := range attrs {
		copied[key] = value
	}
	return copied
}
