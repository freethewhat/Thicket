package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"thicket/internal/fsutil"
	marksPkg "thicket/internal/marks"
)

// Model is the Bubble Tea model for the Miller-column browser. Navigation
// state is derived from activePath plus two integers (activeCursor,
// activeScroll) rather than a cached tree of panes — see spec §5.
type Model struct {
	activePath    string
	activeEntries []fsutil.Entry
	activeCursor  int // -1 when the active directory is empty
	activeScroll  int
	showHidden    bool
	statusErr     string
	// searchMode/searchQuery/searchNoMatch/searchPrevCursor: type-ahead
	// search state (spec docs/superpowers/specs/2026-08-16-thicket-type-ahead-search-design.md).
	// Zero-valued at construction — no change to New()'s behavior.
	searchMode       bool
	searchQuery      string
	searchNoMatch    bool
	searchPrevCursor int
	// helpMode: in-app help screen state (? toggles it open/closed; see
	// internal/tui/help.go). Mutually exclusive with searchMode/findMode/
	// markSetPending/markJumpPending/marksListMode — Update's early-return
	// dispatch order guarantees only one is ever active.
	helpMode bool
	// findMode/findQuery/findResults/findCursor/findTruncated: recursive
	// find state (spec
	// docs/superpowers/specs/2026-08-16-thicket-recursive-find-design.md).
	// Zero-valued at construction, same as searchMode/helpMode. findResults
	// is captured once when f is pressed and held for the session; it is
	// never re-walked while findMode is true, only re-filtered via
	// filterWalk. Mutually exclusive with searchMode/helpMode/
	// markSetPending/markJumpPending/marksListMode — Update's early-return
	// dispatch order guarantees only one mode is ever active.
	findMode      bool
	findQuery     string
	findResults   []fsutil.WalkEntry
	findCursor    int // index into filterWalk(findResults, findQuery); -1 when that view is empty
	findTruncated bool
	// markTable/marksPath/markSetPending/markJumpPending/marksListMode/
	// marksCursor: directory marks (bookmarks) state (spec
	// docs/superpowers/specs/2026-08-16-thicket-directory-marks-design.md
	// §4). markTable and marksPath are populated once in New(); the rest
	// are zero-valued at construction, same as searchMode/helpMode.
	// markSetPending/markJumpPending/marksListMode are mutually exclusive
	// with each other and with searchMode/helpMode — Update's early-return
	// dispatch order guarantees only one mode is ever active. The field is
	// named markTable, not marks, to avoid colliding with the marksPkg
	// import alias used throughout this package.
	markTable       marksPkg.Marks
	marksPath       string
	markSetPending  bool
	markJumpPending bool
	marksListMode   bool
	marksCursor     int // -1 when markTable is empty
	width           int
	height          int
	quitting        bool
	selected        bool
	chosenPath      string
}

// New builds a Model rooted at startPath. startPath must be readable;
// a missing/inaccessible starting directory is a construction error
// (cmd/thicket exits 2 on this, per spec §4). marksPath is where the
// directory-marks table (internal/marks) is loaded from and saved to. An
// existing-but-unreadable marks file is also a construction error, for the
// same reason a bad startPath is: silently starting with an empty
// in-memory mark table would let the very next m<letter> press silently
// overwrite marks that already exist on disk.
func New(startPath, marksPath string) (Model, error) {
	abs, err := filepath.Abs(startPath)
	if err != nil {
		return Model{}, err
	}
	m := Model{activePath: abs}
	entries, err := fsutil.ListDir(m.activePath, m.showHidden)
	if err != nil {
		return Model{}, err
	}
	m.activeEntries = entries
	if len(entries) == 0 {
		m.activeCursor = -1
	}
	m.marksPath = marksPath
	loaded, err := marksPkg.Load(marksPath)
	if err != nil {
		return Model{}, err
	}
	m.markTable = loaded
	m.marksCursor = marksListCursorFor(loaded)
	return m, nil
}

func (m Model) Init() tea.Cmd {
	return nil
}

// Result reports the outcome after the program quits: ok is true only if
// the user pressed Enter (as opposed to q/Esc/Ctrl-C).
func (m Model) Result() (path string, ok bool) {
	return m.chosenPath, m.selected
}
