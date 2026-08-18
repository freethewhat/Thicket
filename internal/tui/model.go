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
// cursorMemory is a session-scoped cache of past activeCursor values per
// directory (spec §5's anticipated follow-up), not additional
// authoritative state; see its doc comment below.
type Model struct {
	activePath    string
	activeEntries []fsutil.Entry
	activeCursor  int // -1 when the active directory is empty
	activeScroll  int
	// cursorMemory remembers the last activeCursor seen in each directory
	// (keyed by absolute path), for the current process only — never
	// persisted to disk. handleRight/handleLeft (internal/tui/update.go)
	// write to it immediately before they change activePath, so it always
	// reflects wherever the cursor was left in the directory being
	// departed. handleRight consults it when re-entering a directory, to
	// restore that position instead of resetting to row 0; handleLeft
	// only writes — its own cursor placement is fully determined by
	// child-name lookup and does not need to read this map. This is the
	// "contained, addressable follow-up" spec
	// docs/superpowers/specs/2026-08-15-thicket-tui-file-browser-design.md
	// §5 anticipated: cursorMemory is itself derived (a cache of past
	// activeCursor values), not authoritative state a transition depends
	// on to be correct, so it does not change the "derived from
	// activePath plus two integers" model below.
	cursorMemory map[string]int
	showHidden   bool
	statusErr    string
	// checkVersion/updateNotice: on-launch update-check state (spec
	// docs/superpowers/specs/2026-08-17-thicket-update-check-design.md).
	// checkVersion is set once via WithUpdateCheck before the Bubble Tea
	// program starts; empty ("" or "dev") disables the check entirely —
	// Init returns nil and no network request is ever made. updateNotice
	// is populated by Update's updateAvailableMsg case and rendered by
	// statusLine; it self-clears via a tea.Tick-scheduled
	// clearUpdateNoticeMsg, mirroring statusErr's shape but with a timed
	// auto-dismiss statusErr does not have.
	checkVersion string
	updateNotice string
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
	// findMode/findQuery/findResults/findCursor/findScroll/findTruncated:
	// recursive find state (spec
	// docs/superpowers/specs/2026-08-16-thicket-recursive-find-design.md).
	// Zero-valued at construction, same as searchMode/helpMode. findResults
	// is captured once when f is pressed and held for the session; it is
	// never re-walked while findMode is true, only re-filtered via
	// filterWalk. findScroll persists the result-list scroll offset the
	// same way activeScroll does for the Miller column (see clampScroll/
	// clampFindScroll in update.go) — findCursor is user-driven, unlike
	// scrollStartFor's pin-to-bottom behavior used by the derived
	// ancestor/preview columns. Mutually exclusive with searchMode/
	// helpMode/markSetPending/markJumpPending/marksListMode — Update's
	// early-return dispatch order guarantees only one mode is ever active.
	findMode      bool
	findQuery     string
	findResults   []fsutil.WalkEntry
	findCursor    int // index into filterWalk(findResults, findQuery); -1 when that view is empty
	findScroll    int // scroll window start into filterWalk(findResults, findQuery); see clampFindScroll
	findTruncated bool
	// markTable/marksPath/markSetPending/markJumpPending/marksListMode/
	// marksCursor: directory marks (bookmarks) state (spec
	// docs/superpowers/specs/2026-08-16-thicket-directory-marks-design.md
	// §4). markTable and marksPath are populated once in New(); the rest
	// are zero-valued at construction, same as searchMode/helpMode.
	// markSetPending/markJumpPending/marksListMode are mutually exclusive
	// with each other and with searchMode/helpMode/findMode — Update's
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
	m := Model{activePath: abs, cursorMemory: make(map[string]int)}
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

// WithUpdateCheck returns a copy of m configured to check for a newer
// release when the Bubble Tea program starts, comparing against
// currentVersion. An empty currentVersion (or "dev") disables the check
// entirely: Init returns nil and no network request is ever made.
// cmd/thicket computes currentVersion once from the build's version and
// THICKET_NO_UPDATE_CHECK before calling this method.
func (m Model) WithUpdateCheck(currentVersion string) Model {
	m.checkVersion = currentVersion
	return m
}

func (m Model) Init() tea.Cmd {
	if m.checkVersion == "" || m.checkVersion == "dev" {
		return nil
	}
	return checkUpdateCmd(m.checkVersion)
}

// Result reports the outcome after the program quits: ok is true only if
// the user pressed Enter (as opposed to q/Esc/Ctrl-C).
func (m Model) Result() (path string, ok bool) {
	return m.chosenPath, m.selected
}
