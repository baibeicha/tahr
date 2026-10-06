package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/i18n"
	"tahr/internal/core/todotree"
	"tahr/internal/ui"
)

// TodoViewMode determines whether items are displayed by file or by tag.
type TodoViewMode int

const (
	TodoViewByFile TodoViewMode = iota
	TodoViewByTag
)

// TodoButtonHit records interactive button coordinates.
type TodoButtonHit struct {
	Action string
	X, Y   int
	Width  int
}

// TodoRowHit records clicked tree row coordinates.
type TodoRowHit struct {
	Index int
	Y     int
	X, W  int
}

// TodoPanel provides an interactive Tool Window for code annotations (TODO, FIXME, BUG, etc.).
type TodoPanel struct {
	WorkspaceDir string
	Scanner      *todotree.Scanner
	ViewMode     TodoViewMode
	Position     string // "right" or "bottom"
	Open         bool

	Items         []todotree.TodoItem
	FilteredItems []todotree.TodoItem
	FilterQuery   string
	SelectedIdx   int
	Offset        int
	Scanning      bool

	ButtonHits []TodoButtonHit
	RowHits    []TodoRowHit

	mu sync.RWMutex

	OnOpenFile func(path string, line int)
	OnToast    func(level, title, msg string)
}

// NewTodoPanel constructs a new TodoPanel.
func NewTodoPanel(workspaceDir string) *TodoPanel {
	return &TodoPanel{
		WorkspaceDir: workspaceDir,
		Scanner:      todotree.NewScanner(),
		ViewMode:     TodoViewByFile,
		Position:     "right",
		Open:         true,
		SelectedIdx:  0,
	}
}

// Refresh triggers an asynchronous scan of the workspace directory.
func (p *TodoPanel) Refresh() {
	p.mu.Lock()
	if p.Scanning {
		p.mu.Unlock()
		return
	}
	p.Scanning = true
	wsDir := p.WorkspaceDir
	scanner := p.Scanner
	p.mu.Unlock()

	go func() {
		items, err := scanner.ScanDirectory(context.Background(), wsDir)
		p.mu.Lock()
		p.Scanning = false
		if err == nil {
			p.Items = items
			p.applyFilterLocked()
		}
		p.mu.Unlock()

		if err != nil && p.OnToast != nil {
			p.OnToast("error", "TODO TREE", "Scan error: "+err.Error())
		}
	}()
}

// SetFilter updates the filter query.
func (p *TodoPanel) SetFilter(query string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.FilterQuery = query
	p.applyFilterLocked()
}

func (p *TodoPanel) applyFilterLocked() {
	if strings.TrimSpace(p.FilterQuery) == "" {
		p.FilteredItems = p.Items
	} else {
		p.FilteredItems = todotree.Filter(p.Items, todotree.FilterOptions{
			Query: p.FilterQuery,
		})
	}
	if p.SelectedIdx >= len(p.FilteredItems) {
		p.SelectedIdx = 0
	}
}

// JumpToSelected triggers navigation to the currently selected todo item.
func (p *TodoPanel) JumpToSelected() {
	p.mu.RLock()
	var selected *todotree.TodoItem
	if p.SelectedIdx >= 0 && p.SelectedIdx < len(p.FilteredItems) {
		it := p.FilteredItems[p.SelectedIdx]
		selected = &it
	}
	fn := p.OnOpenFile
	p.mu.RUnlock()

	if selected != nil && fn != nil {
		fn(selected.FilePath, selected.Line)
	}
}

