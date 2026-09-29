package faceted

import "sort"

// HierSep separates ancestor nodes in hierarchical facet values, for example
// "electronics/phones/mobile".
const HierSep = "/"

// FacetBucket is one node of a hierarchical facet tree.
type FacetBucket struct {
	Value    string
	Count    int
	Selected bool
	Children []*FacetBucket
}

// hierarchicalMatch reports whether an attribute value matches any selected
// tree node. A selected node matches itself and all of its descendants.
func hierarchicalMatch(attrValue string, selected []string) bool {
	for _, node := range selected {
		if attrValue == node {
			return true
		}
		// Selecting a node rolls up its whole subtree: an attribute matches
		// when it equals the node or starts with "node/".
		if len(attrValue) > len(node) &&
			attrValue[:len(node)] == node &&
			attrValue[len(node)] == HierSep[0] {
			return true
		}
	}
	return false
}

// ancestorsOf returns the full value path including every ancestor of a
// hierarchical attribute value, e.g. "a/b/c" -> ["a", "a/b", "a/b/c"].
func ancestorsOf(value string) []string {
	var result []string
	for i := 0; i <= len(value); i++ {
		if i == len(value) || value[i] == HierSep[0] {
			if i > 0 {
				result = append(result, value[:i])
			}
		}
	}
	return result
}

// buildHierarchyTree assembles counted nodes into a parent/child tree.
//
// Every node present in counts is emitted (including zero-count selected
// nodes). A parent's count is the number of base-set documents whose value
// equals the node or lies below it, computed over distinct documents; a
// document is never counted twice within one bucket even though postings roll
// up through ancestors.
func buildHierarchyTree(counts map[string]int, selected map[string]bool) []*FacetBucket {
	values := make(map[string]bool, len(counts))
	for value := range counts {
		for _, ancestor := range ancestorsOf(value) {
			values[ancestor] = true
		}
	}
	for value := range selected {
		for _, ancestor := range ancestorsOf(value) {
			values[ancestor] = true
		}
	}

	sorted := make([]string, 0, len(values))
	for value := range values {
		sorted = append(sorted, value)
	}
	sort.Strings(sorted)

	byValue := make(map[string]*FacetBucket, len(sorted))
	var roots []*FacetBucket
	for _, value := range sorted {
		bucket := &FacetBucket{
			Value:    value,
			Count:    counts[value],
			Selected: selected[value],
		}
		byValue[value] = bucket

		parent := parentOf(value)
		if parent == "" {
			roots = append(roots, bucket)
		} else {
			byValue[parent].Children = append(byValue[parent].Children, bucket)
		}
	}
	return roots
}

// parentOf returns the ancestor path of a hierarchical value, or "" at root.
func parentOf(value string) string {
	for i := len(value) - 1; i >= 0; i-- {
		if value[i] == HierSep[0] {
			return value[:i]
		}
	}
	return ""
}
