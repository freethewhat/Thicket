// Package scripts embeds install.sh so internal/update can run it without
// a runtime network fetch of the script itself. install.sh stays directly
// curl-able at its existing path/URL — this file only adds a Go-importable
// copy of the same bytes.
package scripts

import _ "embed"

//go:embed install.sh
var InstallSh []byte
