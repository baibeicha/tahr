package tui

import (
	"fmt"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/ui"
)

// FindReplaceAction represents an action triggered from the modal.
type FindReplaceAction string

const (
	FRActionNone         FindReplaceAction = ""
	FRActionNext         FindReplaceAction = "next"
	FRActionPrev         FindReplaceAction = "prev"
	FRActionReplace      FindReplaceAction = "replace"
	FRActionReplaceAll   FindReplaceAction = "replace_all"
	FRActionQueryChanged FindReplaceAction = "query_changed"
	FRActionClose        FindReplaceAction = "close"
	FRActionUndo         FindReplaceAction = "undo"
	FRActionRedo         FindReplaceAction = "redo"
)

// FindReplaceModal manages an interactive floating Find and Replace toolbar.
type FindReplaceModal struct {
	Open         bool
	ReplaceMode  bool
	FindQuery    string
	ReplaceQuery string
	FindCursor   int
	ReplCursor   int
	ActiveField  int // 0 = Find, 1 = Replace
	MatchCase    bool
	WholeWord    bool
	CurrentMatch int
	TotalMatches int
}

// NewFindReplaceModal creates a new Find & Replace modal state.
func NewFindReplaceModal() *FindReplaceModal {
	return &FindReplaceModal{
		Open:        false,
		ReplaceMode: false,
		ActiveField: 0,
	}
}

// OpenFind activates the modal in Find-only mode.
func (m *FindReplaceModal) OpenFind(initialText string) {
	m.Open = true
	m.ReplaceMode = false
	m.ActiveField = 0
	if initialText != "" {
		m.FindQuery = initialText
	}
	m.FindCursor = len([]rune(m.FindQuery))
	m.ReplCursor = len([]rune(m.ReplaceQuery))
}

// OpenReplace activates the modal in Find & Replace mode.
func (m *FindReplaceModal) OpenReplace(initialText string) {
	m.Open = true
	m.ReplaceMode = true
	m.ActiveField = 0
	if initialText != "" {
		m.FindQuery = initialText
	}
	m.FindCursor = len([]rune(m.FindQuery))
	m.ReplCursor = len([]rune(m.ReplaceQuery))
}

// Close dismisses the modal.
func (m *FindReplaceModal) Close() {
	m.Open = false
}

// UpdateMatches updates match indices for display.
func (m *FindReplaceModal) UpdateMatches(current, total int) {
	m.CurrentMatch = current
	m.TotalMatches = total
}

