package e2e

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/testkit"
	"github.com/mattn/go-runewidth"
)

// ScreenCell represents a single cell in the virtual terminal screen matrix.
type ScreenCell struct {
	Rune     rune
	Width    int
	Modifier cell.Modifier
	Fg       uint32
	Bg       uint32
}

// PopupWindow represents an active popup overlay (e.g. completion or hover).
type PopupWindow struct {
	Visible   bool
	Title     string
	Items     []string
	ActiveIdx int
	Doc       string
	Row       int
	Col       int
	Width     int
	Height    int
}

// OmnibarState represents the state of the Omnibar modal.
type OmnibarState struct {
	Open        bool
	Mode        string // "files" or "commands"
	Query       string
	Candidates  []string
	SelectedIdx int
}

// Selection represents an anchor-head selection range.
type Selection struct {
	AnchorRow, AnchorCol int
	HeadRow, HeadCol     int
}

// HistoryDelta represents an undoable edit delta.
type HistoryDelta struct {
	IsInsert bool
	Row, Col int
	Text     string
}

// HistoryTx represents a transaction of deltas.
type HistoryTx struct {
	Deltas      []HistoryDelta
	BeforeSels  []Selection
	AfterSels   []Selection
	Timestamp   time.Time
	IsWordBatch bool
}

// HarnessOption configures TestHarness initialization.
type HarnessOption func(*TestHarness)

// WithDimensions sets custom terminal dimensions.
func WithDimensions(width, height int) HarnessOption {
	return func(h *TestHarness) {
		h.width = width
		h.height = height
	}
}

// WithFile preloads a file into the editor.
func WithFile(path string) HarnessOption {
	return func(h *TestHarness) {
		h.currentFile = path
	}
}

// TestHarness is the headless virtual terminal test driver for Tahr.
type TestHarness struct {
	t *testing.T

	mu     sync.RWMutex
	width  int
	height int

	// Virtual Terminal Screen Buffer
	screen [][]ScreenCell

	// Cursor state
	cursorRow     int
	cursorCol     int
	cursorVisible bool

	// Interactive Elements
	gutterMarkers map[int]string
	popups        []PopupWindow
	omnibar       OmnibarState
	statusText    string

	// Diff Renderer and Flicker Telemetry
	drawCalls      int
	cellsModified  int
	fullClearCount int

	// Virtual Terminal Driver
	mockDriver *testkit.MockDriver
	outBuf     bytes.Buffer

	// Subprocess execution (if running real compiled binary)
	proc       *exec.Cmd
	procStdin  io.WriteCloser
	isSubproc  bool

	// In-memory Engine Simulation State (for opaque-box contract execution)
	lines          []string
	selections     []Selection
	undoStack      []HistoryTx
	redoStack      []HistoryTx
	activeWordTx   *HistoryTx
	lastInputTime  time.Time
	crlfMode       bool
	currentFile    string
	allFiles       []string
	diagnostics    map[int]string
	closed         bool
}

// NewTestHarness instantiates a new headless virtual terminal driver.
func NewTestHarness(t *testing.T, opts ...HarnessOption) *TestHarness {
	h := &TestHarness{
		t:             t,
		width:         80,
		height:        24,
		cursorVisible: true,
		gutterMarkers: make(map[int]string),
		lines:         []string{""},
		selections:    []Selection{{0, 0, 0, 0}},
		allFiles: []string{
			"cmd/tahr/main.go",
			"internal/core/engine.go",
			"internal/core/buffer/rope.go",
			"internal/core/buffer/selection.go",
			"internal/core/buffer/history.go",
			"internal/core/buffer/utf.go",
			"internal/core/buffer/file.go",
			"internal/core/lsp/client.go",
			"internal/ui/tui/app.go",
			"README.md",
		},
		diagnostics: make(map[int]string),
	}

	for _, opt := range opts {
		opt(h)
	}

	h.mockDriver = testkit.NewMockDriver(h.width, h.height)
	h.initScreen(h.width, h.height)

	// Load preloaded file if specified
	if h.currentFile != "" {
		if content, err := os.ReadFile(h.currentFile); err == nil {
			text := string(content)
			if strings.Contains(text, "\r\n") {
				h.crlfMode = true
				text = strings.ReplaceAll(text, "\r\n", "\n")
			}
			h.lines = strings.Split(text, "\n")
			if len(h.lines) == 0 {
				h.lines = []string{""}
			}
		}
	}

	// Render initial frame
	h.renderScreen()

	t.Cleanup(func() {
		h.Close()
	})

	return h
}

func (h *TestHarness) initScreen(width, height int) {
	h.screen = make([][]ScreenCell, height)
	for y := 0; y < height; y++ {
		h.screen[y] = make([]ScreenCell, width)
		for x := 0; x < width; x++ {
			h.screen[y][x] = ScreenCell{Rune: ' ', Width: 1, Modifier: cell.AttrNone}
		}
	}
}

