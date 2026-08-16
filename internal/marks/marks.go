// Package marks persists thicket's directory marks (bookmarks): a table
// mapping a single letter to an absolute directory path. It is pure disk
// I/O, parallel to internal/fsutil in the dependency graph — no Bubble
// Tea/TUI concerns live here.
package marks

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Marks is a letter -> absolute-directory-path table. Only 'a'-'z' and
// 'A'-'Z' are valid keys; callers are responsible for validating the rune
// before inserting (internal/tui's Update() does this at the keystroke
// boundary).
type Marks map[rune]string

// Load reads path and returns its Marks table. A missing file is not an
// error — it returns an empty, non-nil Marks. Malformed individual lines
// (wrong field count, non-letter key, multi-rune key) are skipped, not
// fatal, mirroring fsutil.classify's per-entry error tolerance. Any other
// read error (e.g. permission denied on an existing file) is returned to
// the caller.
func Load(path string) (Marks, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Marks{}, nil
		}
		return nil, err
	}
	m := Marks{}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 2 {
			continue
		}
		letters := []rune(fields[0])
		if len(letters) != 1 || !isMarkLetter(letters[0]) {
			continue
		}
		m[letters[0]] = fields[1]
	}
	return m, nil
}

// Save writes m to path as sorted `letter\tpath\n` lines — lowercase
// letters before uppercase, alphabetical within each case (vim/ranger's
// `:marks` convention; NOT plain ascending rune order, since 'A' (65) <
// 'a' (97) in ASCII — ascending-rune sort would put A-Z first). Creates
// the parent directory (mode 0o700) if absent. Directory paths containing
// an embedded tab or newline byte will not round-trip correctly through
// this format — Load skips the resulting malformed line silently, same as
// any other corrupt line. Accepted as an unsupported edge case.
func Save(path string, m Marks) error {
	letters := make([]rune, 0, len(m))
	for r := range m {
		letters = append(letters, r)
	}
	sort.Slice(letters, func(i, j int) bool {
		return letterLess(letters[i], letters[j])
	})

	var b strings.Builder
	for _, r := range letters {
		b.WriteRune(r)
		b.WriteByte('\t')
		b.WriteString(m[r])
		b.WriteByte('\n')
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// DefaultPath returns $XDG_STATE_HOME/thicket/marks, falling back to
// $HOME/.local/state/thicket/marks when XDG_STATE_HOME is unset.
func DefaultPath() (string, error) {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "thicket", "marks"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "thicket", "marks"), nil
}

// isMarkLetter reports whether r is a valid mark key: exactly one ASCII
// letter, matching the 52-slot bound (a-z, A-Z) — not unicode.IsLetter,
// which would accept non-ASCII letters and break that bound.
func isMarkLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// letterLess orders a before b lowercase-before-uppercase, then
// alphabetically within each case — see Save.
func letterLess(a, b rune) bool {
	aLower := a >= 'a' && a <= 'z'
	bLower := b >= 'a' && b <= 'z'
	if aLower != bLower {
		return aLower
	}
	return a < b
}