// HandleKey processes keyboard input inside the Find & Replace dialog.
func (m *FindReplaceModal) HandleKey(k input.Key) (bool, FindReplaceAction) {
	if !m.Open {
		return false, FRActionNone
	}

	// Esc always closes
	if k.Type == input.KeyEsc {
		m.Close()
		return true, FRActionClose
	}

	// Undo / Redo pass-through to editor document
	isUndo := (!k.HasShift() && (k.Rune == 26 || (k.HasCtrl() && (matchKey(k, 'z', 'я') || matchKey(k, 'u', 'г') || k.Rune == 21)))) ||
		(k.HasAlt() && !k.HasShift() && (k.Type == input.KeyBackspace || k.Rune == 8 || k.Rune == 127))
	if isUndo {
		return true, FRActionUndo
	}

	isRedo := MatchKeyToBinding(k, "Ctrl+Shift+Z") || (k.Rune == 26 && k.HasShift()) || k.Rune == 25 ||
		(k.HasCtrl() && matchKey(k, 'y', 'н')) ||
		(k.HasAlt() && k.HasShift() && (k.Type == input.KeyBackspace || k.Rune == 8 || k.Rune == 127))
	if isRedo {
		return true, FRActionRedo
	}

	// Toggle Replace mode via Ctrl+H, Ctrl+R, or Ctrl+F
	if k.HasCtrl() && (matchKey(k, 'h', 'р') || matchKey(k, 'r', 'к') || k.Rune == 8 || k.Rune == 18) {
		m.ReplaceMode = !m.ReplaceMode
		if m.ReplaceMode {
			m.ActiveField = 1
		} else {
			m.ActiveField = 0
		}
		return true, FRActionNone
	}
	if k.HasCtrl() && (matchKey(k, 'f', 'а') || k.Rune == 6) {
		if m.ReplaceMode {
			m.ReplaceMode = false
			m.ActiveField = 0
			return true, FRActionNone
		}
	}

	// Hotkeys for toggles
	if k.HasAlt() {
		switch k.Rune {
		case 'c', 'C', 'с', 'С':
			m.MatchCase = !m.MatchCase
			return true, FRActionQueryChanged
		case 'w', 'W', 'ц', 'Ц':
			m.WholeWord = !m.WholeWord
			return true, FRActionQueryChanged
		case 'p', 'P', 'з', 'З':
			return true, FRActionPrev
		case 'n', 'N', 'т', 'Т':
			return true, FRActionNext
		case 'r', 'R', 'к', 'К':
			if m.ReplaceMode {
				return true, FRActionReplace
			}
		case 'a', 'A', 'ф', 'Ф':
			if m.ReplaceMode {
				return true, FRActionReplaceAll
			}
		}
		if k.Type == input.KeyEnter && m.ReplaceMode {
			return true, FRActionReplaceAll
		}
	}

	// Tab navigates between Find and Replace fields
	if k.Type == input.KeyTab || k.Type == input.KeyBacktab {
		if m.ReplaceMode {
			m.ActiveField = 1 - m.ActiveField
			return true, FRActionNone
		}
		return true, FRActionNone
	}

	// Shift+Enter navigates to previous match
	if k.Type == input.KeyEnter && k.HasShift() {
		return true, FRActionPrev
	}

	// Enter key action depends on active field
	if k.Type == input.KeyEnter {
		if m.ActiveField == 1 {
			return true, FRActionReplace
		}
		return true, FRActionNext
	}

	// Cursor navigation & text editing in active input field
	if m.ActiveField == 0 {
		runes := []rune(m.FindQuery)
		if m.FindCursor > len(runes) {
			m.FindCursor = len(runes)
		}
		if m.FindCursor < 0 {
			m.FindCursor = 0
		}

		switch k.Type {
		case input.KeyLeft:
			if m.FindCursor > 0 {
				m.FindCursor--
			}
			return true, FRActionNone
		case input.KeyRight:
			if m.FindCursor < len(runes) {
				m.FindCursor++
			}
			return true, FRActionNone
		case input.KeyHome:
			m.FindCursor = 0
			return true, FRActionNone
		case input.KeyEnd:
			m.FindCursor = len(runes)
			return true, FRActionNone
		case input.KeyDelete:
			if m.FindCursor < len(runes) {
				m.FindQuery = string(runes[:m.FindCursor]) + string(runes[m.FindCursor+1:])
				return true, FRActionQueryChanged
			}
			return true, FRActionNone
		case input.KeyBackspace:
			if m.FindCursor > 0 && len(runes) > 0 {
				m.FindQuery = string(runes[:m.FindCursor-1]) + string(runes[m.FindCursor:])
				m.FindCursor--
				return true, FRActionQueryChanged
			}
			return true, FRActionNone
		case input.KeySpace:
			m.FindQuery = string(runes[:m.FindCursor]) + " " + string(runes[m.FindCursor:])
			m.FindCursor++
			return true, FRActionQueryChanged
		}

		if k.HasCtrl() && (matchKey(k, 'k', 'л') || k.Rune == 11) {
			m.FindQuery = ""
			m.FindCursor = 0
			return true, FRActionQueryChanged
		}

		if k.Rune >= 32 && !k.HasCtrl() && !k.HasAlt() {
			m.FindQuery = string(runes[:m.FindCursor]) + string(k.Rune) + string(runes[m.FindCursor:])
			m.FindCursor++
			return true, FRActionQueryChanged
		}
	} else {
		runes := []rune(m.ReplaceQuery)
		if m.ReplCursor > len(runes) {
			m.ReplCursor = len(runes)
		}
		if m.ReplCursor < 0 {
			m.ReplCursor = 0
		}

		switch k.Type {
		case input.KeyLeft:
			if m.ReplCursor > 0 {
				m.ReplCursor--
			}
			return true, FRActionNone
		case input.KeyRight:
			if m.ReplCursor < len(runes) {
				m.ReplCursor++
			}
			return true, FRActionNone
		case input.KeyHome:
			m.ReplCursor = 0
			return true, FRActionNone
		case input.KeyEnd:
			m.ReplCursor = len(runes)
			return true, FRActionNone
		case input.KeyDelete:
			if m.ReplCursor < len(runes) {
				m.ReplaceQuery = string(runes[:m.ReplCursor]) + string(runes[m.ReplCursor+1:])
				return true, FRActionNone
			}
			return true, FRActionNone
		case input.KeyBackspace:
			if m.ReplCursor > 0 && len(runes) > 0 {
				m.ReplaceQuery = string(runes[:m.ReplCursor-1]) + string(runes[m.ReplCursor:])
				m.ReplCursor--
				return true, FRActionNone
			}
			return true, FRActionNone
		case input.KeySpace:
			m.ReplaceQuery = string(runes[:m.ReplCursor]) + " " + string(runes[m.ReplCursor:])
			m.ReplCursor++
			return true, FRActionNone
		}

		if k.HasCtrl() && (matchKey(k, 'k', 'л') || k.Rune == 11) {
			m.ReplaceQuery = ""
			m.ReplCursor = 0
			return true, FRActionNone
		}

		if k.Rune >= 32 && !k.HasCtrl() && !k.HasAlt() {
			m.ReplaceQuery = string(runes[:m.ReplCursor]) + string(k.Rune) + string(runes[m.ReplCursor:])
			m.ReplCursor++
			return true, FRActionNone
		}
	}

	return true, FRActionNone
}