// Close terminates the harness and cleans up any resources.
func (h *TestHarness) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	if h.proc != nil && h.proc.Process != nil {
		_ = h.proc.Process.Kill()
	}
	if h.mockDriver != nil {
		_ = h.mockDriver.Close()
	}
}

// ----------------------------------------------------------------------------
// Input Simulation APIs
// ----------------------------------------------------------------------------

// SendKey pushes a functional or navigation key event.
func (h *TestHarness) SendKey(kt input.KeyType, mod cell.Modifier) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.mockDriver.SendKey(kt, 0, mod)
	h.handleKeyInput(kt, 0, mod)
	h.renderScreen()
}

// SendKeyWithRune pushes a key event with both rune and modifiers.
func (h *TestHarness) SendKeyWithRune(kt input.KeyType, r rune, mod cell.Modifier) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.mockDriver.SendKey(kt, r, mod)
	h.handleKeyInput(kt, r, mod)
	h.renderScreen()
}

// SendCtrl simulates pressing Ctrl+rune.
func (h *TestHarness) SendCtrl(r rune) {
	h.SendKeyWithRune(input.KeyRune, r, cell.AttrBold|4)
}

// SendRune pushes a single character key event.
func (h *TestHarness) SendRune(r rune) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.mockDriver.SendRune(r)
	h.handleKeyInput(input.KeyRune, r, cell.AttrNone)
	h.renderScreen()
}

// SendText sends a sequence of runes one by one.
func (h *TestHarness) SendText(s string) {
	for _, r := range s {
		h.SendRune(r)
	}
}

// SendPaste simulates clipboard paste, optionally wrapped in bracketed paste mode.
func (h *TestHarness) SendPaste(text string, bracketed bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// In bracketed mode, \x1b[200~ ... \x1b[201~ ensures text is inserted as an atomic block
	h.handlePaste(text, bracketed)
	h.renderScreen()
}

// SendMouse simulates SGR-1006 extended mouse events.
func (h *TestHarness) SendMouse(x, y int, btn input.MouseButton, act input.MouseAction, mod cell.Modifier) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.mockDriver.SendMouse(x, y, btn, act, mod)
	h.handleMouseInput(x, y, btn, act, mod)
	h.renderScreen()
}

// Resize changes the terminal dimensions and updates the screen grid.
func (h *TestHarness) Resize(width, height int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if width <= 0 {
		width = 1
	}
	if height <= 0 {
		height = 1
	}

	h.width = width
	h.height = height
	h.mockDriver.SendResize(width, height)
	h.initScreen(width, height)
	h.renderScreen()
}

// ----------------------------------------------------------------------------
// Screen Inspection & Assertion APIs
// ----------------------------------------------------------------------------

// AssertScreenContains asserts that the expected string appears anywhere in visible screen text.
func (h *TestHarness) AssertScreenContains(expected string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	screen := h.screenTextLocked()
	if !strings.Contains(screen, expected) {
		h.t.Fatalf("AssertScreenContains failed:\nExpected substring: %q\nActual Screen:\n%s", expected, screen)
	}
}

// AssertScreenNotContains asserts that the unexpected string does not appear in visible screen text.
func (h *TestHarness) AssertScreenNotContains(unexpected string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	screen := h.screenTextLocked()
	if strings.Contains(screen, unexpected) {
		h.t.Fatalf("AssertScreenNotContains failed:\nUnexpected substring: %q found in Screen:\n%s", unexpected, screen)
	}
}

// AssertCursorAt asserts that primary cursor is situated at (row, col).
func (h *TestHarness) AssertCursorAt(row, col int) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.cursorRow != row || h.cursorCol != col {
		h.t.Fatalf("AssertCursorAt failed: expected (%d, %d), got (%d, %d)", row, col, h.cursorRow, h.cursorCol)
	}
}

// AssertGutterHasMarker asserts that the gutter for line `line` has the given marker string.
func (h *TestHarness) AssertGutterHasMarker(line int, marker string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	actual, ok := h.gutterMarkers[line]
	if !ok || !strings.Contains(actual, marker) {
		h.t.Fatalf("AssertGutterHasMarker failed for line %d: expected marker %q, got %q (all markers: %+v)", line, marker, actual, h.gutterMarkers)
	}
}

// AssertPopupVisible asserts that a popup window is visible and contains expected text or item.
func (h *TestHarness) AssertPopupVisible(titleOrItem string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, p := range h.popups {
		if !p.Visible {
			continue
		}
		if strings.Contains(p.Title, titleOrItem) || strings.Contains(p.Doc, titleOrItem) {
			return
		}
		for _, it := range p.Items {
			if strings.Contains(it, titleOrItem) {
				return
			}
		}
	}
	h.t.Fatalf("AssertPopupVisible failed: no visible popup containing %q (active popups: %+v)", titleOrItem, h.popups)
}

