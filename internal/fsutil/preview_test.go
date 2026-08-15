package fsutil_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"thicket/internal/fsutil"
)

func TestReadFilePreview_TextFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	content := "line1\nline2\nline3"
	mustWriteFile(t, path, content)

	p, err := fsutil.ReadFilePreview(path)
	if err != nil {
		t.Fatalf("ReadFilePreview: %v", err)
	}
	if p.Binary {
		t.Fatal("expected non-binary")
	}
	want := []string{"line1", "line2", "line3"}
	if !reflect.DeepEqual(p.Lines, want) {
		t.Fatalf("got %v, want %v", p.Lines, want)
	}
	if p.Size != int64(len(content)) {
		t.Fatalf("size = %d, want %d", p.Size, len(content))
	}
}

func TestReadFilePreview_BinaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.bin")
	data := append([]byte("PNG"), 0x00, 0x01, 0x02)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := fsutil.ReadFilePreview(path)
	if err != nil {
		t.Fatalf("ReadFilePreview: %v", err)
	}
	if !p.Binary {
		t.Fatal("expected binary detection")
	}
	if p.Size != int64(len(data)) {
		t.Fatalf("size = %d, want %d", p.Size, len(data))
	}
}
