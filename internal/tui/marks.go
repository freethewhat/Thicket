package tui

import (
	"sort"

	marksPkg "thicket/internal/marks"
)

// sortedMarkLetters returns the letters of m sorted lowercase-before-
// uppercase, alphabetical within each case (vim/ranger's `:marks`
// convention — NOT Go's default ascending-rune sort, which would put
// 'A'-'Z' before 'a'-'z'), for stable, deterministic list-screen row
// order.
func sortedMarkLetters(m marksPkg.Marks) []rune {
	letters := make([]rune, 0, len(m))
	for r := range m {
		letters = append(letters, r)
	}
	sort.Slice(letters, func(i, j int) bool {
		return markLetterLess(letters[i], letters[j])
	})
	return letters
}

// markLetterLess orders a before b lowercase-before-uppercase, then
// alphabetically within each case — see sortedMarkLetters. Deliberately
// independent of internal/marks's own letterLess: two different packages,
// two different data shapes (bare letters here vs. letter-path pairs
// there), no shared exported symbol to hang a common helper off of.
func markLetterLess(a, b rune) bool {
	aLower := a >= 'a' && a <= 'z'
	bLower := b >= 'a' && b <= 'z'
	if aLower != bLower {
		return aLower
	}
	return a < b
}

// marksListCursorFor returns 0 if m is non-empty, -1 if empty — the
// initial cursor position used when marksListMode opens (spec §5's '
// key). Deleting an entry does NOT reuse this function: a delete
// preserves marksCursor when it's still in range for the shrunk table,
// matching the reload()-style clamp convention in update.go (see Global
// Constraints), and this function has no parameter to preserve a
// previous cursor value with anyway.
func marksListCursorFor(m marksPkg.Marks) int {
	if len(m) == 0 {
		return -1
	}
	return 0
}