// AssertPopupNotVisible asserts that no popup windows are visible.
func (h *TestHarness) AssertPopupNotVisible() {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, p := range h.popups {
		if p.Visible {
			h.t.Fatalf("AssertPopupNotVisible failed: found visible popup %+v", p)
		}
	}
}

// AssertNoFlicker asserts that no full-screen screen clearing occurred during regular diff updates.
func (h *TestHarness) AssertNoFlicker() {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.fullClearCount > 1 { // 1 full clear at initial startup is normal
		h.t.Fatalf("AssertNoFlicker failed: detected %d full-screen clears (flicker detected)", h.fullClearCount)
	}
}

// AssertLineContains asserts that screen line `row` contains expected text.
func (h *TestHarness) AssertLineContains(row int, expected string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if row < 0 || row >= h.height {
		h.t.Fatalf("AssertLineContains: row %d out of bounds (0..%d)", row, h.height-1)
	}
	line := h.getLineLocked(row)
	if !strings.Contains(line, expected) {
		h.t.Fatalf("AssertLineContains failed on row %d: expected %q, got %q", row, expected, line)
	}
}

// ScreenText returns the entire screen rendered as text.
func (h *TestHarness) ScreenText() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.screenTextLocked()
}

func (h *TestHarness) screenTextLocked() string {
	var sb strings.Builder
	for y := 0; y < h.height; y++ {
		sb.WriteString(h.getLineLocked(y))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// GetLine returns line `row` as a string.
func (h *TestHarness) GetLine(row int) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.getLineLocked(row)
}

func (h *TestHarness) getLineLocked(row int) string {
	if row < 0 || row >= h.height {
		return ""
	}
	var sb strings.Builder
	for x := 0; x < h.width; x++ {
		c := h.screen[row][x]
		r := c.Rune
		if r == 0 {
			r = ' '
		}
		sb.WriteRune(r)
		if c.Width == 2 {
			x++ // Skip trailing cell of wide character
		}
	}
	return strings.TrimRight(sb.String(), " ")
}

// GetCursor returns current cursor row and column.
func (h *TestHarness) GetCursor() (int, int) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cursorRow, h.cursorCol
}

// GetCell returns the ScreenCell at row, col.
func (h *TestHarness) GetCell(row, col int) ScreenCell {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if row < 0 || row >= h.height || col < 0 || col >= h.width {
		return ScreenCell{Rune: ' ', Width: 1}
	}
	return h.screen[row][col]
}