// HandleKey handles navigation events in the Todo panel.
func (p *TodoPanel) HandleKey(k input.Key) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch k.Type {
	case input.KeyUp:
		if p.SelectedIdx > 0 {
			p.SelectedIdx--
		}
		return true

	case input.KeyDown:
		if p.SelectedIdx < len(p.FilteredItems)-1 {
			p.SelectedIdx++
		}
		return true

	case input.KeyEnter:
		p.mu.Unlock()
		p.JumpToSelected()
		p.mu.Lock()
		return true

	case input.KeyBackspace:
		if len(p.FilterQuery) > 0 {
			p.FilterQuery = p.FilterQuery[:len(p.FilterQuery)-1]
			p.applyFilterLocked()
		}
		return true
	}

	if k.Rune != 0 {
		p.FilterQuery += string(k.Rune)
		p.applyFilterLocked()
		return true
	}

	return false
}

// HandleClick handles mouse clicks on buttons and rows.
func (p *TodoPanel) HandleClick(x, y int) bool {
	p.mu.RLock()
	btnHits := append([]TodoButtonHit{}, p.ButtonHits...)
	rowHits := append([]TodoRowHit{}, p.RowHits...)
	p.mu.RUnlock()

	for _, hit := range btnHits {
		if y == hit.Y && x >= hit.X && x < hit.X+hit.Width {
			switch hit.Action {
			case "refresh":
				p.Refresh()
			case "view_file":
				p.mu.Lock()
				p.ViewMode = TodoViewByFile
				p.mu.Unlock()
			case "view_tag":
				p.mu.Lock()
				p.ViewMode = TodoViewByTag
				p.mu.Unlock()
			case "jump":
				p.JumpToSelected()
			}
			return true
		}
	}

	for _, hit := range rowHits {
		if y == hit.Y && x >= hit.X && x < hit.X+hit.W {
			p.mu.Lock()
			p.SelectedIdx = hit.Index
			p.mu.Unlock()
			p.JumpToSelected()
			return true
		}
	}

	return false
}

// RenderRect renders the panel in the specified rectangular bounds.
func (p *TodoPanel) RenderRect(buf *buffer.Buffer, bounds buffer.Rect, theme *ui.Theme) {
	p.Render(buf, bounds.X, bounds.Y, bounds.Width, bounds.Height, theme)
}

