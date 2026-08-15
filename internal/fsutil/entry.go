package fsutil

import "time"

// Entry describes one file-tree entry as shown in a browser pane.
type Entry struct {
	Name      string
	IsDir     bool // resolved: follows symlinks
	IsSymlink bool
	Broken    bool // symlink whose target could not be stat'd
	Size      int64
	ModTime   time.Time
}
