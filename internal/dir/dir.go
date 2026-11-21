// Package dir holds the small, library-independent types used to pass
// directory entries between the LDAP client, the mapper and the scanner.
package dir

import (
	"context"
	"strings"
)

// Entry is a directory entry with its attributes. Attribute names are looked
// up case-insensitively, as in LDAP.
type Entry struct {
	DN    string
	Attrs map[string][]string
}

// Values returns all values of the named attribute (case-insensitive).
func (e Entry) Values(name string) []string {
	for k, v := range e.Attrs {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return nil
}

// First returns the first value of the named attribute, or "".
func (e Entry) First(name string) string {
	if v := e.Values(name); len(v) > 0 {
		return v[0]
	}
	return ""
}

// Query describes one directory search.
type Query struct {
	BaseDN     string
	Scope      string // "sub", "one" or "base"
	Filter     string
	Attributes []string
}

// Searcher runs a search and returns all matching entries.
type Searcher interface {
	Search(ctx context.Context, q Query) ([]Entry, error)
}
