package faceted

import (
	"errors"
	"strings"
	"sync"
)

// Index is a concurrency-safe in-memory faceted search index.
// All reads observe a single atomic snapshot: the result set and every
// facet count in one response come from the same update boundary.
type Index struct {
	mu     sync.RWMutex
	schema *Schema

	// docs holds the latest committed document per id.
	docs map[DocID]Document

	// ordered keeps all live document ids sorted for default pagination.
	ordered *idTreap

	// postings[facet][nodeKey] holds documents matching that facet node.
	// For hierarchical facets the node at an ancestor path contains the
	// union of every value beneath it (rollup), so a parent selection
	// matches all descendant-assigned documents.
	postings map[string]map[string]*postingList

	// counts cache for frequent filter combinations.
	cache *countCache
}

// NewIndex creates an index over the given schema.
func NewIndex(schema *Schema) *Index {
	if schema == nil {
		schema = NewSchema()
	}
	return &Index{
		schema:   schema,
		docs:     make(map[DocID]Document),
		ordered:  newIDTreap(),
		postings: make(map[string]map[string]*postingList),
		cache:    newCountCache(),
	}
}

// Upsert atomically inserts a new document or replaces an existing one
// (body and attributes). Readers never observe a partial update.
func (ix *Index) Upsert(doc Document) error {
	if doc.ID == "" {
		return errors.New("document id must not be empty")
	}
	next := doc.Clone()
	if err := ix.validateAttrs(next.Attrs); err != nil {
		return err
	}

	ix.mu.Lock()
	defer ix.mu.Unlock()

	if old, ok := ix.docs[next.ID]; ok {
		ix.removePostings(old)
	}
	ix.docs[next.ID] = next
	ix.ordered.add(next.ID)
	ix.addPostings(next)
	ix.cache.invalidate()
	return nil
}

// Delete atomically removes a document and all of its postings.
func (ix *Index) Delete(id DocID) bool {
	ix.mu.Lock()
	defer ix.mu.Unlock()

	old, ok := ix.docs[id]
	if !ok {
		return false
	}
	ix.removePostings(old)
	delete(ix.docs, id)
	ix.ordered.remove(id)
	ix.cache.invalidate()
	return true
}

// UpdateAttributes atomically replaces the attributes of one document
// while leaving its body unchanged.
func (ix *Index) UpdateAttributes(id DocID, attrs Attributes) error {
	next := attrs
	if err := ix.validateAttrs(next); err != nil {
		return err
	}
	attrsCopy := Document{Attrs: attrs}.Clone().Attrs

	ix.mu.Lock()
	defer ix.mu.Unlock()

	old, ok := ix.docs[id]
	if !ok {
		return errors.New("document not found: " + string(id))
	}
	ix.removePostings(old)
	updated := old.Clone()
	updated.Attrs = attrsCopy
	ix.docs[id] = updated
	ix.addPostings(updated)
	ix.cache.invalidate()
	return nil
}

// Len returns the number of live documents.
func (ix *Index) Len() int {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.docs)
}

// Get returns a deep copy of one live document.
func (ix *Index) Get(id DocID) (Document, bool) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	doc, ok := ix.docs[id]
	if !ok {
		return Document{}, false
	}
	return doc.Clone(), true
}

// nodeKey joins path components into a stable postings map key.
func nodeKey(path FacetPath) string {
	return strings.Join(path, "\x1f")
}

// splitNodeKey reverses nodeKey.
func splitNodeKey(key string) FacetPath {
	return strings.Split(key, "\x1f")
}

// validateAttrs must be called without the lock held.
func (ix *Index) validateAttrs(attrs Attributes) error {
	for facet, paths := range attrs {
		def, ok := ix.schema.Get(facet)
		if !ok {
			return errors.New("unknown facet: " + facet)
		}
		for _, p := range paths {
			if len(p) == 0 {
				return errors.New("empty facet path in facet: " + facet)
			}
			for _, component := range p {
				if component == "" {
					return errors.New("empty path component in facet: " + facet)
				}
			}
			if def.Kind == KindFlat && len(p) != 1 {
				return errors.New("flat facet expects single-component paths: " + facet)
			}
		}
	}
	return nil
}

// indexPaths returns every postings node a value contributes to.
// Hierarchical values roll up to each ancestor; flat values use one node.
func indexPaths(def FacetDef, path FacetPath) []FacetPath {
	if def.Kind == KindHierarchy {
		return ancestorPaths(path)
	}
	return []FacetPath{path}
}

// addPostings and removePostings must be called with the write lock held.
func (ix *Index) addPostings(doc Document) {
	for facet, paths := range doc.Attrs {
		def := ix.schema.facets[facet]
		nodes, ok := ix.postings[facet]
		if !ok {
			nodes = make(map[string]*postingList)
			ix.postings[facet] = nodes
		}
		for _, p := range paths {
			for _, node := range indexPaths(def, p) {
				key := nodeKey(node)
				list, ok := nodes[key]
				if !ok {
					list = newPostingList()
					nodes[key] = list
				}
				list.add(doc.ID)
			}
		}
	}
}

func (ix *Index) removePostings(doc Document) {
	for facet, paths := range doc.Attrs {
		def := ix.schema.facets[facet]
		nodes := ix.postings[facet]
		if nodes == nil {
			continue
		}
		for _, p := range paths {
			for _, node := range indexPaths(def, p) {
				list := nodes[nodeKey(node)]
				if list == nil {
					continue
				}
				list.remove(doc.ID)
				if list.len() == 0 {
					delete(nodes, nodeKey(node))
				}
			}
		}
	}
}
