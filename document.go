package faceted

import "strings"

// DocID is the unique identifier of a document within an Engine.
type DocID uint64

// Document is a searchable unit with free-text fields and structured
// attributes. Every attribute value is a string; multi-valued attributes are
// not supported.
type Document struct {
	ID    DocID
	Title string
	Body  string
	Attrs map[string]string
}

// FacetType distinguishes flat and hierarchical facets.
type FacetType int

const (
	// FacetFlat treats every distinct attribute value as an independent bucket.
	FacetFlat FacetType = iota
	// FacetHierarchical treats "/" separated values as nodes of a tree.
	FacetHierarchical
)

// FacetSpec declares a facet dimension and its type.
type FacetSpec struct {
	Name string
	Type FacetType
}

// docTokens returns the normalized search tokens of Title and Body. Tokens are
// lower-case, whitespace-delimited words. Duplicates are de-duplicated because
// membership in a posting list, not term frequency, drives matching.
func docTokens(title, body string) map[string]struct{} {
	tokens := make(map[string]struct{})
	for _, raw := range strings.Fields(title + " " + body) {
		token := strings.ToLower(raw)
		if token != "" {
			tokens[token] = struct{}{}
		}
	}
	return tokens
}

// queryTokens returns the normalized tokens of a free-text query.
func queryTokens(query string) []string {
	fields := strings.Fields(query)
	tokens := make([]string, 0, len(fields))
	for _, raw := range fields {
		if token := strings.ToLower(raw); token != "" {
			tokens = append(tokens, token)
		}
	}
	return tokens
}
