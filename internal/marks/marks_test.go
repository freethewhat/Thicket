package marks_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"thicket/internal/marks"
)

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoad_MissingFileReturnsEmptyMarks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marks")

	m, err := marks.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m == nil {
		t.Fatal("Load returned nil Marks for a missing file, want non-nil empty")
	}
	if len(m) != 0 {
		t.Fatalf("Load returned %d marks for a missing file, want 0", len(m))
	}
}

func TestLoad_MalformedLineSkipped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marks")
	mustWriteFile(t, path, strings.Join([]string{
		"a\t/only-valid", // the one valid line
		"toofewfields",   // wrong field count (1)
		"c\td\te",        // wrong field count (3)
		"1\t/non-letter-key",
		"ab\t/multi-rune-key",
	}, "\n")+"\n")

	m, err := marks.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(m) != 1 {
		t.Fatalf("Load returned %d marks, want 1 (malformed lines skipped): %+v", len(m), m)
	}
	if m['a'] != "/only-valid" {
		t.Fatalf("m['a'] = %q, want /only-valid", m['a'])
	}
}

func TestLoad_PermissionDeniedReturnsError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	path := filepath.Join(t.TempDir(), "marks")
	mustWriteFile(t, path, "a\t/x\n")
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o644)

	if _, err := marks.Load(path); err == nil {
		t.Fatal("expected an error reading a permission-denied marks file")
	}
}

func TestSaveThenLoad_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "marks")
	want := marks.Marks{'a': "/home/user/projects", 'Z': "/var/log"}

	if err := marks.Save(path, want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := marks.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d marks, want %d: %+v", len(got), len(want), got)
	}
	for r, p := range want {
		if got[r] != p {
			t.Fatalf("got[%q] = %q, want %q", r, got[r], p)
		}
	}
}

func TestSave_CreatesParentDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "state")
	path := filepath.Join(dir, "marks")

	if err := marks.Save(path, marks.Marks{'a': "/x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("parent directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", dir)
	}
}

func TestSave_SortsByLetterAscending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "marks")
	m := marks.Marks{'B': "/b", 'a': "/a", 'A': "/cap-a", 'z': "/z"}

	if err := marks.Save(path, m); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "a\t/a\nz\t/z\nA\t/cap-a\nB\t/b\n"
	if string(data) != want {
		t.Fatalf("Save wrote:\n%q\nwant:\n%q", data, want)
	}
}

func TestDefaultPath_UsesXDGStateHomeWhenSet(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_STATE_HOME", xdg)

	got, err := marks.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join(xdg, "thicket", "marks")
	if got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}

func TestDefaultPath_FallsBackToHomeLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := marks.DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join(home, ".local", "state", "thicket", "marks")
	if got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}
}
