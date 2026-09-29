package faceted

import (
	"sort"
	"strings"
)

// Filter expresses the selected values of a single facet. Multiple values in
// one filter are combined with OR; filters of different facets are combined
// with AND.
type Filter struct {
	facet  string
	values []string
}

// NewFilter builds a Filter for one facet from the selected values.
func NewFilter(facet string, values ...string) Filter {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return Filter{facet: facet, values: unique}
}

// Facet returns the facet name the filter applies to.
func (f Filter) Facet() string {
	return f.facet
}

// Values returns the selected facet values.
func (f Filter) Values() []string {
	return f.values
}

// filterKey renders a value list as a stable, collision-resistant key. Values
// are assumed to be already sorted and de-duplicated (as produced by
// NewFilter). Lengths are embedded to avoid ambiguity.
func filterKey(values []string) string {
	var builder strings.Builder
	for _, value := range values {
		builder.WriteString(itoa(len(value)))
		builder.WriteByte(':')
		builder.WriteString(value)
		builder.WriteByte(';')
	}
	return builder.String()
}

// filtersKey renders the AND-combination of every filter except the skipped
// facet as a stable cache key.
func filtersKey(filters map[string]Filter, skip string) string {
	names := make([]string, 0, len(filters))
	for name := range filters {
		if name != skip {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var builder strings.Builder
	for _, name := range names {
		builder.WriteString(itoa(len(name)))
		builder.WriteByte(':')
		builder.WriteString(name)
		builder.WriteByte('=')
		builder.WriteString(filterKey(filters[name].values))
		builder.WriteByte('&')
	}
	return builder.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	pos := len(digits)
	for n > 0 {
		pos--
		digits[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[pos:])
}
