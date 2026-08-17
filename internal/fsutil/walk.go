package fsutil

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// walkMaxDepth is the descent limit below a WalkSubtree root.
	walkMaxDepth = 12
	// walkMaxEntries is the total-entries-visited limit for a WalkSubtree
	// call, regardless of depth.
	walkMaxEntries = 20000
)

// WalkEntry is one node found under a WalkSubtree root.
type WalkEntry struct {
	Entry
	// RelPath is the path relative to the walk root, e.g. "sub/dir/file.go".
	RelPath string
}

// WalkSubtree recursively lists root and everything under it (files and
// directories), reusing the same per-entry classification ListDir already
// uses. showHidden is honored at every level exactly like ListDir: a
// dotfile/dotdir is skipped, and a skipped directory is never descended
// into. Symlinked directories are included as leaf WalkEntry results but
// are never traversed, regardless of depth — no symlink-cycle risk.
// Permission-denied subdirectories are skipped silently; the walk
// continues with everything else. The walk stops as soon as walkMaxDepth
// or walkMaxEntries is reached, whichever comes first; truncated reports
// whether that happened before the whole subtree was covered. err is
// non-nil only if root itself cannot be listed.
func WalkSubtree(root string, showHidden bool) ([]WalkEntry, bool, error) {
	rootEntries, err := os.ReadDir(root)
	if err != nil {
		return nil, false, err
	}

	var results []WalkEntry
	truncated := false
	var stopped bool

	var walk func(dirEntries []os.DirEntry, dir, relDir string, depth int)
	walk = func(dirEntries []os.DirEntry, dir, relDir string, depth int) {
		names := make([]string, 0, len(dirEntries))
		for _, de := range dirEntries {
			name := de.Name()
			if !showHidden && strings.HasPrefix(name, ".") {
				continue
			}
			names = append(names, name)
		}
		sort.SliceStable(names, func(i, j int) bool {
			return strings.ToLower(names[i]) < strings.ToLower(names[j])
		})

		for _, name := range names {
			if stopped {
				return
			}
			if len(results) >= walkMaxEntries {
				stopped = true
				truncated = true
				return
			}
			e := classify(dir, name)
			relPath := name
			if relDir != "" {
				relPath = filepath.Join(relDir, name)
			}
			results = append(results, WalkEntry{Entry: e, RelPath: relPath})

			if e.IsDir && !e.IsSymlink {
				if depth < walkMaxDepth {
					childDir := filepath.Join(dir, name)
					childEntries, err := os.ReadDir(childDir)
					if err != nil {
						continue // permission-denied subdir: skip silently
					}
					walk(childEntries, childDir, relPath, depth+1)
				} else {
					truncated = true
				}
			}
		}
	}

	walk(rootEntries, root, "", 0)
	return results, truncated, nil
}
