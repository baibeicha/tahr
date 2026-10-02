package tui

import (
	"fmt"
	"path/filepath"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"tahr/internal/core/dap"
	"tahr/internal/ui"
)

// DAPHUDTab represents the active view in the debugger HUD.
type DAPHUDTab string

const (
	HUDTabVariables DAPHUDTab = "variables"
	HUDTabStack     DAPHUDTab = "stack"
	HUDTabWatch     DAPHUDTab = "watch"
)

// DAPHUDBtn records clickable regions in the HUD.
type DAPHUDBtn struct {
	ID   string
	MinX int
	MaxX int
	Y    int
}

// DAPHUDState manages the state of the interactive DAP Debugger HUD.
type DAPHUDState struct {
	Open          bool
	ActiveTab     DAPHUDTab
	Height        int
	WatchInput    string
	WatchHistory  []string
	WatchResults  []string
	Buttons       []DAPHUDBtn
	ScrollY       int
	SelectedFrame int
}

// NewDAPHUDState initializes a new DAP HUD state.
func NewDAPHUDState() *DAPHUDState {
	return &DAPHUDState{
		Open:         false,
		ActiveTab:    HUDTabVariables,
		Height:       8,
		WatchHistory: make([]string, 0),
		WatchResults: make([]string, 0),
	}
}

// Toggle toggles the HUD open/closed.
func (hud *DAPHUDState) Toggle() {
	hud.Open = !hud.Open
}

// RenderDAPHUD draws the debugger HUD onto the frame buffer.
func RenderDAPHUD(
	buf *buffer.Buffer,
	th ui.Theme,
	hud *DAPHUDState,
	sess *dap.Session,
	x, y, w, h int,
) {
	if buf == nil || hud == nil || !hud.Open || w <= 0 || h <= 0 {
		return
	}

	hud.Buttons = nil

	bg := toColor(th.StatusBarBg)
	fg := toColor(th.StatusBarFg)
	borderFg := toColor(th.BorderColor)
	accentFg := toColor(th.Function)
	errFg := toColor(th.DiagnosticError)
	warnFg := toColor(th.DiagnosticWarn)
	greenFg := toColor(th.String)
	btnBg := toColor(th.PopupSelBg)

	// Fill background
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			buf.SetRune(x+col, y+row, ' ', fg, bg, cell.AttrNone)
		}
	}

	// 1. Top border divider
	for col := 0; col < w; col++ {
		buf.SetRune(x+col, y, '─', borderFg, bg, cell.AttrNone)
	}

	// 2. Control Toolbar (Row 0 of HUD)
	title := " DEBUGGER "
	curX := x + 1
	for _, r := range title {
		buf.SetRune(curX, y, r, warnFg, bg, cell.AttrBold)
		curX++
	}

	controlBtns := []struct {
		id    string
		label string
		fg    cell.Color
	}{
		{"dap_cont", " ▶ Cont ", greenFg},
		{"dap_over", " ↷ Over ", accentFg},
		{"dap_into", " ⇣ Into ", accentFg},
		{"dap_out", " ⇡ Out ", accentFg},
		{"dap_stop", " ■ Stop ", errFg},
	}

	curX += 2
	for _, b := range controlBtns {
		btnStart := curX
		for _, r := range b.label {
			if curX < x+w-2 {
				buf.SetRune(curX, y, r, b.fg, btnBg, cell.AttrBold)
				curX++
			}
		}
		if curX > btnStart {
			hud.Buttons = append(hud.Buttons, DAPHUDBtn{
				ID:   b.id,
				MinX: btnStart,
				MaxX: curX - 1,
				Y:    y,
			})
			curX++
		}
	}

	closeBtn := " ✕ "
	closeStartX := x + w - len(closeBtn) - 1
	if closeStartX > curX {
		for i, r := range closeBtn {
			buf.SetRune(closeStartX+i, y, r, errFg, btnBg, cell.AttrBold)
		}
		hud.Buttons = append(hud.Buttons, DAPHUDBtn{
			ID:   "dap_close",
			MinX: closeStartX,
			MaxX: closeStartX + len(closeBtn) - 1,
			Y:    y,
		})
	}

	// 3. Tab Bar (Row 1 of HUD)
	tabY := y + 1
	tabs := []struct {
		tab   DAPHUDTab
		label string
	}{
		{HUDTabVariables, " Variables "},
		{HUDTabStack, " Call Stack "},
		{HUDTabWatch, " Watch & Eval "},
	}

	tabX := x + 2
	for _, t := range tabs {
		tabStart := tabX
		isActive := (hud.ActiveTab == t.tab)
		tBg := bg
		tFg := fg
		attr := cell.AttrNone
		if isActive {
			tBg = toColor(th.SelectionBg)
			tFg = toColor(th.Foreground)
			attr = cell.AttrBold
		}
		for _, r := range t.label {
			if tabX < x+w-2 {
				buf.SetRune(tabX, tabY, r, tFg, tBg, attr)
				tabX++
			}
		}
		hud.Buttons = append(hud.Buttons, DAPHUDBtn{
			ID:   "tab_" + string(t.tab),
			MinX: tabStart,
			MaxX: tabX - 1,
			Y:    tabY,
		})
		tabX += 2
	}

	// 4. Content Area (Row 2 to h-1)
	contentY := y + 2
	contentH := h - 2
	if contentH <= 0 {
		return
	}

	var contentLines []string
	if sess == nil {
		contentLines = []string{"(No active debugging session. Press F5 to launch)"}
	} else {
		switch hud.ActiveTab {
		case HUDTabVariables:
			vars := sess.Variables()
			if len(vars) == 0 {
				contentLines = []string{"(No local variables available at current breakpoint)"}
			} else {
				for _, v := range vars {
					contentLines = append(contentLines, fmt.Sprintf("  %s (%s) = %s", v.Name, v.Type, v.Value))
				}
			}

		case HUDTabStack:
			frames := sess.StackFrames()
			if len(frames) == 0 {
				contentLines = []string{"(No call stack available)"}
			} else {
				for i, fr := range frames {
					prefix := "  "
					if i == hud.SelectedFrame {
						prefix = "▶ "
					}
					base := filepath.Base(fr.Source.Path)
					contentLines = append(contentLines, fmt.Sprintf("%s%s:%d [%s]", prefix, base, fr.Line, fr.Name))
				}
			}

		case HUDTabWatch:
			contentLines = append(contentLines, fmt.Sprintf("Eval Expr: %s█", hud.WatchInput))
			contentLines = append(contentLines, "Results History:")
			if len(hud.WatchResults) == 0 {
				contentLines = append(contentLines, "  (Type expression and press Enter to evaluate)")
			} else {
				for _, res := range hud.WatchResults {
					contentLines = append(contentLines, fmt.Sprintf("  %s", res))
				}
			}
		}
	}

	for row := 0; row < contentH; row++ {
		screenY := contentY + row
		idx := hud.ScrollY + row
		var lineText string
		if idx < len(contentLines) {
			lineText = contentLines[idx]
		}
		runes := []rune(lineText)
		for col := 0; col < w-4; col++ {
			r := ' '
			if col < len(runes) {
				r = runes[col]
			}
			buf.SetRune(x+2+col, screenY, r, toColor(th.Foreground), bg, cell.AttrNone)
		}
	}
}

// FindDAPHUDBtn checks if mouse clicked on a button or tab in the HUD.
func (hud *DAPHUDState) FindDAPHUDBtn(clickX, clickY int) string {
	for _, b := range hud.Buttons {
		if clickY == b.Y && clickX >= b.MinX && clickX <= b.MaxX {
			return b.ID
		}
	}
	return ""
}
