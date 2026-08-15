package fsutil_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"thicket/internal/fsutil"
)

func TestListDir_SortsDirsFirstAndFiltersHidden(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "zdir"))
	mustMkdir(t, filepath.Join(dir, "adir"))
	mustWriteFile(t, filepath.Join(dir, "bfile.txt"), "hello")
	mustWriteFile(t, filepath.Join(dir, ".hidden"), "secret")

	entries, err := fsutil.ListDir(dir, false)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
	}
	want := []string{"adir", "zdir", "bfile.txt"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("got %v, want %v", names, want)
	}
}

func TestListDir_ShowHiddenIncludesDotfiles(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".hidden"), "x")

	entries, err := fsutil.ListDir(dir, true)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name != ".hidden" {
		t.Fatalf("got %+v", entries)
	}
}

func TestListDir_ClassifiesSymlinks(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "target"))
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "broken")); err != nil {
		t.Fatal(err)
	}

	entries, err := fsutil.ListDir(dir, false)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	byName := map[string]fsutil.Entry{}
	for _, e := range entries {
		byName[e.Name] = e
	}
	link := byName["link"]
	if !link.IsSymlink || !link.IsDir || link.Broken {
		t.Fatalf("link entry wrong: %+v", link)
	}
	broken := byName["broken"]
	if !broken.IsSymlink || !broken.Broken {
		t.Fatalf("broken entry wrong: %+v", broken)
	}
}

func TestListDir_PermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "locked")
	mustMkdir(t, sub)
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(sub, 0o755)

	if _, err := fsutil.ListDir(sub, false); err == nil {
		t.Fatal("expected error for unreadable dir")
	}
}

func TestIndexOfName(t *testing.T) {
	entries := []fsutil.Entry{{Name: "a"}, {Name: "b"}}
	if got := fsutil.IndexOfName(entries, "b"); got != 1 {
		t.Fatalf("got %d, want 1", got)
	}
	if got := fsutil.IndexOfName(entries, "z"); got != -1 {
		t.Fatalf("got %d, want -1", got)
	}
}
