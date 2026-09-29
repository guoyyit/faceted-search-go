package faceted

import (
	"sort"
	"strings"
)

// SearchParams describes a faceted search request.
type SearchParams struct {
	// Query is free text; all whitespace-separated tokens must match
	// (case-insensitive) in Title or Body. Empty means no text constraint.
	Query string

	// Filters maps facet name to its selected values.
	Filters map[string]Filter

	// Offset/Limit define the stable page; Limit <= 0 means a default size.
	Offset int
	Limit  int
}

// FacetValue is one flat facet bucket with its current count.
type FacetValue struct {
	Value    string
	Count    int
	Selected bool
}

// FacetCounts is the count view of one facet for one response.
type FacetCounts struct {
	Name    string
	Type    FacetType
	Buckets []FacetValue
	Tree    []*FacetBucket
}

// SearchResult is one response: hits and facet counts come from the same
// snapshot version and are therefore mutually consistent.
type SearchResult struct {
	Version     uint64
	Total       int
	Offset      int
	Limit       int
	Documents   []*Document
	Facets      map[string]*FacetCounts
	DocsScanned int
}

// defaultLimit is the page size applied when SearchParams.Limit is not
// positive.
const defaultLimit = 20

// matchSet resolves the ascending DocID set satisfying the free-text query and
// every selected facet. Same-facet values are OR-combined; distinct facets are
// AND-combined; hierarchical selections match a node together with its
// subtree.
//
// nil means "the whole index": with no query and no filters no work is spent
// scanning documents.
func (e *Engine) matchSet(snap *snapshot, params SearchParams) ([]DocID, bool) {
	var result []DocID
	restricted := false

	tokens := queryTokens(params.Query)
	if len(tokens) > 0 {
		result = snap.text[tokens[0]]
		for _, token := range tokens[1:] {
			result = intersectSorted(result, snap.text[token])
		}
		restricted = true
	}

	names := make([]string, 0, len(params.Filters))
	for name := range params.Filters {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		filter := params.Filters[name]
		values := filter.Values()
		if len(values) == 0 {
			continue
		}
		facetSet := e.facetMatchSet(snap, name, values)
		if restricted {
			result = intersectSorted(result, facetSet)
		} else {
			result = facetSet
		}
		restricted = true
	}

	return result, restricted
}

// facetMatchSet returns the union of documents matching the selected values of
// one facet, honoring subtree roll-up for hierarchical facets.
func (e *Engine) facetMatchSet(snap *snapshot, name string, values []string) []DocID {
	spec, declared := e.specs[name]
	if !declared {
		return nil
	}
	buckets := snap.postings[name]
	var result []DocID
	if spec.Type == FacetHierarchical {
		// Postings already roll descendants up into ancestors, so unioning the
		// node lists yields exact distinct subtree matches.
		for _, value := range values {
			result = unionSorted(result, buckets[value])
		}
		return result
	}
	for _, value := range values {
		result = unionSorted(result, buckets[value])
	}
	return result
}

// fullCountKey renders the complete response-shaping input (query tokens and
// every selected filter) as a stable cache key. Counts for every facet are
// computed over exactly this result set, so a single key identifies them.
func fullCountKey(params SearchParams) string {
	var builder strings.Builder
	tokens := queryTokens(params.Query)
	builder.WriteString("q=")
	builder.WriteString(filterKey(tokens))
	builder.WriteByte('&')
	builder.WriteString("f=")
	builder.WriteString(filtersKey(params.Filters, ""))
	return builder.String()
}

// countFacet computes counts for one facet over an ascending candidate set.
// nil candidates means the unconstrained universe.
func (e *Engine) countFacet(snap *snapshot, facet string, candidates []DocID, restricted bool) *facetCountResult {
	spec := e.specs[facet]
	result := &facetCountResult{counts: make(map[string]int)}

	if !restricted {
		// Unconstrained universe: posting lists already hold the exact rolled
		// up sets, so no document has to be inspected.
		for value, list := range snap.postings[facet] {
			result.counts[value] = len(list)
		}
		return result
	}

	result.docsScanned = len(candidates)
	for _, id := range candidates {
		doc := snap.docs[id]
		if doc == nil {
			continue
		}
		value := doc.Attrs[facet]
		if value == "" {
			continue
		}
		if spec.Type == FacetHierarchical {
			// Distinct documents per node: each ancestor is incremented once
			// per document even though the document also posts to descendants.
			for _, ancestor := range ancestorsOf(value) {
				result.counts[ancestor]++
			}
			continue
		}
		result.counts[value]++
	}
	return result
}

// pageWindow returns the [start, end) bounds of one stable page.
func pageWindow(total, offset, limit int) (int, int) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = defaultLimit
	}
	start := offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	return start, end
}