// Render draws the complete Todo Tree tool window at (x, y, w, h).
func (p *TodoPanel) Render(buf *buffer.Buffer, x, y, w, h int, theme *ui.Theme) {
	if w <= 0 || h <= 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.ButtonHits = p.ButtonHits[:0]
	p.RowHits = p.RowHits[:0]

	bg := toColor(theme.StatusBarBg)
	textFg := toColor(theme.Foreground)
	dimFg := toColor(theme.Comment)
	selBg := toColor(theme.PopupSelBg)
	accentFg := toColor(theme.Function)
	errFg := toColor(theme.DiagnosticError)
	warnFg := toColor(theme.DiagnosticWarn)
	greenFg := toColor(theme.String)

	// Fill background
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			buf.SetRune(x+col, y+row, ' ', textFg, bg, cell.AttrNone)
		}
	}

	curY := y

	// 1. Header Bar: Status
	statsText := "○ idle"
	statsColor := greenFg
	if p.Scanning {
		statsText = "● scanning..."
		statsColor = warnFg
	} else {
		statsText = fmt.Sprintf("%d items", len(p.FilteredItems))
	}
	drawText(buf, x+1, curY, fmt.Sprintf("Annotations: %s", statsText), statsColor, bg, cell.AttrBold)
	curY++

	// 2. Action buttons row (with wrapping for narrow sidebars)
	buttons := []struct {
		Label  string
		Action string
		Active bool
	}{
		{" Refresh ", "refresh", false},
		{" By File ", "view_file", p.ViewMode == TodoViewByFile},
		{" By Tag ", "view_tag", p.ViewMode == TodoViewByTag},
		{" Jump ", "jump", false},
	}
	btnX := x + 1
	for _, b := range buttons {
		bLen := len([]rune(b.Label))
		if btnX+bLen >= x+w-1 && btnX > x+1 {
			curY++
			btnX = x + 1
		}
		btnBg := toColor(theme.CursorLineBg)
		btnFg := textFg
		if b.Active {
			btnBg = toColor(theme.SelectionBg)
		}
		drawText(buf, btnX, curY, b.Label, btnFg, btnBg, cell.AttrBold)
		p.ButtonHits = append(p.ButtonHits, TodoButtonHit{
			Action: b.Action,
			X:      btnX,
			Y:      curY,
			Width:  bLen,
		})
		btnX += bLen + 1
	}
	curY++

	// 3. Filter indicator if active
	if p.FilterQuery != "" {
		filterLine := fmt.Sprintf("Filter: %s", p.FilterQuery)
		if len(filterLine) > w-2 {
			filterLine = filterLine[:w-2]
		}
		drawText(buf, x+1, curY, filterLine, dimFg, bg, cell.AttrNone)
		curY++
	}

	// 4. Items List
	if len(p.FilteredItems) == 0 {
		curY++
		emptyTitle := i18n.T("todo.empty_title")
		suppTags := i18n.T("todo.supported_tags")
		instructions := i18n.T("todo.instructions")
		emptyLines := []string{
			emptyTitle,
			"───────────────────────────────",
			suppTags,
			i18n.T("todo.tag_todo"),
			i18n.T("todo.tag_fixme"),
			i18n.T("todo.tag_bug"),
			"• HACK / XXX / NOTE / OPTIMIZE",
			"",
			instructions,
			i18n.T("todo.hint_refresh"),
			i18n.T("todo.hint_add"),
		}
		if p.Scanning {
			emptyLines = []string{
				i18n.T("todo.scanning"),
				"───────────────────────────────",
				i18n.T("todo.scanning_l1"),
				i18n.T("todo.scanning_l2"),
			}
		}
		for _, el := range emptyLines {
			if curY >= y+h-1 {
				break
			}
			c := dimFg
			attr := cell.AttrNone
			if el == emptyTitle || el == i18n.T("todo.scanning") {
				c = warnFg
				attr = cell.AttrBold
			} else if el == suppTags || el == instructions {
				c = accentFg
			} else if strings.HasPrefix(el, "•") {
				c = textFg
			}
			drawText(buf, x+1, curY, el, c, bg, attr)
			curY++
		}
		return
	}

	listH := y + h - curY
	for i := 0; i < listH && i+p.Offset < len(p.FilteredItems); i++ {
		idx := i + p.Offset
		it := p.FilteredItems[idx]
		rowY := curY + i

		isSelected := idx == p.SelectedIdx
		rowBg := bg
		rowFg := textFg
		prefix := "  "
		if isSelected {
			rowBg = selBg
			prefix = "● "
		}

		tagFg := greenFg
		switch it.Tag {
		case "BUG":
			tagFg = errFg
		case "FIXME", "XXX":
			tagFg = warnFg
		case "TODO":
			tagFg = accentFg
		}

		authorPart := ""
		if it.Author != "" {
			authorPart = "(" + it.Author + ")"
		}

		dispText := ""
		if p.ViewMode == TodoViewByTag {
			dispText = fmt.Sprintf("%s%s:%d %s", prefix, it.FilePath, it.Line, it.Message)
		} else {
			dispText = fmt.Sprintf("%s%s%s: %s [%d]", prefix, it.Tag, authorPart, it.Message, it.Line)
		}

		if len(dispText) > w-1 {
			dispText = dispText[:w-1]
		}

		drawText(buf, x+1, rowY, dispText, rowFg, rowBg, cell.AttrNone)
		if !isSelected {
			// Highlight tag badge in color
			tagCol := strings.Index(dispText, it.Tag)
			if tagCol >= 0 {
				drawText(buf, x+1+tagCol, rowY, it.Tag, tagFg, rowBg, cell.AttrBold)
			}
		}

		p.RowHits = append(p.RowHits, TodoRowHit{
			Index: idx,
			Y:     rowY,
			X:     x + 1,
			W:     w - 2,
		})
	}
}
