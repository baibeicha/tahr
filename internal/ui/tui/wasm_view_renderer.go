package tui

import (
	"fmt"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"tahr/internal/core/sdk"
	"tahr/internal/ui"
)

// ViewRenderer renders declarative sdk.Node trees onto the terminal GoatUI buffer.
type ViewRenderer struct {
	Theme     *ui.Theme
	FocusedID string
}

// NewViewRenderer creates a renderer using the active editor theme.
func NewViewRenderer(theme *ui.Theme) *ViewRenderer {
	return &ViewRenderer{
		Theme: theme,
	}
}

// RenderNode recursively draws a node tree into bounds.
func (vr *ViewRenderer) RenderNode(buf *buffer.Buffer, node *sdk.Node, bounds buffer.Rect) {
	if node == nil || bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}

	fg := toColor(vr.Theme.Foreground)
	bg := toColor(vr.Theme.Background)
	borderFg := toColor(vr.Theme.LineNumber)

	switch node.Type {
	case sdk.NodeTypeVStack:
		vr.renderVStack(buf, node, bounds)

	case sdk.NodeTypeHStack:
		vr.renderHStack(buf, node, bounds)

	case sdk.NodeTypeText:
		textRunes := []rune(node.Text)
		for i, r := range textRunes {
			if i >= bounds.Width {
				break
			}
			buf.SetRune(bounds.X+i, bounds.Y, r, fg, bg, cell.AttrNone)
		}

	case sdk.NodeTypeButton:
		isFocused := node.ID != "" && node.ID == vr.FocusedID
		btnFg := fg
		btnBg := toColor(vr.Theme.SelectionBg)
		attr := cell.AttrNone
		if isFocused {
			btnFg = toColor(vr.Theme.Function)
			attr = cell.AttrBold | cell.AttrUnderline
		}
		label := fmt.Sprintf("[ %s ]", node.Text)
		for i, r := range []rune(label) {
			if i >= bounds.Width {
				break
			}
			buf.SetRune(bounds.X+i, bounds.Y, r, btnFg, btnBg, attr)
		}

	case sdk.NodeTypeInput:
		isFocused := node.ID != "" && node.ID == vr.FocusedID
		inBorderFg := borderFg
		if isFocused {
			inBorderFg = toColor(vr.Theme.Function)
		}
		dispText := node.Value
		if dispText == "" {
			dispText = node.Placeholder
		}

		// Frame: [ text ]
		buf.SetRune(bounds.X, bounds.Y, '[', inBorderFg, bg, cell.AttrNone)
		runes := []rune(dispText)
		contentW := bounds.Width - 2
		for i := 0; i < contentW; i++ {
			r := ' '
			if i < len(runes) {
				r = runes[i]
			}
			buf.SetRune(bounds.X+1+i, bounds.Y, r, fg, bg, cell.AttrNone)
		}
		if bounds.Width > 1 {
			buf.SetRune(bounds.X+bounds.Width-1, bounds.Y, ']', inBorderFg, bg, cell.AttrNone)
		}

	case sdk.NodeTypeDataGrid:
		vr.renderDataGrid(buf, node, bounds)

	case sdk.NodeTypeGraphCanvas:
		// Draw frame placeholder for DAG Canvas
		vr.renderBox(buf, bounds, borderFg, bg)
		msg := "── [ DAG Canvas Viewport ] ──"
		startX := bounds.X + (bounds.Width-len(msg))/2
		if startX < bounds.X {
			startX = bounds.X
		}
		for i, r := range []rune(msg) {
			if startX+i < bounds.X+bounds.Width {
				buf.SetRune(startX+i, bounds.Y, r, toColor(vr.Theme.Function), bg, cell.AttrBold)
			}
		}
	}
}

