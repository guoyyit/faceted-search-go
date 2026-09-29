package faceted

import "errors"

var (
	errNilDocument      = errors.New("faceted: document must not be nil")
	errDocumentNotFound = errors.New("faceted: document not found")
)