// HandleClick processes mouse interactions on modal elements.
func (m *FindReplaceModal) HandleClick(mouseX, mouseY, screenW, screenH int) (bool, FindReplaceAction) {
	if !m.Open {
		return false, FRActionNone
	}

	modalW := 68
	if modalW > screenW-4 {
		modalW = screenW - 4
	}
	modalH := 5
	if m.ReplaceMode {
		modalH = 7
	}
	startX := screenW - modalW - 3
	if startX < 2 {
		startX = 2
	}
	startY := 2

	// Click outside modal dismisses
	if mouseX < startX || mouseX >= startX+modalW || mouseY < startY || mouseY >= startY+modalH {
		m.Close()
		return true, FRActionClose
	}

	// Close button ✕ at top-right
	if mouseY == startY && mouseX >= startX+modalW-3 && mouseX < startX+modalW {
		m.Close()
		return true, FRActionClose
	}

	findFieldX := startX + 8
	findFieldW := modalW - 37
	if findFieldW < 10 {
		findFieldW = 10
	}
	findFieldEnd := findFieldX + findFieldW

	// Row 1: Find Input + Aa + \b + Prev + Next + ToggleRepl
	if mouseY == startY+1 {
		// Click inside Find input: position cursor
		if mouseX >= findFieldX && mouseX < findFieldEnd {
			m.ActiveField = 0
			clickOff := mouseX - findFieldX
			runes := []rune(m.FindQuery)
			if clickOff > len(runes) {
				clickOff = len(runes)
			}
			m.FindCursor = clickOff
			return true, FRActionNone
		}
		// [Aa] Case button (findFieldEnd+1 .. findFieldEnd+4)
		if mouseX >= findFieldEnd+1 && mouseX <= findFieldEnd+4 {
			m.MatchCase = !m.MatchCase
			return true, FRActionQueryChanged
		}
		// [\b] Word button (findFieldEnd+5 .. findFieldEnd+8)
		if mouseX >= findFieldEnd+5 && mouseX <= findFieldEnd+8 {
			m.WholeWord = !m.WholeWord
			return true, FRActionQueryChanged
		}
		// [◀] Prev (findFieldEnd+9 .. findFieldEnd+11)
		if mouseX >= findFieldEnd+9 && mouseX <= findFieldEnd+11 {
			return true, FRActionPrev
		}
		// [▶] Next (findFieldEnd+12 .. findFieldEnd+14)
		if mouseX >= findFieldEnd+12 && mouseX <= findFieldEnd+14 {
			return true, FRActionNext
		}
		// [⇄] Toggle Replace mode (findFieldEnd+15 .. findFieldEnd+17)
		if mouseX >= findFieldEnd+15 && mouseX <= findFieldEnd+17 {
			m.ReplaceMode = !m.ReplaceMode
			if m.ReplaceMode {
				m.ActiveField = 1
			} else {
				m.ActiveField = 0
			}
			return true, FRActionNone
		}
	}

	// Row 2: Replace Input + [Replace] + [Replace All] (in ReplaceMode)
	if m.ReplaceMode && mouseY == startY+3 {
		replFieldEnd := findFieldEnd
		// Click inside Replace input: position cursor
		if mouseX >= findFieldX && mouseX < replFieldEnd {
			m.ActiveField = 1
			clickOff := mouseX - findFieldX
			runes := []rune(m.ReplaceQuery)
			if clickOff > len(runes) {
				clickOff = len(runes)
			}
			m.ReplCursor = clickOff
			return true, FRActionNone
		}
		// [Replace] (findFieldEnd+1 .. findFieldEnd+9)
		if mouseX >= findFieldEnd+1 && mouseX <= findFieldEnd+9 {
			return true, FRActionReplace
		}
		// [Replace All] (findFieldEnd+11 .. findFieldEnd+23)
		if mouseX >= findFieldEnd+11 && mouseX <= findFieldEnd+23 {
			return true, FRActionReplaceAll
		}
	}

	return true, FRActionNone
}

