package faceted

import "sort"

// Filter selects documents by facet values.
// Values within one facet are combined with OR; distinct facets are
// combined with AND.
type Filter map[string][]FacetPath

// Empty reports whether no value is selected.
func (f Filter) Empty() bool {
	for _, vals := range f {
		if len(vals) > 0 {
			return false
		}
	}
	return true
}

// Clone returns a deep copy of the filter.
func (f Filter) Clone() Filter {
	cp := make(Filter, len(f))
	for facet, vals := range f {
		cloned := make([]FacetPath, len(vals))
		for i, p := range vals {
			pc := make(FacetPath, len(p))
			copy(pc, p)
			cloned[i] = pc
		}
		cp[facet] = cloned
	}
	return cp
}

// withoutFacet returns a clone with the given facet's selection removed.
func (f Filter) withoutFacet(name string) Filter {
	cp := f.Clone()
	delete(cp, name)
	return cp
}

// cacheKey renders the filter into a stable string key.
func (f Filter) cacheKey() string {
	facets := make([]string, 0, len(f))
	for name, vals := range f {
		if len(vals) > 0 {
			facets = append(facets, name)
		}
	}
	sort.Strings(facets)
	var b []byte
	for _, name := range facets {
		paths := append([]FacetPath(nil), f[name]...)
		sort.Slice(paths, func(i, j int) bool {
			return nodeKey(paths[i]) < nodeKey(paths[j])
		})
		b = append(b, name...)
		b = append(b, '=')
		for i, p := range paths {
			if i > 0 {
				b = append(b, '|')
			}
			b = append(b, nodeKey(p)...)
		}
		b = append(b, ';')
	}
	return string(b)
}
