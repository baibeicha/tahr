package tui

import (
	"fmt"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/ui"
)

// RenameModal manages the interactive symbol rename dialog.
type RenameModal struct {
	Open    bool
	OldName string
	NewName string
	Line    int
	Col     int
	URI     string
}

// NewRenameModal creates a new RenameModal instance.
func NewRenameModal() *RenameModal {
	return &RenameModal{
		Open: false,
	}
}

// OpenModal activates the rename dialog with target symbol information.
func (m *RenameModal) OpenModal(oldName string, line, col int, uri string) {
	m.Open = true
	m.OldName = oldName
	m.NewName = oldName
	m.Line = line
	m.Col = col
	m.URI = uri
}

// Close dismisses the rename dialog.
func (m *RenameModal) Close() {
	m.Open = false
}

// HandleKey handles keyboard input inside the rename dialog.
// Returns (consumed bool, confirmed bool).
func (m *RenameModal) HandleKey(k input.Key) (bool, bool) {
	if !m.Open {
		return false, false
	}

	if k.Type == input.KeyEsc {
		m.Close()
		return true, false
	}

	if k.Type == input.KeyEnter {
		name := strings.TrimSpace(m.NewName)
		if name != "" && name != m.OldName {
			m.Close()
			return true, true
		}
		m.Close()
		return true, false
	}

	if k.Type == input.KeyBackspace {
		runes := []rune(m.NewName)
		if len(runes) > 0 {
			m.NewName = string(runes[:len(runes)-1])
		}
		return true, false
	}

	if k.Type == input.KeySpace {
		// Variable names generally don't have spaces, but let the user type if desired
		m.NewName += " "
		return true, false
	}

	if k.Rune >= 32 && !k.HasCtrl() && !k.HasAlt() {
		m.NewName += string(k.Rune)
		return true, false
	}

	return true, false
}

// HandleClick processes mouse interactions inside the rename dialog.
func (m *RenameModal) HandleClick(mouseX, mouseY, screenW, screenH int) (bool, bool) {
	if !m.Open {
		return false, false
	}

	modalW := 52
	if modalW > screenW-4 {
		modalW = screenW - 4
	}
	modalH := 7
	startX := (screenW - modalW) / 2
	startY := (screenH - modalH) / 2

	// Click outside -> dismiss
	if mouseX < startX || mouseX >= startX+modalW || mouseY < startY || mouseY >= startY+modalH {
		m.Close()
		return true, false
	}

	// Close button [✕] at top-right
	if mouseY == startY && mouseX >= startX+modalW-3 && mouseX < startX+modalW {
		m.Close()
		return true, false
	}

	// Button row at startY+5: [Rename] (startX+12) and [Cancel] (startX+26)
	if mouseY == startY+5 {
		if mouseX >= startX+10 && mouseX <= startX+22 {
			name := strings.TrimSpace(m.NewName)
			if name != "" && name != m.OldName {
				m.Close()
				return true, true
			}
			m.Close()
			return true, false
		}
		if mouseX >= startX+24 && mouseX <= startX+34 {
			m.Close()
			return true, false
		}
	}

	return true, false
}

// Render draws the rename symbol dialog onto the buffer.
func (m *RenameModal) Render(buf *buffer.Buffer, screenW, screenH int, theme *ui.Theme) {
	if !m.Open {
		return
	}

	modalW := 52
	if modalW > screenW-4 {
		modalW = screenW - 4
	}
	modalH := 7
	startX := (screenW - modalW) / 2
	startY := (screenH - modalH) / 2

	bg := toColor(theme.PopupBg)
	fg := toColor(theme.Foreground)
	borderFg := toColor(theme.BorderColor)
	activeBg := toColor(theme.PopupSelBg)
	accentFg := toColor(theme.Function)

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

	// Title & Close [✕]
	title := " RENAME SYMBOL (PROJECT-WIDE) "
	for i, r := range title {
		if startX+2+i < startX+modalW-4 {
			buf.SetRune(startX+2+i, startY, r, accentFg, bg, cell.AttrBold)
		}
	}
	buf.SetRune(startX+modalW-3, startY, '✕', toColor(theme.DiagnosticError), bg, cell.AttrBold)

	// Old Name line
	oldLbl := fmt.Sprintf("Current: %s", m.OldName)
	for i, r := range oldLbl {
		if startX+3+i < startX+modalW-3 {
			buf.SetRune(startX+3+i, startY+2, r, toColor(theme.Comment), bg, cell.AttrNone)
		}
	}

	// Input row: New Name: [_______]
	inLbl := "New Name: "
	for i, r := range inLbl {
		buf.SetRune(startX+3+i, startY+3, r, fg, bg, cell.AttrBold)
	}
	inFieldX := startX + 3 + len(inLbl)
	inFieldW := modalW - len(inLbl) - 6
	disp := m.NewName + "▏"
	for i := 0; i < inFieldW; i++ {
		r := ' '
		if i < len([]rune(disp)) {
			r = []rune(disp)[i]
		}
		buf.SetRune(inFieldX+i, startY+3, r, fg, activeBg, cell.AttrNone)
	}

	// Buttons: Rename (Enter)   Cancel (Esc)
	btnRename := " Rename "
	for i, r := range btnRename {
		buf.SetRune(startX+12+i, startY+5, r, toColor(theme.String), activeBg, cell.AttrBold)
	}
	btnCancel := " Cancel "
	for i, r := range btnCancel {
		buf.SetRune(startX+26+i, startY+5, r, toColor(theme.Comment), bg, cell.AttrNone)
	}
}
