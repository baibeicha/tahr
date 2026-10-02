package tui

import (
	"fmt"
	"path/filepath"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"tahr/internal/core"
)

// SplitLayoutMode defines the geometry arrangement of editor splits (1 to 6 panes).
type SplitLayoutMode int

const (
	SplitSingle SplitLayoutMode = 1 // 1 pane (100% width)
	Split2Cols  SplitLayoutMode = 2 // 2 vertical columns
	Split2Rows  SplitLayoutMode = 3 // 2 horizontal rows
	Split3Cols  SplitLayoutMode = 4 // 3 vertical columns
	Split4Grid  SplitLayoutMode = 5 // 2x2 grid (4 panes)
	Split5Panes SplitLayoutMode = 6 // 2 top, 3 bottom
	Split6Grid  SplitLayoutMode = 7 // 3 columns x 2 rows (6 panes)
)

// SplitPane represents a single independent viewport slice inside the multi-split grid.
type SplitPane struct {
	Index           int
	DocID           string
	ViewportX       int
	ViewportY       int
	TargetViewportY float64
	SmoothScrollY   float64
	ScrollVelocity  float64
	Bounds          buffer.Rect // Area on screen (X, Y, Width, Height)
	GutterWidth     int
}

// SplitManager coordinates active panes, multi-file assignments, and grid calculations.
type SplitManager struct {
	Mode        SplitLayoutMode
	ActiveIndex int
	Panes       []SplitPane
}

// NewSplitManager initializes a single-pane editor by default.
func NewSplitManager() *SplitManager {
	return &SplitManager{
		Mode:        SplitSingle,
		ActiveIndex: 0,
		Panes: []SplitPane{
			{Index: 0},
		},
	}
}

// TotalPanes returns the number of active panes for the current layout mode.
func (sm *SplitManager) TotalPanes() int {
	switch sm.Mode {
	case SplitSingle:
		return 1
	case Split2Cols, Split2Rows:
		return 2
	case Split3Cols:
		return 3
	case Split4Grid:
		return 4
	case Split5Panes:
		return 5
	case Split6Grid:
		return 6
	default:
		return 1
	}
}

// NextPane advances focus to the next pane in cyclic order.
func (sm *SplitManager) NextPane() int {
	total := sm.TotalPanes()
	if total <= 1 {
		return 0
	}
	sm.ActiveIndex = (sm.ActiveIndex + 1) % total
	return sm.ActiveIndex
}

// PrevPane moves focus to the previous pane in cyclic order.
func (sm *SplitManager) PrevPane() int {
	total := sm.TotalPanes()
	if total <= 1 {
		return 0
	}
	sm.ActiveIndex = (sm.ActiveIndex - 1 + total) % total
	return sm.ActiveIndex
}

// SetPane directly focuses a pane by its 0-based index.
func (sm *SplitManager) SetPane(idx int) {
	total := sm.TotalPanes()
	if idx >= 0 && idx < total {
		sm.ActiveIndex = idx
	}
}

// ActivePaneIndex returns the index of the currently active pane.
func (sm *SplitManager) ActivePaneIndex() int {
	return sm.ActiveIndex
}

// PaneAt returns a pointer to the pane at idx or nil if out of bounds.
func (sm *SplitManager) PaneAt(idx int) *SplitPane {
	if idx >= 0 && idx < len(sm.Panes) {
		return &sm.Panes[idx]
	}
	return nil
}

// CycleLayout cycles between all 1 to 6 pane split configurations.
func (sm *SplitManager) CycleLayout() SplitLayoutMode {
	switch sm.Mode {
	case SplitSingle:
		sm.Mode = Split2Cols
	case Split2Cols:
		sm.Mode = Split2Rows
	case Split2Rows:
		sm.Mode = Split3Cols
	case Split3Cols:
		sm.Mode = Split4Grid
	case Split4Grid:
		sm.Mode = Split5Panes
	case Split5Panes:
		sm.Mode = Split6Grid
	case Split6Grid:
		sm.Mode = SplitSingle
	default:
		sm.Mode = SplitSingle
	}
	if sm.ActiveIndex >= sm.TotalPanes() {
		sm.ActiveIndex = 0
	}
	return sm.Mode
}

// SetLayout explicitly sets the layout mode and keeps ActiveIndex within bounds.
func (sm *SplitManager) SetLayout(mode SplitLayoutMode) {
	sm.Mode = mode
	if sm.ActiveIndex >= sm.TotalPanes() {
		sm.ActiveIndex = max(0, sm.TotalPanes()-1)
	}
}

// ModeTitle returns a clean label describing the current split layout.
func (sm *SplitManager) ModeTitle() string {
	switch sm.Mode {
	case SplitSingle:
		return "Single Pane"
	case Split2Cols:
		return "2 Columns"
	case Split2Rows:
		return "2 Rows"
	case Split3Cols:
		return "3 Columns"
	case Split4Grid:
		return "4 Grid (2x2)"
	case Split5Panes:
		return "5 Panes"
	case Split6Grid:
		return "6 Grid (3x2)"
	default:
		return "Single Pane"
	}
}

