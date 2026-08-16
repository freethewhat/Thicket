package main

import (
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"thicket/internal/tui"
)

func main() {
	start := "."
	if len(os.Args) > 1 {
		start = os.Args[1]
	}

	m, err := tui.New(start)
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
