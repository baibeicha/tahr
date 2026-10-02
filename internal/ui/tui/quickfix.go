package tui

import (
	"fmt"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/lsp"
	"tahr/internal/ui"
)

// QuickFixModal renders a floating popup for LSP Code Actions and Quick Fixes (Alt+Enter).
type QuickFixModal struct {
	Open          bool
	Actions       []lsp.CodeAction
	SelectedIndex int
	X             int
	Y             int
	Width         int
	Height        int
	BorderRounded bool
}

// NewQuickFixModal initializes a QuickFixModal.
func NewQuickFixModal() *QuickFixModal {
	return &QuickFixModal{
		Open:          false,
		Actions:       make([]lsp.CodeAction, 0),
		SelectedIndex: 0,
		Width:         52,
		Height:        10,
	}
}

// OpenForActions activates the popup at or near the cursor position.
func (qf *QuickFixModal) OpenForActions(actions []lsp.CodeAction, cursorX, cursorY, screenW, screenH int) {
	if len(actions) == 0 {
		return
	}
	qf.Open = true
	qf.Actions = actions
	qf.SelectedIndex = 0

	w := 54
	maxTitleLen := 20
	for _, a := range actions {
		if l := len(a.Title) + 14; l > maxTitleLen {
			maxTitleLen = l
		}
	}
	if maxTitleLen+4 > w {
		w = maxTitleLen + 4
	}
	if w > screenW-4 {
		w = screenW - 4
	}
	if w < 36 {
		w = 36
	}
	qf.Width = w

	h := len(actions) + 4
	if h > 12 {
		h = 12
	}
	if h > screenH-4 {
		h = screenH - 4
	}
	qf.Height = h

	// Position popup directly below cursor if space permits, else above
	x := cursorX
	if x+w > screenW-2 {
		x = screenW - w - 2
	}
	if x < 1 {
		x = 1
	}

	y := cursorY + 1
	if y+h > screenH-2 {
		y = cursorY - h
	}
	if y < 1 {
		y = 1
	}

	qf.X = x
	qf.Y = y
}

// Close dismisses the modal.
func (qf *QuickFixModal) Close() {
	qf.Open = false
	qf.Actions = nil
	qf.SelectedIndex = 0
}

// HandleKey handles keyboard navigation and action selection.
func (qf *QuickFixModal) HandleKey(k input.Key) (applied bool, action *lsp.CodeAction, closed bool) {
	if !qf.Open {
		return false, nil, false
	}

	switch k.Type {
	case input.KeyEsc:
		qf.Close()
		return false, nil, true

	case input.KeyUp:
		if qf.SelectedIndex > 0 {
			qf.SelectedIndex--
		} else {
			qf.SelectedIndex = len(qf.Actions) - 1
		}
		return false, nil, false

	case input.KeyDown:
		if qf.SelectedIndex < len(qf.Actions)-1 {
			qf.SelectedIndex++
		} else {
			qf.SelectedIndex = 0
		}
		return false, nil, false

	case input.KeyEnter:
		if qf.SelectedIndex >= 0 && qf.SelectedIndex < len(qf.Actions) {
			act := qf.Actions[qf.SelectedIndex]
			qf.Close()
			return true, &act, true
		}
	}

	return false, nil, false
}

// HandleClick handles mouse clicks on items or outside to dismiss.
func (qf *QuickFixModal) HandleClick(mouseX, mouseY int) (applied bool, action *lsp.CodeAction, closed bool) {
	if !qf.Open {
		return false, nil, false
	}

	// Outside click -> dismiss
	if mouseX < qf.X || mouseX >= qf.X+qf.Width || mouseY < qf.Y || mouseY >= qf.Y+qf.Height {
		qf.Close()
		return false, nil, true
	}

	itemY := mouseY - (qf.Y + 2)
	if itemY >= 0 && itemY < len(qf.Actions) {
		qf.SelectedIndex = itemY
		act := qf.Actions[itemY]
		qf.Close()
		return true, &act, true
	}

	return false, nil, false
}

// Render draws the Quick Fix popup on the GoatUI buffer.
func (qf *QuickFixModal) Render(buf *buffer.Buffer, theme *ui.Theme) {
	if !qf.Open || buf == nil || len(qf.Actions) == 0 {
		return
	}

	bg := toColor(theme.Background)
	fg := toColor(theme.Foreground)
	borderFg := toColor(theme.BorderColor)
	accentFg := toColor(theme.Function)
	selectBg := toColor(theme.SelectionBg)
	selectFg := toColor(theme.Foreground)

	// Draw rounded frame and background
	for y := qf.Y; y < qf.Y+qf.Height; y++ {
		for x := qf.X; x < qf.X+qf.Width; x++ {
			var ch rune = ' '
			cBg := bg
			cFg := fg

			isTop := y == qf.Y
			isBottom := y == qf.Y+qf.Height-1
			isLeft := x == qf.X
			isRight := x == qf.X+qf.Width-1

			tl, tr, bl, br := '┌', '┐', '└', '┘'
			if qf.BorderRounded {
				tl, tr, bl, br = '╭', '╮', '╰', '╯'
			}

			if isTop && isLeft {
				ch = tl
				cFg = borderFg
			} else if isTop && isRight {
				ch = tr
				cFg = borderFg
			} else if isBottom && isLeft {
				ch = bl
				cFg = borderFg
			} else if isBottom && isRight {
				ch = br
				cFg = borderFg
			} else if isTop || isBottom {
				ch = '─'
				cFg = borderFg
			} else if isLeft || isRight {
				ch = '│'
				cFg = borderFg
			}

			buf.SetRune(x, y, ch, cFg, cBg, cell.AttrNone)
		}
	}

	// Title
	title := " Quick Fix (Alt+Enter) "
	for i, r := range title {
		tx := qf.X + 2 + i
		if tx < qf.X+qf.Width-1 {
			buf.SetRune(tx, qf.Y, r, accentFg, bg, cell.AttrBold)
		}
	}

	// Actions list
	maxVisible := qf.Height - 3
	for i := 0; i < len(qf.Actions) && i < maxVisible; i++ {
		act := qf.Actions[i]
		rowY := qf.Y + 2 + i
		isSelected := i == qf.SelectedIndex

		itemBg := bg
		itemFg := fg
		prefix := "   "
		if isSelected {
			itemBg = selectBg
			itemFg = selectFg
			prefix = " ▸ "
		}

		kindTag := "Fix"
		if strings.Contains(act.Kind, "refactor") {
			kindTag = "Refactor"
		} else if strings.Contains(act.Kind, "organizeImports") {
			kindTag = "Import"
		}

		lineStr := fmt.Sprintf("%s%-10s %s", prefix, kindTag, act.Title)
		for col := 0; col < qf.Width-2; col++ {
			drawX := qf.X + 1 + col
			var r rune = ' '
			if col < len(lineStr) {
				r = rune(lineStr[col])
			}
			attr := cell.AttrNone
			if isSelected {
				attr = cell.AttrBold
			}
			buf.SetRune(drawX, rowY, r, itemFg, itemBg, attr)
		}
	}

	// Footer hint
	footer := " Enter: Apply  Esc: Cancel "
	for i, r := range footer {
		fx := qf.X + qf.Width - len(footer) - 2 + i
		if fx > qf.X && fx < qf.X+qf.Width-1 {
			buf.SetRune(fx, qf.Y+qf.Height-1, r, toColor(theme.Comment), bg, cell.AttrNone)
		}
	}
}