func (vr *ViewRenderer) renderVStack(buf *buffer.Buffer, node *sdk.Node, bounds buffer.Rect) {
	if len(node.Children) == 0 {
		return
	}
	y := bounds.Y
	availH := bounds.Height

	// Calculate fixed heights vs flex items
	flexCount := 0
	usedH := 0
	for _, child := range node.Children {
		if child.Style.Height > 0 {
			usedH += int(child.Style.Height)
		} else if child.Type == sdk.NodeTypeInput || child.Type == sdk.NodeTypeButton || child.Type == sdk.NodeTypeText {
			usedH += 1
		} else {
			flexCount++
		}
	}

	flexH := 0
	if flexCount > 0 && availH > usedH {
		flexH = (availH - usedH) / flexCount
	}

	for _, child := range node.Children {
		h := 1
		if child.Style.Height > 0 {
			h = int(child.Style.Height)
		} else if flexCount > 0 && (child.Style.Height == sdk.SizeFill || child.Type == sdk.NodeTypeDataGrid || child.Type == sdk.NodeTypeGraphCanvas) {
			h = flexH
		}
		if y+h > bounds.Y+bounds.Height {
			h = bounds.Y + bounds.Height - y
		}
		if h <= 0 {
			break
		}
		vr.RenderNode(buf, child, buffer.NewRect(bounds.X, y, bounds.Width, h))
		y += h
	}
}

func (vr *ViewRenderer) renderHStack(buf *buffer.Buffer, node *sdk.Node, bounds buffer.Rect) {
	if len(node.Children) == 0 {
		return
	}
	x := bounds.X
	totalChildren := len(node.Children)
	colW := bounds.Width / totalChildren
	if colW < 1 {
		colW = 1
	}

	for i, child := range node.Children {
		w := colW
		if i == totalChildren-1 {
			w = bounds.X + bounds.Width - x
		}
		if w <= 0 {
			break
		}
		vr.RenderNode(buf, child, buffer.NewRect(x, bounds.Y, w, bounds.Height))
		x += w
	}
}

func (vr *ViewRenderer) renderDataGrid(buf *buffer.Buffer, node *sdk.Node, bounds buffer.Rect) {
	if bounds.Height < 2 || bounds.Width < 4 {
		return
	}
	fg := toColor(vr.Theme.Foreground)
	bg := toColor(vr.Theme.Background)
	hdrBg := toColor(vr.Theme.SelectionBg)
	hdrFg := toColor(vr.Theme.Function)
	borderFg := toColor(vr.Theme.LineNumber)

	colCount := len(node.DataGridCols)
	if colCount == 0 {
		return
	}
	colW := (bounds.Width - colCount - 1) / colCount
	if colW < 4 {
		colW = 4
	}

	// 1. Draw Column Header
	x := bounds.X
	for c, col := range node.DataGridCols {
		cellText := fmt.Sprintf(" %-*s", colW, col)
		for i, r := range []rune(cellText) {
			if x+i < bounds.X+bounds.Width {
				buf.SetRune(x+i, bounds.Y, r, hdrFg, hdrBg, cell.AttrBold)
			}
		}
		x += colW
		if c < colCount-1 && x < bounds.X+bounds.Width {
			buf.SetRune(x, bounds.Y, '│', borderFg, hdrBg, cell.AttrNone)
			x++
		}
	}

	// 2. Draw Data Rows
	for r, row := range node.DataGridRows {
		rowY := bounds.Y + 1 + r
		if rowY >= bounds.Y+bounds.Height {
			break
		}
		rx := bounds.X
		for c := 0; c < colCount; c++ {
			val := ""
			if c < len(row) {
				val = row[c]
			}
			cellText := fmt.Sprintf(" %-*s", colW, val)
			for i, ru := range []rune(cellText) {
				if rx+i < bounds.X+bounds.Width {
					buf.SetRune(rx+i, rowY, ru, fg, bg, cell.AttrNone)
				}
			}
			rx += colW
			if c < colCount-1 && rx < bounds.X+bounds.Width {
				buf.SetRune(rx, rowY, '│', borderFg, bg, cell.AttrNone)
				rx++
			}
		}
	}
}

