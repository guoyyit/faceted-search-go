package faceted

// ancestorPaths returns the path itself and every ancestor prefix.
func ancestorPaths(path FacetPath) []FacetPath {
	out := make([]FacetPath, 0, len(path))
	for depth := 1; depth <= len(path); depth++ {
		p := make(FacetPath, depth)
		copy(p, path[:depth])
		out = append(out, p)
	}
	return out
}

// isAncestorOrEqual reports whether candidate == path or candidate is a
// strict ancestor of path.
func isAncestorOrEqual(candidate, path FacetPath) bool {
	if len(candidate) > len(path) {
		return false
	}
	for i, c := range candidate {
		if c != path[i] {
			return false
		}
	}
	return true
}

// facetsEqual reports whether two paths contain the same components.
func facetsEqual(a, b FacetPath) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
