package tui

import (
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"tahr/internal/core/db"
	"tahr/internal/ui"
)

// DBDiffModal displays the generated SQL DDL migration diff before execution.
type DBDiffModal struct {
	Diff      *db.SchemaDiff
	UpSQL     string
	DownSQL   string
	ActiveTab int // 0 = Up Migration, 1 = Down Rollback
	Visible   bool
	Theme     *ui.Theme
}

// NewDBDiffModal creates a modal dialog for reviewing schema diffs.
func NewDBDiffModal(diff *db.SchemaDiff, theme *ui.Theme) *DBDiffModal {
	up, down := "", ""
	if diff != nil {
		up, down = diff.GenerateDDLPostgres()
	}
	return &DBDiffModal{
		Diff:    diff,
		UpSQL:   up,
		DownSQL: down,
		Visible: true,
		Theme:   theme,
	}
}

// Render draws the modal dialog centered over screen bounds.
func (m *DBDiffModal) Render(buf *buffer.Buffer, screenW, screenH int) {
	if !m.Visible || m.Diff == nil {
		return
	}

	w := screenW - 10
	if w > 80 {
		w = 80
	}
	h := screenH - 6
	if h > 22 {
		h = 22
	}
	x := (screenW - w) / 2
	y := (screenH - h) / 2

	fg := toColor(m.Theme.Foreground)
	bg := toColor(m.Theme.Background)
	borderFg := toColor(m.Theme.Function)
	warnFg := toColor(0xEB6F92) // Love / Warning Red

	// 1. Draw Modal Window Frame
	for cx := x; cx < x+w; cx++ {
		buf.SetRune(cx, y, '─', borderFg, bg, cell.AttrNone)
		buf.SetRune(cx, y+h-1, '─', borderFg, bg, cell.AttrNone)
	}
	for cy := y; cy < y+h; cy++ {
		buf.SetRune(x, cy, '│', borderFg, bg, cell.AttrNone)
		buf.SetRune(x+w-1, cy, '│', borderFg, bg, cell.AttrNone)
	}
	buf.SetRune(x, y, '╭', borderFg, bg, cell.AttrNone)
	buf.SetRune(x+w-1, y, '╮', borderFg, bg, cell.AttrNone)
	buf.SetRune(x, y+h-1, '╰', borderFg, bg, cell.AttrNone)
	buf.SetRune(x+w-1, y+h-1, '╯', borderFg, bg, cell.AttrNone)

	// Fill background
	for cy := y + 1; cy < y+h-1; cy++ {
		for cx := x + 1; cx < x+w-1; cx++ {
			buf.SetRune(cx, cy, ' ', fg, bg, cell.AttrNone)
		}
	}

	// 2. Title & Tabs
	title := " ⚡ Schema Migration Review "
	for i, r := range []rune(title) {
		buf.SetRune(x+2+i, y, r, borderFg, bg, cell.AttrBold)
	}

	tabBar := " [1: Up Migration (DDL)]   [2: Down Rollback] "
	for i, r := range []rune(tabBar) {
		buf.SetRune(x+2+i, y+2, r, fg, bg, cell.AttrBold)
	}

	// 3. Destructive Warning Banner
	rowY := y + 4
	if m.Diff.HasDestructive {
		warnMsg := " ⚠️  CAUTION: Destructive changes detected (DROP COLUMN / TABLE)! Data loss possible."
		for i, r := range []rune(warnMsg) {
			if x+2+i < x+w-2 {
				buf.SetRune(x+2+i, rowY, r, warnFg, bg, cell.AttrBold)
			}
		}
		rowY += 2
	}

	// 4. SQL Script Viewport
	content := m.UpSQL
	if m.ActiveTab == 1 {
		content = m.DownSQL
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if rowY+i >= y+h-3 {
			break
		}
		lineFg := fg
		if strings.HasPrefix(line, "ALTER") || strings.HasPrefix(line, "CREATE") {
			lineFg = toColor(m.Theme.Function)
		} else if strings.HasPrefix(line, "DROP") {
			lineFg = warnFg
		}
		for j, r := range []rune(line) {
			if x+3+j < x+w-3 {
				buf.SetRune(x+3+j, rowY+i, r, lineFg, bg, cell.AttrNone)
			}
		}
	}

	// 5. Actions Footer
	footer := "[ Apply to Dev DB (Enter) ]   [ Save Migration (Ctrl+S) ]   [ Cancel (Esc) ]"
	footX := x + (w-len(footer))/2
	if footX < x+2 {
		footX = x + 2
	}
	for i, r := range []rune(footer) {
		if footX+i < x+w-2 {
			buf.SetRune(footX+i, y+h-2, r, toColor(m.Theme.Function), bg, cell.AttrBold)
		}
	}
}