func (vr *ViewRenderer) renderBox(buf *buffer.Buffer, b buffer.Rect, fg, bg cell.Color) {
	for x := b.X; x < b.X+b.Width; x++ {
		buf.SetRune(x, b.Y, '─', fg, bg, cell.AttrNone)
		buf.SetRune(x, b.Y+b.Height-1, '─', fg, bg, cell.AttrNone)
	}
	for y := b.Y; y < b.Y+b.Height; y++ {
		buf.SetRune(b.X, y, '│', fg, bg, cell.AttrNone)
		buf.SetRune(b.X+b.Width-1, y, '│', fg, bg, cell.AttrNone)
	}
	buf.SetRune(b.X, b.Y, '╭', fg, bg, cell.AttrNone)
	buf.SetRune(b.X+b.Width-1, b.Y, '╮', fg, bg, cell.AttrNone)
	buf.SetRune(b.X, b.Y+b.Height-1, '╰', fg, bg, cell.AttrNone)
	buf.SetRune(b.X+b.Width-1, b.Y+b.Height-1, '╯', fg, bg, cell.AttrNone)
}

// RenderErrorBoundary draws a prominent crash card when a WASM plugin throws a panic or timeout.
func (vr *ViewRenderer) RenderErrorBoundary(buf *buffer.Buffer, bounds buffer.Rect, pluginID, reason string) {
	warnFg := toColor(0xEB6F92) // Love / Warning Red
	fg := toColor(vr.Theme.Foreground)
	bg := toColor(vr.Theme.Background)

	vr.renderBox(buf, bounds, warnFg, bg)

	title := fmt.Sprintf(" ⚠️ Extension Error Boundary: [%s] ", pluginID)
	for i, r := range []rune(title) {
		if bounds.X+2+i < bounds.X+bounds.Width-2 {
			buf.SetRune(bounds.X+2+i, bounds.Y+1, r, warnFg, bg, cell.AttrBold)
		}
	}

	detail := fmt.Sprintf("Plugin stopped: %s", reason)
	for i, r := range []rune(detail) {
		if bounds.X+2+i < bounds.X+bounds.Width-2 {
			buf.SetRune(bounds.X+2+i, bounds.Y+3, r, fg, bg, cell.AttrNone)
		}
	}

	btn := "[ Reload View (Enter) ]"
	for i, r := range []rune(btn) {
		if bounds.X+2+i < bounds.X+bounds.Width-2 {
			buf.SetRune(bounds.X+2+i, bounds.Y+5, r, toColor(vr.Theme.Function), bg, cell.AttrBold|cell.AttrUnderline)
		}
	}
}

// CollectFocusableIDs traverses node tree and gathers IDs of interactive elements.
func CollectFocusableIDs(node *sdk.Node) []string {
	if node == nil {
		return nil
	}
	var ids []string
	if (node.Type == sdk.NodeTypeButton || node.Type == sdk.NodeTypeInput) && node.ID != "" && !node.Disabled {
		ids = append(ids, node.ID)
	}
	for _, child := range node.Children {
		ids = append(ids, CollectFocusableIDs(child)...)
	}
	return ids
}

// NextFocusID returns the next focusable node ID in cyclic order.
func NextFocusID(node *sdk.Node, current string) string {
	ids := CollectFocusableIDs(node)
	if len(ids) == 0 {
		return ""
	}
	for i, id := range ids {
		if id == current {
			return ids[(i+1)%len(ids)]
		}
	}
	return ids[0]
}

// PrevFocusID returns the previous focusable node ID in cyclic order.
func PrevFocusID(node *sdk.Node, current string) string {
	ids := CollectFocusableIDs(node)
	if len(ids) == 0 {
		return ""
	}
	for i, id := range ids {
		if id == current {
			return ids[(i-1+len(ids))%len(ids)]
		}
	}
	return ids[len(ids)-1]
}