// UpdateLayout distributes the editor canvas rectangle across the active number of panes.
func (sm *SplitManager) UpdateLayout(area buffer.Rect, docs []*core.Document, activeDocID string) {
	total := sm.TotalPanes()
	for len(sm.Panes) < total {
		sm.Panes = append(sm.Panes, SplitPane{Index: len(sm.Panes)})
	}
	if len(sm.Panes) > total {
		sm.Panes = sm.Panes[:total]
	}

	// Assign documents to panes: active document to active pane, other open docs to other panes
	for i := range sm.Panes {
		p := &sm.Panes[i]
		if p.DocID == "" {
			if i < len(docs) {
				p.DocID = docs[i].ID
			} else {
				p.DocID = activeDocID
			}
		}
	}
	if sm.ActiveIndex >= 0 && sm.ActiveIndex < len(sm.Panes) && activeDocID != "" {
		sm.Panes[sm.ActiveIndex].DocID = activeDocID
	}

	x := area.X
	y := area.Y
	w := area.Width
	h := area.Height

	switch sm.Mode {
	case SplitSingle:
		sm.Panes[0].Bounds = area

	case Split2Cols:
		w1 := (w - 1) / 2
		w2 := w - 1 - w1
		sm.Panes[0].Bounds = buffer.NewRect(x, y, w1, h)
		sm.Panes[1].Bounds = buffer.NewRect(x+w1+1, y, w2, h)

	case Split2Rows:
		h1 := (h - 1) / 2
		h2 := h - 1 - h1
		sm.Panes[0].Bounds = buffer.NewRect(x, y, w, h1)
		sm.Panes[1].Bounds = buffer.NewRect(x, y+h1+1, w, h2)

	case Split3Cols:
		colW := (w - 2) / 3
		sm.Panes[0].Bounds = buffer.NewRect(x, y, colW, h)
		sm.Panes[1].Bounds = buffer.NewRect(x+colW+1, y, colW, h)
		sm.Panes[2].Bounds = buffer.NewRect(x+(colW+1)*2, y, w-(colW+1)*2, h)

	case Split4Grid:
		w1 := (w - 1) / 2
		w2 := w - 1 - w1
		h1 := (h - 1) / 2
		h2 := h - 1 - h1
		sm.Panes[0].Bounds = buffer.NewRect(x, y, w1, h1)
		sm.Panes[1].Bounds = buffer.NewRect(x+w1+1, y, w2, h1)
		sm.Panes[2].Bounds = buffer.NewRect(x, y+h1+1, w1, h2)
		sm.Panes[3].Bounds = buffer.NewRect(x+w1+1, y+h1+1, w2, h2)

	case Split5Panes:
		// Top row: 2 panes; Bottom row: 3 panes
		h1 := (h - 1) / 2
		h2 := h - 1 - h1
		wTop1 := (w - 1) / 2
		wTop2 := w - 1 - wTop1
		sm.Panes[0].Bounds = buffer.NewRect(x, y, wTop1, h1)
		sm.Panes[1].Bounds = buffer.NewRect(x+wTop1+1, y, wTop2, h1)

		colBotW := (w - 2) / 3
		sm.Panes[2].Bounds = buffer.NewRect(x, y+h1+1, colBotW, h2)
		sm.Panes[3].Bounds = buffer.NewRect(x+colBotW+1, y+h1+1, colBotW, h2)
		sm.Panes[4].Bounds = buffer.NewRect(x+(colBotW+1)*2, y+h1+1, w-(colBotW+1)*2, h2)

	case Split6Grid:
		// 3 columns x 2 rows
		h1 := (h - 1) / 2
		h2 := h - 1 - h1
		colW := (w - 2) / 3
		w3 := w - (colW+1)*2
		sm.Panes[0].Bounds = buffer.NewRect(x, y, colW, h1)
		sm.Panes[1].Bounds = buffer.NewRect(x+colW+1, y, colW, h1)
		sm.Panes[2].Bounds = buffer.NewRect(x+(colW+1)*2, y, w3, h1)
		sm.Panes[3].Bounds = buffer.NewRect(x, y+h1+1, colW, h2)
		sm.Panes[4].Bounds = buffer.NewRect(x+colW+1, y+h1+1, colW, h2)
		sm.Panes[5].Bounds = buffer.NewRect(x+(colW+1)*2, y+h1+1, w3, h2)
	}
}

// FindPaneAt finds which split pane contains the (x, y) terminal coordinates.
func (sm *SplitManager) FindPaneAt(x, y int) int {
	for i, p := range sm.Panes {
		if x >= p.Bounds.X && x < p.Bounds.X+p.Bounds.Width &&
			y >= p.Bounds.Y && y < p.Bounds.Y+p.Bounds.Height {
			return i
		}
	}
	return -1
}

// ActivePane returns pointer to currently focused SplitPane.
func (sm *SplitManager) ActivePane() *SplitPane {
	if sm.ActiveIndex >= 0 && sm.ActiveIndex < len(sm.Panes) {
		return &sm.Panes[sm.ActiveIndex]
	}
	if len(sm.Panes) > 0 {
		return &sm.Panes[0]
	}
	return nil
}

// PaneTitle formats the mini pill title for a pane.
func PaneTitle(idx int, doc *core.Document) string {
	name := "untitled"
	mod := ""
	if doc != nil {
		if doc.FilePath != "" {
			name = filepath.Base(doc.FilePath)
		}
		if doc.Buffer.IsModified() {
			mod = " ●"
		}
	}
	return fmt.Sprintf(" [%d: %s%s] ", idx+1, name, mod)
}
