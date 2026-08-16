package tui

import (
	"strings"

	"thicket/internal/fsutil"
)

// firstMatch returns the index of the first entry in entries (in list
// order) whose Name case-insensitively contains query, or -1 if query is
// empty or no entry matches. Search always scans from the top of the list
// for the whole current query (spec §5) — deterministic regardless of
// prior cursor position.
func firstMatch(entries []fsutil.Entry, query string) int {
	if query == "" {
		return -1
	}
	q := strings.ToLower(query)
	for i, e := range entries {
		if strings.Contains(strings.ToLower(e.Name), q) {
			return i
		}
	}
	return -1
}
