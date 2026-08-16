package fsutil

import (
	"bytes"
	"io"
	"os"
	"strings"
)

const (
	previewReadLimit = 64 * 1024
	binarySniffLen   = 8000
)

// FilePreview is the content shown in the preview pane for a regular file.
// Special is set for paths that exist but are not a regular file (e.g. a
// FIFO, device, or socket) — these are reported without ever calling
// os.Open, since opening a FIFO with no writer blocks indefinitely and
// would freeze the whole TUI (View runs inline on the Bubble Tea event
// loop).
type FilePreview struct {
	Binary  bool
	Special bool
	Lines   []string
	Size    int64
}

// ReadFilePreview reads up to previewReadLimit bytes from path and detects
// binary content via a NUL byte in the first binarySniffLen bytes.
func ReadFilePreview(path string) (FilePreview, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FilePreview{}, err
	}
	if !info.Mode().IsRegular() {
		return FilePreview{Special: true, Size: info.Size()}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return FilePreview{}, err
	}
	defer f.Close()

	buf := make([]byte, previewReadLimit)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return FilePreview{}, err
	}
	buf = buf[:n]

	sniff := buf
	if len(sniff) > binarySniffLen {
		sniff = sniff[:binarySniffLen]
	}
	if bytes.IndexByte(sniff, 0) != -1 {
		return FilePreview{Binary: true, Size: info.Size()}, nil
	}

	return FilePreview{Lines: strings.Split(string(buf), "\n"), Size: info.Size()}, nil
}
