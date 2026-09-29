package faceted

import "errors"

// FacetKind distinguishes flat facets from hierarchical facets.
type FacetKind int

const (
	// KindFlat is a flat list of unrelated values.
	KindFlat FacetKind = iota
	// KindHierarchy is a parent/child value tree.
	KindHierarchy
)

// FacetDef describes one facet.
type FacetDef struct {
	Name string
	Kind FacetKind
}

// Schema is the ordered set of known facet definitions.
type Schema struct {
	facets map[string]FacetDef
	order  []string
}

// NewSchema creates an empty schema.
func NewSchema() *Schema {
	return &Schema{facets: make(map[string]FacetDef)}
}

// AddFlat registers a flat facet.
func (s *Schema) AddFlat(name string) error {
	if name == "" {
		return errors.New("facet name must not be empty")
	}
	if _, ok := s.facets[name]; ok {
		return errors.New("facet already registered: " + name)
	}
	s.facets[name] = FacetDef{Name: name, Kind: KindFlat}
	s.order = append(s.order, name)
	return nil
}

// AddHierarchy registers a hierarchical facet.
func (s *Schema) AddHierarchy(name string) error {
	if name == "" {
		return errors.New("facet name must not be empty")
	}
	if _, ok := s.facets[name]; ok {
		return errors.New("facet already registered: " + name)
	}
	s.facets[name] = FacetDef{Name: name, Kind: KindHierarchy}
	s.order = append(s.order, name)
	return nil
}

// Get returns the definition of a facet.
func (s *Schema) Get(name string) (FacetDef, bool) {
	d, ok := s.facets[name]
	return d, ok
}

// Names returns facet names in registration order.
func (s *Schema) Names() []string {
	out := make([]string, len(s.order))
	copy(out, s.order)
	return out
}
