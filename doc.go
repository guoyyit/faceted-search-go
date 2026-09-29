package faceted

// DocID is the unique identifier of a document.
type DocID string

// FacetPath is one assigned facet value, expressed as a path of components.
// A flat facet always uses a single-component path; a hierarchical facet
// uses one component per tree level (root first).
type FacetPath []string

// Attributes maps a facet name to the values assigned to a document.
// A document may carry multiple values for the same facet.
type Attributes map[string][]FacetPath

// Clone returns a deep copy of the attributes.
func (a Attributes) Clone() Attributes {
	cp := make(Attributes, len(a))
	for facet, paths := range a {
		cloned := make([]FacetPath, len(paths))
		for i, p := range paths {
			pc := make(FacetPath, len(p))
			copy(pc, p)
			cloned[i] = pc
		}
		cp[facet] = cloned
	}
	return cp
}

// Document is one indexed record: a body plus structured facet attributes.
type Document struct {
	ID    DocID
	Body  string
	Attrs Attributes
}

// Clone returns a deep copy of the document so callers may mutate the
// original maps/slices after the call without affecting the index.
func (d Document) Clone() Document {
	cp := Document{ID: d.ID, Body: d.Body}
	if d.Attrs != nil {
		cp.Attrs = make(Attributes, len(d.Attrs))
		for facet, paths := range d.Attrs {
			cloned := make([]FacetPath, len(paths))
			for i, p := range paths {
				pc := make(FacetPath, len(p))
				copy(pc, p)
				cloned[i] = pc
			}
			cp.Attrs[facet] = cloned
		}
	}
	return cp
}
