// internal/clipboard/clipboard.go
package clipboard

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrNoMechanism is returned when none of wl-copy/xclip/xsel is found on
// $PATH.
var ErrNoMechanism = errors.New("no clipboard mechanism found (install xclip, wl-copy, or xsel)")

// copyTimeout bounds each individual mechanism attempt, not the whole
// Copy call — see Copy's doc comment.
const copyTimeout = 1 * time.Second

// candidateCommands returns the ordered list of clipboard-helper argv
// slices to try, based on whether $WAYLAND_DISPLAY is set. Every
// mechanism found on $PATH is attempted in this order, not just the
// first — see Copy's doc comment.
func candidateCommands(waylandDisplay string) [][]string {
	wl := []string{"wl-copy"}
	xclip := []string{"xclip", "-selection", "clipboard"}
	xsel := []string{"xsel", "--clipboard", "--input"}
	if waylandDisplay != "" {
		return [][]string{wl, xclip, xsel}
	}
	return [][]string{xclip, xsel, wl}
}

// Copy writes text to the system clipboard, trying every mechanism found
// on PATH in preference order until one succeeds: wl-copy, xclip, xsel
// when $WAYLAND_DISPLAY is set, else xclip, xsel, wl-copy. A
// preferred-but-non-functional helper (e.g. wl-copy present via XWayland
// with no running Wayland compositor) does not mask a working fallback —
// mixed X11/Wayland setups are the common case this guards against. Each
// candidate is bounded individually by copyTimeout, so one hung mechanism
// yields to the next rather than blocking the whole call indefinitely.
// Returns ErrNoMechanism if none of the three binaries is on PATH;
// otherwise, if every found mechanism's attempt failed, returns their
// combined errors via errors.Join.
func Copy(text string) error {
	commands := candidateCommands(os.Getenv("WAYLAND_DISPLAY"))
	var found bool
	var errs []error
	for _, argv := range commands {
		path, err := exec.LookPath(argv[0])
		if err != nil {
			continue
		}
		found = true
		if err := runCopy(path, argv[1:], text); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", argv[0], err))
			continue
		}
		return nil
	}
	if !found {
		return ErrNoMechanism
	}
	return errors.Join(errs...)
}

// runCopy runs path with the given args, writing text to its stdin,
// bounded by copyTimeout.
func runCopy(path string, args []string, text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), copyTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
