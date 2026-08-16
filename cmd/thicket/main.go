package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"thicket/internal/marks"
	"thicket/internal/tui"
)

func main() {
	start := "."
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help":
			fmt.Print(helpText())
			return
		default:
			start = os.Args[1]
		}
	}

	marksPath, err := marks.DefaultPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "thicket: %v\n", err)
		os.Exit(2)
	}

	m, err := tui.New(start, marksPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "thicket: %v\n", err)
		os.Exit(2)
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "thicket: opening /dev/tty: %v\n", err)
		os.Exit(2)
	}
	defer tty.Close()

	// lipgloss's default renderer detects color capability from os.Stdout.
	// The wrapper shells (th) capture thicket's stdout via command
	// substitution to read the selected directory, which turns os.Stdout
	// into a pipe and makes lipgloss downgrade to a colorless profile even
	// though the UI itself renders to the real tty. Bind the renderer to
	// that tty so color/border detection reflects the actual display.
	//
	// SetDefaultRenderer alone only affects styles built after this point:
	// internal/tui's package-level styles are constructed during Go's init
	// phase (before main runs), so each already captured a reference to the
	// stdout-bound renderer. tui.SetRenderer rebinds those existing styles.
	ttyRenderer := lipgloss.NewRenderer(tty)
	lipgloss.SetDefaultRenderer(ttyRenderer)
	tui.SetRenderer(ttyRenderer)

	p := tea.NewProgram(m, tea.WithInput(tty), tea.WithOutput(tty), tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "thicket: %v\n", err)
		os.Exit(2)
	}

	path, ok := finalModel.(tui.Model).Result()
	if !ok {
		os.Exit(1)
	}
	if err := writeSelection(os.Stdout, path); err != nil {
		fmt.Fprintf(os.Stderr, "thicket: writing selected path: %v\n", err)
		os.Exit(2)
	}
}

func writeSelection(w io.Writer, path string) error {
	_, err := fmt.Fprintln(w, path)
	return err
}

// helpText builds the --help output: usage plus the same keybinding table
// shown by the in-app ? help screen (tui.Keybindings) and documented in
// man/thicket.1 — keep all three in sync when a binding changes.
func helpText() string {
	var b strings.Builder
	b.WriteString("thicket - Miller-column TUI file browser for the terminal\n\n")
	b.WriteString("Usage:\n")
	b.WriteString("  thicket [path]\n")
	b.WriteString("  thicket -h | --help\n\n")
	b.WriteString("Arguments:\n")
	b.WriteString("  path    Directory to start browsing in (default: current directory)\n\n")
	b.WriteString("Keys:\n")
	for _, kb := range tui.Keybindings {
		fmt.Fprintf(&b, "  %-16s%s\n", kb.Keys, kb.Action)
	}
	b.WriteString("\nSee man thicket for more.\n")
	return b.String()
}
