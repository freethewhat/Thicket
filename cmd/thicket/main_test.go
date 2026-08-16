package main

import (
	"errors"
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
