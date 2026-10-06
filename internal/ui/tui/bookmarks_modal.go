package tui

import (
	"fmt"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/bookmarks"
	"tahr/internal/ui"
)

// BookmarksModalButtonHit records the bounding box of an interactive button.
type BookmarksModalButtonHit struct {
	Action string
	X, Y   int
	Width  int
}

// BookmarkRowHit records row coordinates for list item clicks.
type BookmarkRowHit struct {
	Index int
	Y     int
	X, W  int
}

// BookmarksModal provides an interactive bookmark management UI.
type BookmarksModal struct {
	Open        bool
	Store       *bookmarks.Store
	FilterQuery string
	SelectedIdx int
	Offset      int

	ButtonHits []BookmarksModalButtonHit
	RowHits    []BookmarkRowHit

	OnJump func(bm bookmarks.Bookmark)
}

// NewBookmarksModal creates a new bookmarks modal.
func NewBookmarksModal(store *bookmarks.Store) *BookmarksModal {
	if store == nil {
		store = bookmarks.NewStore("")
	}
	return &BookmarksModal{
		Open:        false,
		Store:       store,
		SelectedIdx: 0,
	}
}

// Show opens the modal.
func (m *BookmarksModal) Show() {
	m.Open = true
	m.FilterQuery = ""
	m.SelectedIdx = 0
	m.Offset = 0
}

// Close closes the modal.
func (m *BookmarksModal) Close() {
	m.Open = false
}

// GetFilteredBookmarks returns bookmarks matching the current filter query.
func (m *BookmarksModal) GetFilteredBookmarks() []*bookmarks.Bookmark {
	all := m.Store.GetBookmarks()
	if strings.TrimSpace(m.FilterQuery) == "" {
		return all
	}
	query := strings.ToLower(strings.TrimSpace(m.FilterQuery))
	var filtered []*bookmarks.Bookmark
	for _, bm := range all {
		if strings.Contains(strings.ToLower(bm.FilePath), query) ||
			strings.Contains(strings.ToLower(bm.Label), query) ||
			strings.Contains(strings.ToLower(bm.LinePreview), query) {
			filtered = append(filtered, bm)
		}
	}
	return filtered
}

// JumpToSelected triggers navigation to the selected bookmark.
func (m *BookmarksModal) JumpToSelected() {
	list := m.GetFilteredBookmarks()
	if m.SelectedIdx >= 0 && m.SelectedIdx < len(list) {
		selected := list[m.SelectedIdx]
		m.Close()
		if m.OnJump != nil {
			m.OnJump(*selected)
		}
	}
}

// DeleteSelected removes the selected bookmark.
func (m *BookmarksModal) DeleteSelected() {
	list := m.GetFilteredBookmarks()
	if m.SelectedIdx >= 0 && m.SelectedIdx < len(list) {
		selected := list[m.SelectedIdx]
		m.Store.RemoveBookmark(selected.ID)
		if m.SelectedIdx >= len(list)-1 && m.SelectedIdx > 0 {
			m.SelectedIdx--
		}
	}
}

// HandleKey processes keyboard navigation and actions.
func (m *BookmarksModal) HandleKey(k input.Key) bool {
	if !m.Open {
		return false
	}

	list := m.GetFilteredBookmarks()

	switch k.Type {
	case input.KeyEsc:
		m.Close()
		return true

	case input.KeyUp:
		if m.SelectedIdx > 0 {
			m.SelectedIdx--
		}
		return true

	case input.KeyDown:
		if m.SelectedIdx < len(list)-1 {
			m.SelectedIdx++
		}
		return true

	case input.KeyEnter:
		m.JumpToSelected()
		return true

	case input.KeyDelete:
		m.DeleteSelected()
		return true

	case input.KeyBackspace:
		if len(m.FilterQuery) > 0 {
			m.FilterQuery = m.FilterQuery[:len(m.FilterQuery)-1]
			m.SelectedIdx = 0
		}
		return true
	}

	if k.Rune != 0 {
		m.FilterQuery += string(k.Rune)
		m.SelectedIdx = 0
		return true
	}

	return false
}

// HandleClick processes clicks on buttons and bookmark rows.
func (m *BookmarksModal) HandleClick(x, y int) bool {
	if !m.Open {
		return false
	}

	for _, hit := range m.ButtonHits {
		if y == hit.Y && x >= hit.X && x < hit.X+hit.Width {
			switch hit.Action {
			case "jump":
				m.JumpToSelected()
			case "delete":
				m.DeleteSelected()
			case "clear_all":
				_ = m.Store.ClearAll()
				m.SelectedIdx = 0
			case "close":
				m.Close()
			}
			return true
		}
	}

	for _, hit := range m.RowHits {
		if y == hit.Y && x >= hit.X && x < hit.X+hit.W {
			m.SelectedIdx = hit.Index
			return true
		}
	}

	return false
}

