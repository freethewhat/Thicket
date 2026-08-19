// internal/clipboard/clipboard_test.go
package clipboard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeStub creates an executable shell script named name inside dir,
// running body as its only statement.
func writeStub(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	content := "#!/bin/sh\n" + body
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func TestCopy_PrefersWlCopyWhenWaylandDisplaySet(t *testing.T) {
	binDir := t.TempDir()
	wlOut := filepath.Join(t.TempDir(), "wl-out")
	xclipOut := filepath.Join(t.TempDir(), "xclip-out")
	writeStub(t, binDir, "wl-copy", fmt.Sprintf("cat > %q\n", wlOut))
	writeStub(t, binDir, "xclip", fmt.Sprintf("cat > %q\n", xclipOut))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")

	if err := Copy("hello"); err != nil {
		t.Fatalf("Copy() error = %v, want nil", err)
	}
	got, err := os.ReadFile(wlOut)
	if err != nil {
		t.Fatalf("wl-copy was not invoked: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("wl-copy stdin = %q, want %q", got, "hello")
	}
	if _, err := os.Stat(xclipOut); !os.IsNotExist(err) {
		t.Fatalf("xclip should not have been invoked when wl-copy succeeds")
	}
}

func TestCopy_PrefersXclipWhenWaylandDisplayUnset(t *testing.T) {
	binDir := t.TempDir()
	wlOut := filepath.Join(t.TempDir(), "wl-out")
	xclipOut := filepath.Join(t.TempDir(), "xclip-out")
	writeStub(t, binDir, "wl-copy", fmt.Sprintf("cat > %q\n", wlOut))
	writeStub(t, binDir, "xclip", fmt.Sprintf("cat > %q\n", xclipOut))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WAYLAND_DISPLAY", "")

	if err := Copy("hello"); err != nil {
		t.Fatalf("Copy() error = %v, want nil", err)
	}
	if _, err := os.ReadFile(xclipOut); err != nil {
		t.Fatalf("xclip was not invoked: %v", err)
	}
	if _, err := os.Stat(wlOut); !os.IsNotExist(err) {
		t.Fatalf("wl-copy should not have been invoked when WAYLAND_DISPLAY is unset")
	}
}

func TestCopy_FallsBackWhenPreferredMechanismExitsNonZero(t *testing.T) {
	binDir := t.TempDir()
	xclipOut := filepath.Join(t.TempDir(), "xclip-out")
	writeStub(t, binDir, "wl-copy", "exit 1\n")
	writeStub(t, binDir, "xclip", fmt.Sprintf("cat > %q\n", xclipOut))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")

	if err := Copy("hello"); err != nil {
		t.Fatalf("Copy() error = %v, want nil (should fall back to xclip)", err)
	}
	got, err := os.ReadFile(xclipOut)
	if err != nil || string(got) != "hello" {
		t.Fatalf("xclip stdin = %q, %v, want %q, nil", got, err, "hello")
	}
}

func TestCopy_FallsBackWhenPreferredMechanismTimesOut(t *testing.T) {
	binDir := t.TempDir()
	xclipOut := filepath.Join(t.TempDir(), "xclip-out")
	writeStub(t, binDir, "wl-copy", "sleep 5\n")
	writeStub(t, binDir, "xclip", fmt.Sprintf("cat > %q\n", xclipOut))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")

	if err := Copy("hello"); err != nil {
		t.Fatalf("Copy() error = %v, want nil (should fall back to xclip after wl-copy times out)", err)
	}
	if got, err := os.ReadFile(xclipOut); err != nil || string(got) != "hello" {
		t.Fatalf("xclip stdin = %q, %v, want %q, nil", got, err, "hello")
	}
}

func TestCopy_AllMechanismsFailReturnsJoinedError(t *testing.T) {
	binDir := t.TempDir()
	writeStub(t, binDir, "wl-copy", "exit 1\n")
	writeStub(t, binDir, "xclip", "exit 1\n")
	writeStub(t, binDir, "xsel", "exit 1\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")

	err := Copy("hello")
	if err == nil {
		t.Fatal("Copy() error = nil, want non-nil when every found mechanism fails")
	}
	if errors.Is(err, ErrNoMechanism) {
		t.Fatalf("Copy() error = %v, want a run failure, not ErrNoMechanism (mechanisms were found)", err)
	}
}

func TestCopy_NoMechanismOnPathReturnsErrNoMechanism(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	err := Copy("hello")
	if !errors.Is(err, ErrNoMechanism) {
		t.Fatalf("Copy() error = %v, want ErrNoMechanism", err)
	}
}