// Render draws the floating Find & Replace panel into the buffer.
func (m *FindReplaceModal) Render(buf *buffer.Buffer, screenW, screenH int, theme *ui.Theme) {
	if !m.Open {
		return
	}

	modalW := 68
	if modalW > screenW-4 {
		modalW = screenW - 4
	}
	modalH := 5
	if m.ReplaceMode {
		modalH = 7
	}
	startX := screenW - modalW - 3
	if startX < 2 {
		startX = 2
	}
	startY := 2

	bg := toColor(theme.PopupBg)
	fg := toColor(theme.Foreground)
	borderFg := toColor(theme.BorderColor)
	activeBg := toColor(theme.PopupSelBg)
	accentFg := toColor(theme.Keyword)

	// Draw frame
	for y := startY; y < startY+modalH; y++ {
		for x := startX; x < startX+modalW; x++ {
			r := ' '
			f := borderFg
			b := bg
			if y == startY && x == startX {
				r = '┌'
			} else if y == startY && x == startX+modalW-1 {
				r = '┐'
			} else if y == startY+modalH-1 && x == startX {
				r = '└'
			} else if y == startY+modalH-1 && x == startX+modalW-1 {
				r = '┘'
			} else if y == startY || y == startY+modalH-1 {
				r = '─'
			} else if x == startX || x == startX+modalW-1 {
				r = '│'
			}
			buf.SetRune(x, y, r, f, b, cell.AttrNone)
		}
	}

	// Header Title & Close button
	title := " FIND "
	if m.ReplaceMode {
		title = " FIND & REPLACE "
	}
	for i, r := range title {
		if startX+2+i < startX+modalW-4 {
			buf.SetRune(startX+2+i, startY, r, accentFg, bg, cell.AttrBold)
		}
	}
	buf.SetRune(startX+modalW-2, startY, '✕', toColor(theme.DiagnosticError), bg, cell.AttrBold)

	// Row 1: Find: [query_______]  Aa  \b  ◀  ▶  ⇄  2/14
	lblFind := "Find: "
	for i, r := range lblFind {
		buf.SetRune(startX+2+i, startY+1, r, fg, bg, cell.AttrBold)
	}

	findFieldX := startX + 8
	findFieldW := modalW - 37
	if findFieldW < 10 {
		findFieldW = 10
	}
	findFieldEnd := findFieldX + findFieldW

	fBg := bg
	if m.ActiveField == 0 {
		fBg = activeBg
	}
	findRunes := []rune(m.FindQuery)
	for i := 0; i < findFieldW; i++ {
		r := ' '
		attr := cell.AttrNone
		fgColor := fg
		bgColor := fBg
		if i < len(findRunes) {
			r = findRunes[i]
		}
		if m.ActiveField == 0 && i == m.FindCursor {
			attr |= cell.AttrReverse
		}
		buf.SetRune(findFieldX+i, startY+1, r, fgColor, bgColor, attr)
	}

	// [Aa] Case toggle (width 4: findFieldEnd+1 .. findFieldEnd+4)
	caseFg := toColor(theme.Comment)
	caseBg := bg
	if m.MatchCase {
		caseFg = toColor(theme.Keyword)
		caseBg = activeBg
	}
	caseStr := " Aa "
	for i, r := range caseStr {
		buf.SetRune(findFieldEnd+1+i, startY+1, r, caseFg, caseBg, cell.AttrBold)
	}

	// [\b] Word toggle (width 4: findFieldEnd+5 .. findFieldEnd+8)
	wordFg := toColor(theme.Comment)
	wordBg := bg
	if m.WholeWord {
		wordFg = toColor(theme.Keyword)
		wordBg = activeBg
	}
	wordStr := " \\b "
	for i, r := range wordStr {
		buf.SetRune(findFieldEnd+5+i, startY+1, r, wordFg, wordBg, cell.AttrBold)
	}

	// [◀] Prev and [▶] Next buttons (Clean text glyphs on panel background, NO dark block!)
	prevStr := " ◀ "
	for i, r := range prevStr {
		buf.SetRune(findFieldEnd+9+i, startY+1, r, toColor(theme.Function), bg, cell.AttrBold)
	}
	nextStr := " ▶ "
	for i, r := range nextStr {
		buf.SetRune(findFieldEnd+12+i, startY+1, r, toColor(theme.Function), bg, cell.AttrBold)
	}

	// [⇄] Toggle Replace Mode button (width 3: findFieldEnd+15 .. findFieldEnd+17)
	toggleFg := toColor(theme.Comment)
	toggleBg := bg
	if m.ReplaceMode {
		toggleFg = toColor(theme.Keyword)
		toggleBg = activeBg
	}
	toggleStr := " ⇄ "
	for i, r := range toggleStr {
		buf.SetRune(findFieldEnd+15+i, startY+1, r, toggleFg, toggleBg, cell.AttrBold)
	}

	// Match Count: 2/14 (no square brackets, clean text)
	matchInfo := ""
	if m.FindQuery != "" {
		if m.TotalMatches > 0 {
			matchInfo = fmt.Sprintf(" %d/%d ", m.CurrentMatch, m.TotalMatches)
		} else {
			matchInfo = " 0/0 "
		}
	}
	for i, r := range matchInfo {
		if findFieldEnd+18+i < startX+modalW-1 {
			buf.SetRune(findFieldEnd+18+i, startY+1, r, toColor(theme.Constant), bg, cell.AttrNone)
		}
	}

	// Row 2 / 3: Replace Mode Elements
	if m.ReplaceMode {
		lblRepl := "Repl: "
		for i, r := range lblRepl {
			buf.SetRune(startX+2+i, startY+3, r, fg, bg, cell.AttrBold)
		}

		rBg := bg
		if m.ActiveField == 1 {
			rBg = activeBg
		}
		replRunes := []rune(m.ReplaceQuery)
		for i := 0; i < findFieldW; i++ {
			r := ' '
			attr := cell.AttrNone
			fgColor := fg
			bgColor := rBg
			if i < len(replRunes) {
				r = replRunes[i]
			}
			if m.ActiveField == 1 && i == m.ReplCursor {
				attr |= cell.AttrReverse
			}
			buf.SetRune(findFieldX+i, startY+3, r, fgColor, bgColor, attr)
		}

		// Replace and Replace All buttons (clean text without harsh dark background or square brackets)
		replBtn := " Replace "
		for i, r := range replBtn {
			buf.SetRune(findFieldEnd+1+i, startY+3, r, toColor(theme.String), bg, cell.AttrBold)
		}
		allBtn := " Replace All "
		for i, r := range allBtn {
			buf.SetRune(findFieldEnd+11+i, startY+3, r, toColor(theme.DiagnosticWarn), bg, cell.AttrBold)
		}

		// Hotkey Hint
		hint := "Enter: Replace │ Alt+Enter: All │ Tab: Switch │ ⇄: Find │ Alt+Bksp: Undo"
		for i, r := range hint {
			if startX+2+i < startX+modalW-2 {
				buf.SetRune(startX+2+i, startY+5, r, toColor(theme.Comment), bg, cell.AttrNone)
			}
		}
	} else {
		// Find Hint
		hint := "Enter: Next │ Shift+Enter: Prev │ Alt+W: Word │ ⇄: Repl │ Alt+Bksp: Undo"
		for i, r := range hint {
			if startX+2+i < startX+modalW-2 {
				buf.SetRune(startX+2+i, startY+3, r, toColor(theme.Comment), bg, cell.AttrNone)
			}
		}
	}
}
