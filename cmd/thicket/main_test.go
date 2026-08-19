package main

import (
	"errors"
	"strings"
	"testing"
)

var errOutputWrite = errors.New("stdout unavailable")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errOutputWrite
}

func TestWriteSelectionReturnsErrorWhenOutputWriteFails(t *testing.T) {
	if err := writeSelection(failingWriter{}, "/selected/path"); !errors.Is(err, errOutputWrite) {
		t.Fatalf("writeSelection() error = %v, want %v", err, errOutputWrite)
	}
}

func TestHelpTextContainsUsageAndKeybindings(t *testing.T) {
	out := helpText()
	for _, want := range []string{"Usage:", "thicket [path]", "-h | --help", "Move selection up", "Toggle this help screen", "Copy the highlighted entry's path to the clipboard"} {
		if !strings.Contains(out, want) {
			t.Fatalf("helpText() missing %q:\n%s", want, out)
		}
	}
}
