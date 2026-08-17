// internal/fsutil/walk_test.go
package fsutil_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"thicket/internal/fsutil"
)

// walkFixture builds:
//
//	root/
//	  sub/
//	    grand/
//	      deep.txt
//	    leaf.txt
//	  file.txt
//	  .hidden
//	  denied/        (chmod 0o000)
//	  link -> sub     (symlink to a directory)
func walkFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "sub", "grand"))
	mustWriteFile(t, filepath.Join(root, "sub", "grand", "deep.txt"), "hi")
	mustWriteFile(t, filepath.Join(root, "sub", "leaf.txt"), "hi")
	mustWriteFile(t, filepath.Join(root, "file.txt"), "hi")
	mustWriteFile(t, filepath.Join(root, ".hidden"), "hi")
	return root
}

func TestWalkSubtree_FindsNestedFilesAndDirs(t *testing.T) {
	root := walkFixture(t)

	entries, truncated, err := fsutil.WalkSubtree(root, false)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	if truncated {
		t.Fatal("expected truncated == false")
	}
	want := map[string]bool{
		"sub":                false,
		"sub/grand":          false,
		filepath.Join("sub", "grand", "deep.txt"): false,
		filepath.Join("sub", "leaf.txt"):           false,
		"file.txt":                                 false,
	}
	for _, e := range entries {
		if _, ok := want[e.RelPath]; ok {
			want[e.RelPath] = true
		}
	}
	for relPath, found := range want {
		if !found {
			t.Fatalf("expected RelPath %q in results, got %+v", relPath, entries)
		}
	}
}

func TestWalkSubtree_SkipsHiddenWhenShowHiddenFalse(t *testing.T) {
	root := walkFixture(t)

	entries, _, err := fsutil.WalkSubtree(root, false)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	for _, e := range entries {
		if e.RelPath == ".hidden" {
			t.Fatal("expected .hidden excluded when showHidden is false")
		}
	}

	entries, _, err = fsutil.WalkSubtree(root, true)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.RelPath == ".hidden" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected .hidden included when showHidden is true")
	}
}

func TestWalkSubtree_DoesNotDescendIntoSymlinkedDir(t *testing.T) {
	root := walkFixture(t)
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join(root, "sub"), link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	entries, _, err := fsutil.WalkSubtree(root, false)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	sawLink := false
	for _, e := range entries {
		if e.RelPath == "link" {
			sawLink = true
			if !e.IsSymlink {
				t.Fatal("expected link entry to have IsSymlink == true")
			}
		}
		if e.RelPath == filepath.Join("link", "leaf.txt") {
			t.Fatal("expected walk to not descend into symlinked directory")
		}
	}
	if !sawLink {
		t.Fatal("expected link itself to appear as a leaf result")
	}
}

func TestWalkSubtree_SkipsPermissionDeniedSubdir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	root := walkFixture(t)
	denied := filepath.Join(root, "denied")
	mustMkdir(t, denied)
	mustWriteFile(t, filepath.Join(denied, "secret.txt"), "hi")
	if err := os.Chmod(denied, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(denied, 0o755) })

	entries, _, err := fsutil.WalkSubtree(root, false)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	sawDenied := false
	for _, e := range entries {
		if e.RelPath == "denied" {
			sawDenied = true
		}
		if e.RelPath == filepath.Join("denied", "secret.txt") {
			t.Fatal("expected walk to not surface contents of a permission-denied subdir")
		}
	}
	if !sawDenied {
		t.Fatal("expected the denied directory itself to still appear as a result")
	}
}

func TestWalkSubtree_StopsAtMaxDepthAndSetsTruncated(t *testing.T) {
	root := t.TempDir()
	dir := root
	for i := 0; i < 20; i++ {
		dir = filepath.Join(dir, "d")
		mustMkdir(t, dir)
	}

	entries, truncated, err := fsutil.WalkSubtree(root, false)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncated == true for a 20-level-deep tree")
	}
	if len(entries) >= 20 {
		t.Fatalf("expected depth cap to bound results well under 20, got %d", len(entries))
	}
}

func TestWalkSubtree_BelowEntryCapReturnsAllUntruncated(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 200; i++ {
		mustWriteFile(t, filepath.Join(root, "f"+string(rune('a'+i%26))+string(rune('0'+i/26))+".txt"), "hi")
	}

	entries, truncated, err := fsutil.WalkSubtree(root, false)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	if truncated {
		t.Fatal("expected truncated == false well below the entry cap")
	}
	if len(entries) != 200 {
		t.Fatalf("got %d entries, want 200", len(entries))
	}
}

// TestWalkSubtree_StopsAtMaxEntriesAndSetsTruncated genuinely exceeds the
// real walkMaxEntries (20000) cap — the walkMaxDepth test above bounds
// worst-case cost via depth, this one via breadth, so both caps get an
// honest exercise rather than one being asserted only by name.
func TestWalkSubtree_StopsAtMaxEntriesAndSetsTruncated(t *testing.T) {
	root := t.TempDir()
	const overCap = 20050 // > walkMaxEntries (20000)
	for i := 0; i < overCap; i++ {
		name := filepath.Join(root, fmt.Sprintf("f%05d.txt", i))
		if err := os.WriteFile(name, nil, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	entries, truncated, err := fsutil.WalkSubtree(root, false)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	if !truncated {
		t.Fatal("expected truncated == true for a subtree over the entry cap")
	}
	if len(entries) != 20000 {
		t.Fatalf("got %d entries, want exactly the 20000 cap", len(entries))
	}
}

func TestWalkSubtree_RelPathIsRelativeToRoot(t *testing.T) {
	root := walkFixture(t)

	entries, _, err := fsutil.WalkSubtree(root, false)
	if err != nil {
		t.Fatalf("WalkSubtree: %v", err)
	}
	for _, e := range entries {
		if filepath.IsAbs(e.RelPath) {
			t.Fatalf("RelPath %q must be relative, not absolute", e.RelPath)
		}
	}
}

func TestWalkSubtree_ErrorsOnUnreadableRoot(t *testing.T) {
	_, _, err := fsutil.WalkSubtree(filepath.Join(t.TempDir(), "does-not-exist"), false)
	if err == nil {
		t.Fatal("expected an error for an unreadable root")
	}
}