// Render draws the Bookmarks modal dialog.
func (m *BookmarksModal) Render(buf *buffer.Buffer, screenW, screenH int, theme *ui.Theme) {
	if !m.Open {
		return
	}

	modalW := 76
	if modalW > screenW-4 {
		modalW = screenW - 4
	}
	modalH := 20
	if modalH > screenH-4 {
		modalH = screenH - 4
	}

	x := (screenW - modalW) / 2
	y := (screenH - modalH) / 2

	m.ButtonHits = m.ButtonHits[:0]
	m.RowHits = m.RowHits[:0]

	bg := toColor(theme.StatusBarBg)
	cardBg := toColor(theme.Background)
	textFg := toColor(theme.Foreground)
	dimFg := toColor(theme.Comment)
	borderFg := toColor(theme.BorderColor)
	selBg := toColor(theme.PopupSelBg)
	accentFg := toColor(theme.Function)

	// Backdrop
	for row := 0; row < modalH; row++ {
		for col := 0; col < modalW; col++ {
			buf.SetRune(x+col, y+row, ' ', textFg, bg, cell.AttrNone)
		}
	}

	// Border
	drawBorderBox(buf, x, y, modalW, modalH, borderFg, bg)

	// Title
	title := " Bookmarks Manager "
	drawText(buf, x+2, y, title, accentFg, bg, cell.AttrBold)

	// Close button
	closeLabel := " Close "
	closeX := x + modalW - len(closeLabel) - 2
	drawText(buf, closeX, y, closeLabel, dimFg, toColor(theme.CursorLineBg), cell.AttrNone)
	m.ButtonHits = append(m.ButtonHits, BookmarksModalButtonHit{
		Action: "close",
		X:      closeX,
		Y:      y,
		Width:  len(closeLabel),
	})

	curY := y + 2

	// Filter box
	drawText(buf, x+2, curY, "Filter:", dimFg, bg, cell.AttrNone)
	drawBorderBox(buf, x+10, curY-1, modalW-12, 3, borderFg, cardBg)
	filterDisp := m.FilterQuery
	if filterDisp == "" {
		filterDisp = "Type to search bookmarks..."
		drawText(buf, x+12, curY, filterDisp, dimFg, cardBg, cell.AttrNone)
	} else {
		drawText(buf, x+12, curY, filterDisp, textFg, cardBg, cell.AttrNone)
	}
	curY += 3

	// Action buttons
	buttons := []struct {
		Label  string
		Action string
	}{
		{" Jump ", "jump"},
		{" Delete ", "delete"},
		{" Clear All ", "clear_all"},
	}
	btnX := x + 2
	for _, b := range buttons {
		drawText(buf, btnX, curY, b.Label, textFg, toColor(theme.CursorLineBg), cell.AttrBold)
		m.ButtonHits = append(m.ButtonHits, BookmarksModalButtonHit{
			Action: b.Action,
			X:      btnX,
			Y:      curY,
			Width:  len(b.Label),
		})
		btnX += len(b.Label) + 1
	}
	curY += 2

	// Bookmarks List
	list := m.GetFilteredBookmarks()
	listH := y + modalH - curY - 1

	if len(list) == 0 {
		emptyMsg := "No bookmarks recorded. Press Ctrl+F2 to toggle bookmark on current line."
		drawText(buf, x+3, curY+1, emptyMsg, dimFg, bg, cell.AttrNone)
		return
	}

	for i := 0; i < listH && i+m.Offset < len(list); i++ {
		idx := i + m.Offset
		bm := list[idx]
		rowY := curY + i

		isSelected := idx == m.SelectedIdx
		rowBg := bg
		rowFg := textFg
		prefix := "  "
		if isSelected {
			rowBg = selBg
			prefix = "● "
		}

		desc := bm.Label
		if desc == "" {
			desc = bm.LinePreview
			if desc == "" {
				desc = fmt.Sprintf("Line %d", bm.LineNumber)
			}
		}

		rowText := fmt.Sprintf("%s%s:%d ─ %s", prefix, bm.FilePath, bm.LineNumber, desc)
		if len(rowText) > modalW-4 {
			rowText = rowText[:modalW-4]
		}

		drawText(buf, x+2, rowY, rowText, rowFg, rowBg, cell.AttrNone)
		m.RowHits = append(m.RowHits, BookmarkRowHit{
			Index: idx,
			Y:     rowY,
			X:     x + 2,
			W:     modalW - 4,
		})
	}
}
