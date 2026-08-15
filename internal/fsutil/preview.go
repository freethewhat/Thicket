package fsutil

import (
	"bytes"
	"io"
	"os"
	"strings"
)

const (
	previewReadLimit = 64 * 1024
	binarySniffLen  = 8000
)

// FilePreview is the content shown in the preview pane for a regular file.
type FilePreview struct {
	Binary bool
	Lines  []string
	Size   int64
}

// ReadFilePreview reads up to previewReadLimit bytes from path and detects
// binary content via a NUL byte in the first binarySniffLen bytes.
func ReadFilePreview(path string) (FilePreview, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FilePreview{}, err
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
