package update

import (
	"bytes"
	"context"
	"os"
	"os/exec"

	"thicket/scripts"
)

// Run installs the latest thicket release by piping the embedded
// install.sh to sh — the same "curl .../install.sh | sh" flow documented
// in README.md, minus the curl fetch of the script itself. No version
// argument is passed, so install.sh resolves "latest" itself.
//
// cmd.Stdin is the script's own bytes, not interactive input: install.sh
// makes no further reads from its stdin. cmd.Stdout/cmd.Stderr are
// inherited from the current process so curl's progress output and any
// sudo password prompt (sudo reads that from the controlling tty, not
// from this process's Stdin) behave identically to running install.sh
// directly. PREFIX/VERSION and all other environment variables are
// inherited unchanged from the current process — install.sh already
// reads them itself, no special-casing needed here.
func Run(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "sh", "-s", "--")
	cmd.Stdin = bytes.NewReader(scripts.InstallSh)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