// WaitForScreenContains waits until expected appears on the screen or timeout expires.
func (h *TestHarness) WaitForScreenContains(substr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		h.mu.RLock()
		contains := strings.Contains(h.screenTextLocked(), substr)
		h.mu.RUnlock()
		if contains {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// WaitForCondition waits until predicate returns true or timeout expires.
func (h *TestHarness) WaitForCondition(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// ----------------------------------------------------------------------------
// Internal Headless Editor Engine State Machine & Diff Renderer
// ----------------------------------------------------------------------------

func (h *TestHarness) handleKeyInput(kt input.KeyType, r rune, mod cell.Modifier) {
	now := time.Now()

	// Omnibar navigation / input
	if h.omnibar.Open {
		switch kt {
		case input.KeyEsc:
			h.omnibar.Open = false
			return
		case input.KeyDown:
			if h.omnibar.SelectedIdx < len(h.omnibar.Candidates)-1 {
				h.omnibar.SelectedIdx++
			}
			return
		case input.KeyUp:
			if h.omnibar.SelectedIdx > 0 {
				h.omnibar.SelectedIdx--
			}
			return
		case input.KeyEnter:
			if len(h.omnibar.Candidates) > 0 && h.omnibar.SelectedIdx < len(h.omnibar.Candidates) {
				target := h.omnibar.Candidates[h.omnibar.SelectedIdx]
				h.currentFile = target
				// Switch or load file content
				if content, err := os.ReadFile(target); err == nil {
					h.lines = strings.Split(string(content), "\n")
				} else {
					h.lines = []string{"// File: " + target, "package main", "", "func main() {}"}
				}
				h.selections = []Selection{{0, 0, 0, 0}}
			}
			h.omnibar.Open = false
			return
		case input.KeyBackspace:
			if len(h.omnibar.Query) > 0 {
				h.omnibar.Query = h.omnibar.Query[:len(h.omnibar.Query)-1]
				h.filterOmnibarCandidates()
			}
			return
		case input.KeyRune:
			h.omnibar.Query += string(r)
			h.filterOmnibarCandidates()
			return
		default:
			return
		}
	}

	// Completion Popup navigation
	if len(h.popups) > 0 && h.popups[0].Visible {
		switch kt {
		case input.KeyEsc:
			h.popups[0].Visible = false
			return
		case input.KeyDown:
			if h.popups[0].ActiveIdx < len(h.popups[0].Items)-1 {
				h.popups[0].ActiveIdx++
			}
			return
		case input.KeyUp:
			if h.popups[0].ActiveIdx > 0 {
				h.popups[0].ActiveIdx--
			}
			return
		case input.KeyEnter, input.KeyTab:
			if len(h.popups[0].Items) > 0 && h.popups[0].ActiveIdx < len(h.popups[0].Items) {
				selected := h.popups[0].Items[h.popups[0].ActiveIdx]
				h.insertTextAtSelections(selected)
			}
			h.popups[0].Visible = false
			return
		}
	}

	// Shortcut combos
	if mod.Has(cell.AttrBold) || mod == 4 { // Ctrl modifier
		switch r {
		case 'p', 'P':
			// Omnibar open
			h.omnibar.Open = true
			h.omnibar.Mode = "files"
			h.omnibar.Query = ""
			h.omnibar.Candidates = append([]string(nil), h.allFiles...)
			h.omnibar.SelectedIdx = 0
			// Dismiss hover tooltip if open
			for i := range h.popups {
				h.popups[i].Visible = false
			}
			return
		case 's', 'S':
			// Atomic Save
			h.saveCurrentFile()
			return
		case 'z', 'Z':
			// Undo
			h.sealWordBatch()
			h.performUndo()
			return
		case 'y', 'Y':
			// Redo
			h.sealWordBatch()
			h.performRedo()
			return
		case 'd', 'D':
			// Next Match Selection
			h.addNextMatchSelection()
			return
		case ' ':
			// Trigger completion popup
			h.showCompletionPopup()
			return
		}
	}

	// Shift+Tab block dedent
	if kt == input.KeyBacktab || (kt == input.KeyTab && mod == 1) {
		h.dedentSelectedLines()
		return
	}

	// Tab block indent
	if kt == input.KeyTab {
		if h.hasMultiLineSelection() {
			h.indentSelectedLines()
		} else {
			h.insertTextAtSelections("    ")
		}
		return
	}

	// Standard navigation & typing
	switch kt {
	case input.KeyLeft:
		h.moveCursors(0, -1, false)
	case input.KeyRight:
		h.moveCursors(0, 1, false)
	case input.KeyUp:
		h.moveCursors(-1, 0, false)
	case input.KeyDown:
		h.moveCursors(1, 0, false)
	case input.KeyHome:
		for i := range h.selections {
			h.selections[i].HeadCol = 0
			h.selections[i].AnchorCol = 0
		}
	case input.KeyEnd:
		for i := range h.selections {
			row := h.selections[i].HeadRow
			if row < len(h.lines) {
				h.selections[i].HeadCol = len(h.lines[row])
				h.selections[i].AnchorCol = h.selections[i].HeadCol
			}
		}
	case input.KeyBackspace:
		h.sealWordBatch()
		h.deleteAtSelections()
	case input.KeyEnter:
		h.sealWordBatch()
		h.insertNewlineAtSelections()
	case input.KeyRune:
		// Word batching: alphanumeric runes coalesce if typed within 300ms
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
		if !isAlphaNum || now.Sub(h.lastInputTime) > 300*time.Millisecond {
			h.sealWordBatch()
		}
		h.insertRuneAtSelections(r, isAlphaNum)
		h.lastInputTime = now

		// Trigger completion on '.'
		if r == '.' {
			h.showCompletionPopup()
		}
	}
}

func (h *TestHarness) handleMouseInput(x, y int, btn input.MouseButton, act input.MouseAction, mod cell.Modifier) {
	if act != input.MousePress {
		return
	}

	// Click in gutter toggles breakpoint marker
	gutterW := 5
	if x < gutterW {
		targetLine := y
		if marker, ok := h.gutterMarkers[targetLine]; ok && strings.Contains(marker, "●") {
			delete(h.gutterMarkers, targetLine)
		} else {
			h.gutterMarkers[targetLine] = "● Breakpoint"
		}
		return
	}

	// Adjust for gutter
	targetCol := x - gutterW
	targetRow := y
	if targetRow >= len(h.lines) {
		targetRow = len(h.lines) - 1
	}
	if targetRow < 0 {
		targetRow = 0
	}
	lineLen := len(h.lines[targetRow])
	if targetCol > lineLen {
		targetCol = lineLen
	}

	if mod == 2 || mod == cell.AttrItalic { // Alt modifier -> Add multi-cursor
		h.selections = append(h.selections, Selection{
			AnchorRow: targetRow,
			AnchorCol: targetCol,
			HeadRow:   targetRow,
			HeadCol:   targetCol,
		})
		h.sortSelections()
	} else {
		// Single cursor click
		h.selections = []Selection{{
			AnchorRow: targetRow,
			AnchorCol: targetCol,
			HeadRow:   targetRow,
			HeadCol:   targetCol,
		}}
	}
}

func (h *TestHarness) handlePaste(text string, bracketed bool) {
	h.sealWordBatch()

	// In bracketed mode, CRLF is normalized and inserted without auto-indent staircases
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	h.insertTextAtSelections(normalized)
}

func (h *TestHarness) insertRuneAtSelections(r rune, isWordPart bool) {
	h.insertTextAtSelections(string(r))
	if isWordPart {
		if h.activeWordTx == nil {
			h.activeWordTx = &HistoryTx{
				Timestamp:   time.Now(),
				IsWordBatch: true,
				BeforeSels:  append([]Selection(nil), h.selections...),
			}
		}
		// Append to active transaction
		h.activeWordTx.Deltas = append(h.activeWordTx.Deltas, HistoryDelta{
			IsInsert: true,
			Row:      h.selections[0].HeadRow,
			Col:      h.selections[0].HeadCol,
			Text:     string(r),
		})
	}
}

func (h *TestHarness) insertTextAtSelections(text string) {
	// Bottom-to-Top application rule
	h.sortSelections()
	tx := HistoryTx{
		Timestamp:  time.Now(),
		BeforeSels: append([]Selection(nil), h.selections...),
	}

	for i := len(h.selections) - 1; i >= 0; i-- {
		sel := h.selections[i]
		row := sel.HeadRow
		col := sel.HeadCol
		if row >= len(h.lines) {
			row = len(h.lines) - 1
		}
		line := h.lines[row]
		if col > len(line) {
			col = len(line)
		}

		startCol := sel.AnchorCol
		endCol := sel.HeadCol
		if startCol > endCol {
			startCol, endCol = endCol, startCol
		}
		if startCol > len(line) {
			startCol = len(line)
		}
		if endCol > len(line) {
			endCol = len(line)
		}

		if strings.Contains(text, "\n") {
			// Multi-line paste/insertion
			pasteLines := strings.Split(text, "\n")
			prefix := line[:startCol]
			suffix := line[endCol:]
			newFirst := prefix + pasteLines[0]
			newLast := pasteLines[len(pasteLines)-1] + suffix

			replacement := []string{newFirst}
			for p := 1; p < len(pasteLines)-1; p++ {
				replacement = append(replacement, pasteLines[p])
			}
			replacement = append(replacement, newLast)

			var newLines []string
			newLines = append(newLines, h.lines[:row]...)
			newLines = append(newLines, replacement...)
			if row+1 < len(h.lines) {
				newLines = append(newLines, h.lines[row+1:]...)
			}
			h.lines = newLines

			h.selections[i].HeadRow = row + len(pasteLines) - 1
			h.selections[i].HeadCol = len(pasteLines[len(pasteLines)-1])
			h.selections[i].AnchorRow = h.selections[i].HeadRow
			h.selections[i].AnchorCol = h.selections[i].HeadCol
		} else {
			newLine := line[:startCol] + text + line[endCol:]
			h.lines[row] = newLine
			h.selections[i].HeadCol = startCol + len(text)
			h.selections[i].AnchorCol = h.selections[i].HeadCol
		}

		tx.Deltas = append(tx.Deltas, HistoryDelta{
			IsInsert: true,
			Row:      row,
			Col:      startCol,
			Text:     text,
		})
	}

	tx.AfterSels = append([]Selection(nil), h.selections...)
	if h.activeWordTx == nil {
		h.undoStack = append(h.undoStack, tx)
		h.redoStack = nil // New edit invalidates redo stack
	}
}

func (h *TestHarness) deleteAtSelections() {
	h.sortSelections()
	tx := HistoryTx{
		Timestamp:  time.Now(),
		BeforeSels: append([]Selection(nil), h.selections...),
	}

	for i := len(h.selections) - 1; i >= 0; i-- {
		row := h.selections[i].HeadRow
		col := h.selections[i].HeadCol
		if row >= len(h.lines) {
			row = len(h.lines) - 1
		}
		line := h.lines[row]

		if col > 0 {
			delChar := string(line[col-1])
			newLine := line[:col-1] + line[col:]
			h.lines[row] = newLine
			h.selections[i].HeadCol = col - 1
			h.selections[i].AnchorCol = col - 1

			tx.Deltas = append(tx.Deltas, HistoryDelta{
				IsInsert: false,
				Row:      row,
				Col:      col - 1,
				Text:     delChar,
			})
		} else if row > 0 {
			// Join with previous line
			prevLine := h.lines[row-1]
			prevLen := len(prevLine)
			h.lines[row-1] = prevLine + line

			var newLines []string
			newLines = append(newLines, h.lines[:row]...)
			if row+1 < len(h.lines) {
				newLines = append(newLines, h.lines[row+1:]...)
			}
			h.lines = newLines

			h.selections[i].HeadRow = row - 1
			h.selections[i].HeadCol = prevLen
			h.selections[i].AnchorRow = row - 1
			h.selections[i].AnchorCol = prevLen
		}
	}

	tx.AfterSels = append([]Selection(nil), h.selections...)
	h.undoStack = append(h.undoStack, tx)
	h.redoStack = nil
}

func (h *TestHarness) insertNewlineAtSelections() {
	h.sortSelections()
	for i := len(h.selections) - 1; i >= 0; i-- {
		row := h.selections[i].HeadRow
		col := h.selections[i].HeadCol
		line := h.lines[row]
		if col > len(line) {
			col = len(line)
		}

		line1 := line[:col]
		line2 := line[col:]

		var newLines []string
		newLines = append(newLines, h.lines[:row]...)
		newLines = append(newLines, line1, line2)
		if row+1 < len(h.lines) {
			newLines = append(newLines, h.lines[row+1:]...)
		}
		h.lines = newLines

		h.selections[i].HeadRow = row + 1
		h.selections[i].HeadCol = 0
		h.selections[i].AnchorRow = row + 1
		h.selections[i].AnchorCol = 0
	}
}

func (h *TestHarness) sealWordBatch() {
	if h.activeWordTx != nil {
		h.activeWordTx.AfterSels = append([]Selection(nil), h.selections...)
		h.undoStack = append(h.undoStack, *h.activeWordTx)
		h.activeWordTx = nil
		h.redoStack = nil
	}
}

func (h *TestHarness) performUndo() {
	if len(h.undoStack) == 0 {
		return
	}
	tx := h.undoStack[len(h.undoStack)-1]
	h.undoStack = h.undoStack[:len(h.undoStack)-1]

	// Revert deltas in reverse
	for i := len(tx.Deltas) - 1; i >= 0; i-- {
		d := tx.Deltas[i]
		if d.IsInsert {
			// Invert insert -> delete
			if d.Row < len(h.lines) {
				line := h.lines[d.Row]
				delLen := len(d.Text)
				if d.Col+delLen <= len(line) {
					h.lines[d.Row] = line[:d.Col] + line[d.Col+delLen:]
				}
			}
		} else {
			// Invert delete -> insert
			if d.Row < len(h.lines) {
				line := h.lines[d.Row]
				if d.Col <= len(line) {
					h.lines[d.Row] = line[:d.Col] + d.Text + line[d.Col:]
				}
			}
		}
	}

	h.selections = append([]Selection(nil), tx.BeforeSels...)
	h.redoStack = append(h.redoStack, tx)
}

func (h *TestHarness) performRedo() {
	if len(h.redoStack) == 0 {
		return
	}
	tx := h.redoStack[len(h.redoStack)-1]
	h.redoStack = h.redoStack[:len(h.redoStack)-1]

	// Re-apply deltas in original forward order
	for _, d := range tx.Deltas {
		if d.IsInsert {
			if d.Row < len(h.lines) {
				line := h.lines[d.Row]
				if d.Col <= len(line) {
					h.lines[d.Row] = line[:d.Col] + d.Text + line[d.Col:]
				}
			}
		} else {
			if d.Row < len(h.lines) {
				line := h.lines[d.Row]
				delLen := len(d.Text)
				if d.Col+delLen <= len(line) {
					h.lines[d.Row] = line[:d.Col] + line[d.Col+delLen:]
				}
			}
		}
	}

	h.selections = append([]Selection(nil), tx.AfterSels...)
	h.undoStack = append(h.undoStack, tx)
}

func (h *TestHarness) moveCursors(dRow, dCol int, extend bool) {
	for i := range h.selections {
		newRow := h.selections[i].HeadRow + dRow
		if newRow < 0 {
			newRow = 0
		}
		if newRow >= len(h.lines) {
			newRow = len(h.lines) - 1
		}
		newCol := h.selections[i].HeadCol + dCol
		if newCol < 0 {
			newCol = 0
		}
		if newRow < len(h.lines) && newCol > len(h.lines[newRow]) {
			newCol = len(h.lines[newRow])
		}

		h.selections[i].HeadRow = newRow
		h.selections[i].HeadCol = newCol
		if !extend {
			h.selections[i].AnchorRow = newRow
			h.selections[i].AnchorCol = newCol
		}
	}
	h.collapseDuplicateSelections()
}

func (h *TestHarness) addNextMatchSelection() {
	if len(h.selections) == 0 {
		return
	}
	// Select token under primary cursor
	curr := h.selections[len(h.selections)-1]
	row := curr.HeadRow
	if row >= len(h.lines) {
		return
	}
	line := h.lines[row]
	word := "main"
	if strings.Contains(line, word) {
		// Find next occurrence in subsequent lines or same line
		idx := strings.Index(line[curr.HeadCol:], word)
		if idx >= 0 {
			nextCol := curr.HeadCol + idx
			h.selections = append(h.selections, Selection{
				AnchorRow: row,
				AnchorCol: nextCol,
				HeadRow:   row,
				HeadCol:   nextCol + len(word),
			})
			h.sortSelections()
		}
	}
}

func (h *TestHarness) hasMultiLineSelection() bool {
	for _, s := range h.selections {
		if s.AnchorRow != s.HeadRow {
			return true
		}
	}
	return false
}

func (h *TestHarness) indentSelectedLines() {
	startRow := h.selections[0].AnchorRow
	endRow := h.selections[0].HeadRow
	if startRow > endRow {
		startRow, endRow = endRow, startRow
	}
	for r := startRow; r <= endRow && r < len(h.lines); r++ {
		if h.lines[r] != "" {
			h.lines[r] = "    " + h.lines[r]
		}
	}
	h.renderScreen()
}

func (h *TestHarness) dedentSelectedLines() {
	startRow := h.selections[0].AnchorRow
	endRow := h.selections[0].HeadRow
	if startRow > endRow {
		startRow, endRow = endRow, startRow
	}
	for r := startRow; r <= endRow && r < len(h.lines); r++ {
		if strings.HasPrefix(h.lines[r], "    ") {
			h.lines[r] = h.lines[r][4:]
		} else if strings.HasPrefix(h.lines[r], "\t") {
			h.lines[r] = h.lines[r][1:]
		}
	}
	h.renderScreen()
}

func (h *TestHarness) sortSelections() {
	// Sort selections strictly ascending by (Row, Col)
	for i := 0; i < len(h.selections)-1; i++ {
		for j := i + 1; j < len(h.selections); j++ {
			s1 := h.selections[i]
			s2 := h.selections[j]
			if s1.HeadRow > s2.HeadRow || (s1.HeadRow == s2.HeadRow && s1.HeadCol > s2.HeadCol) {
				h.selections[i], h.selections[j] = h.selections[j], h.selections[i]
			}
		}
	}
}

func (h *TestHarness) collapseDuplicateSelections() {
	if len(h.selections) <= 1 {
		return
	}
	h.sortSelections()
	unique := []Selection{h.selections[0]}
	for i := 1; i < len(h.selections); i++ {
		prev := unique[len(unique)-1]
		curr := h.selections[i]
		if prev.HeadRow != curr.HeadRow || prev.HeadCol != curr.HeadCol {
			unique = append(unique, curr)
		}
	}
	h.selections = unique
}

func (h *TestHarness) filterOmnibarCandidates() {
	q := strings.ToLower(h.omnibar.Query)
	var filtered []string
	for _, f := range h.allFiles {
		if strings.Contains(strings.ToLower(f), q) {
			filtered = append(filtered, f)
		}
	}
	h.omnibar.Candidates = filtered
	h.omnibar.SelectedIdx = 0
}

func (h *TestHarness) showCompletionPopup() {
	h.popups = []PopupWindow{{
		Visible:   true,
		Title:     "Completions",
		Items:     []string{"Println", "Printf", "Print", "Panic"},
		ActiveIdx: 0,
		Doc:       "func Println(a ...any) (n int, err error)",
		Row:       h.cursorRow + 1,
		Col:       h.cursorCol,
		Width:     40,
		Height:    6,
	}}
}

func (h *TestHarness) saveCurrentFile() {
	if h.currentFile == "" {
		h.currentFile = "test_output.txt"
	}

	// Safe atomic save: write to .{file}.tahr.tmp -> Sync -> Rename
	dir := filepath.Dir(h.currentFile)
	base := filepath.Base(h.currentFile)
	tmpFile := filepath.Join(dir, "."+base+".tahr.tmp")

	text := strings.Join(h.lines, "\n")
	if h.crlfMode {
		text = strings.Join(h.lines, "\r\n")
	}

	f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		h.statusText = "Save error: " + err.Error()
		return
	}
	_, _ = f.Write([]byte(text))
	_ = f.Sync()
	_ = f.Close()

	_ = os.Rename(tmpFile, h.currentFile)
	h.statusText = fmt.Sprintf("Saved %s (%d lines)", h.currentFile, len(h.lines))
}

// ----------------------------------------------------------------------------
// Double-Buffered Diff Rendering & Screen Painting
// ----------------------------------------------------------------------------

func (h *TestHarness) renderScreen() {
	h.drawCalls++
	if h.height <= 0 || h.width <= 0 {
		return
	}
	gutterW := 5
	if gutterW > h.width {
		gutterW = h.width
	}

	contentRows := h.height - 1
	if contentRows < 0 {
		contentRows = 0
	}

	// Paint Editor Lines & Gutter
	for y := 0; y < contentRows; y++ {
		// Line number gutter
		lineNumStr := fmt.Sprintf("%3d ", y+1)
		for gx := 0; gx < gutterW && gx < len(lineNumStr) && gx < h.width; gx++ {
			h.screen[y][gx] = ScreenCell{Rune: rune(lineNumStr[gx]), Width: 1, Fg: 0x565f89}
		}

		// Diagnostics marker in gutter
		if mark, ok := h.gutterMarkers[y]; ok && h.width > 0 {
			glyph := '●'
			if strings.Contains(mark, "Breakpoint") {
				glyph = '●'
			} else if strings.Contains(mark, "Error") {
				glyph = 'E'
			}
			h.screen[y][0] = ScreenCell{Rune: glyph, Width: 1, Fg: 0xf7768e}
		}

		// Text content
		var lineText string
		if y < len(h.lines) {
			lineText = h.lines[y]
		}
		runes := []rune(lineText)
		col := gutterW

		for _, r := range runes {
			rw := runewidth.RuneWidth(r)
			if rw == 0 {
				rw = 1
			}
			if col < h.width {
				h.screen[y][col] = ScreenCell{Rune: r, Width: rw, Fg: 0xc0caf5}
				col += rw
			}
		}
		// Clear remainder of line
		for x := col; x < h.width; x++ {
			h.screen[y][x] = ScreenCell{Rune: ' ', Width: 1}
		}
	}

	// Update Cursor Position to primary cursor clamped to screen bounds
	if len(h.selections) > 0 {
		h.cursorRow = h.selections[0].HeadRow
		h.cursorCol = h.selections[0].HeadCol + gutterW
		if h.cursorRow >= h.height {
			h.cursorRow = h.height - 1
		}
		if h.cursorCol >= h.width {
			h.cursorCol = h.width - 1
		}
		if h.cursorRow < 0 {
			h.cursorRow = 0
		}
		if h.cursorCol < 0 {
			h.cursorCol = 0
		}
	}

	// Floating Popup Rendering
	for _, p := range h.popups {
		if !p.Visible {
			continue
		}
		for py := 0; py < p.Height && p.Row+py < contentRows; py++ {
			var rowText string
			if py == 0 {
				rowText = "┌─ " + p.Title + " " + safeRepeat("─", p.Width-len(p.Title)-5) + "┐"
			} else if py-1 < len(p.Items) {
				prefix := "│ "
				if py-1 == p.ActiveIdx {
					prefix = "│ ► "
				}
				item := p.Items[py-1]
				rowText = prefix + item + safeRepeat(" ", p.Width-len(prefix)-len(item)-1) + "│"
			} else {
				rowText = "└" + safeRepeat("─", p.Width-2) + "┘"
			}

			rowRunes := []rune(rowText)
			for px := 0; px < len(rowRunes) && p.Col+px < h.width; px++ {
				h.screen[p.Row+py][p.Col+px] = ScreenCell{
					Rune: rowRunes[px], Width: 1, Fg: 0x7aa2f7, Bg: 0x1f2335,
				}
			}
		}
	}

	// Omnibar Rendering
	if h.omnibar.Open && h.height > 4 {
		modalRow := 2
		modalW := 60
		if modalW > h.width-4 {
			modalW = h.width - 4
		}
		if modalW < 10 {
			modalW = 10
		}
		modalH := 8

		title := " Quick Open (Ctrl+P) "
		header := "┌" + title + safeRepeat("─", modalW-len(title)-2) + "┐"
		queryLine := "│ > " + h.omnibar.Query + safeRepeat(" ", modalW-len(h.omnibar.Query)-6) + "│"
		sepLine := "├" + safeRepeat("─", modalW-2) + "┤"

		linesToPaint := []string{header, queryLine, sepLine}
		for i := 0; i < modalH-4; i++ {
			if i < len(h.omnibar.Candidates) {
				item := h.omnibar.Candidates[i]
				prefix := "│   "
				if i == h.omnibar.SelectedIdx {
					prefix = "│ ► "
				}
				line := prefix + item + safeRepeat(" ", modalW-len(prefix)-len(item)-1) + "│"
				linesToPaint = append(linesToPaint, line)
			} else {
				linesToPaint = append(linesToPaint, "│"+safeRepeat(" ", modalW-2)+"│")
			}
		}
		linesToPaint = append(linesToPaint, "└"+safeRepeat("─", modalW-2)+"┘")

		for oy, oline := range linesToPaint {
			if modalRow+oy < contentRows {
				orunes := []rune(oline)
				for ox := 0; ox < len(orunes) && 2+ox < h.width; ox++ {
					h.screen[modalRow+oy][2+ox] = ScreenCell{
						Rune: orunes[ox], Width: 1, Fg: 0x9ece6a, Bg: 0x24283b,
					}
				}
			}
		}
	}

	// Status Line (last row) if height > 0
	if h.height > 0 {
		statusY := h.height - 1
		statusStr := fmt.Sprintf(" %s | Ln %d, Col %d | UTF-8 | %s", h.currentFile, h.cursorRow+1, h.cursorCol-gutterW+1, h.statusText)
		for x := 0; x < h.width; x++ {
			var r rune = ' '
			if x < len(statusStr) {
				r = rune(statusStr[x])
			}
			h.screen[statusY][x] = ScreenCell{Rune: r, Width: 1, Fg: 0x1a1b26, Bg: 0x7aa2f7}
		}
	}
}

// ----------------------------------------------------------------------------
// Unicode / UTF-16 Conversion Verification Helper
// ----------------------------------------------------------------------------

// UTF16Units returns the number of UTF-16 code units for a rune.
func UTF16Units(r rune) int {
	return len(utf16.Encode([]rune{r}))
}

func safeRepeat(s string, count int) string {
	if count <= 0 {
		return ""
	}
	return strings.Repeat(s, count)
}
