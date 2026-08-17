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

// filterWalk returns the entries in results (in walk order) whose RelPath
// case-insensitively contains query. An empty query matches every entry
// (returns results unchanged, in walk order) — deliberately diverging
// from firstMatch's "empty query matches nothing" convention: find mode
// is a browsable list, useful immediately after f is pressed, not only
// once the user starts typing.
func filterWalk(results []fsutil.WalkEntry, query string) []fsutil.WalkEntry {
	if query == "" {
		return results
	}
	q := strings.ToLower(query)
	var matched []fsutil.WalkEntry
	for _, e := range results {
		if strings.Contains(strings.ToLower(e.RelPath), q) {
			matched = append(matched, e)
		}
	}
	return matched
}
