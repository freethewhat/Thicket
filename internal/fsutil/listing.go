package fsutil

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListDir reads dir, classifies each entry, filters dotfiles unless
// showHidden is set, and sorts directories before files (case-insensitive
// alphabetical within each group).
func ListDir(dir string, showHidden bool) ([]Entry, error) {
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		name := de.Name()
		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		entries = append(entries, classify(dir, name))
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

func classify(dir, name string) Entry {
	full := filepath.Join(dir, name)
	lst, err := os.Lstat(full)
	if err != nil {
		return Entry{Name: name, Broken: true}
	}
	if lst.Mode()&os.ModeSymlink == 0 {
		return Entry{Name: name, IsDir: lst.IsDir(), Size: lst.Size(), ModTime: lst.ModTime()}
	}
	st, err := os.Stat(full) // follows the symlink
	if err != nil {
		return Entry{Name: name, IsSymlink: true, Broken: true}
	}
	return Entry{Name: name, IsDir: st.IsDir(), IsSymlink: true, Size: st.Size(), ModTime: st.ModTime()}
}

// IndexOfName returns the index of the entry named name, or -1 if absent.
func IndexOfName(entries []Entry, name string) int {
	for i, e := range entries {
		if e.Name == name {
			return i
		}
	}
	return -1
}
