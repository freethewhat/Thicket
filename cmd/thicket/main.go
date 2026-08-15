package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
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
	fmt.Fprintln(os.Stdout, path)
}
