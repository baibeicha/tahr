package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/i18n"
	"tahr/internal/core/lsp"
	"tahr/internal/ui"
)

// ProblemItem represents a single issue in the Problems panel.
type ProblemItem struct {
	URI      string
	FilePath string
	Line     int
	Col      int
	Severity lsp.DiagnosticSeverity
	Message  string
	Source   string
}

// ProblemsPanel manages the diagnostics and compiler errors drawer/panel.
type ProblemsPanel struct {
	Open          bool
	Height        int
	Items         []ProblemItem
	SelectedIndex int
	ScrollOffset  int

	OnJump func(filePath string, line, col int)
}

// NewProblemsPanel creates an empty problems view.
func NewProblemsPanel() *ProblemsPanel {
	return &ProblemsPanel{
		Open:   false,
		Height: 8,
		Items:  make([]ProblemItem, 0),
	}
}

// Toggle toggles the problems panel open or closed.
func (p *ProblemsPanel) Toggle() {
	p.Open = !p.Open
}

// Refresh populates problems from the active document diagnostic mapping.
func (p *ProblemsPanel) Refresh(docDiagDetails map[string]map[int][]lsp.Diagnostic) {
	var items []ProblemItem

	for uri, lineMap := range docDiagDetails {
		filePath := uri
		if strings.HasPrefix(filePath, "file:///") {
			filePath = strings.TrimPrefix(filePath, "file:///")
		} else if strings.HasPrefix(filePath, "file://") {
			filePath = strings.TrimPrefix(filePath, "file://")
		}

		for lineIdx, diagList := range lineMap {
			for _, diag := range diagList {
				msg := strings.TrimSpace(diag.Message)
				if idx := strings.Index(msg, "\n"); idx != -1 {
					msg = msg[:idx] // Single line summary
				}
				items = append(items, ProblemItem{
					URI:      uri,
					FilePath: filePath,
					Line:     lineIdx,
					Col:      diag.Range.Start.Character,
					Severity: diag.Severity,
					Message:  msg,
					Source:   diag.Source,
				})
			}
		}
	}

	// Sort by severity (errors first, then warnings), then file, then line
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Severity != items[j].Severity {
			return items[i].Severity < items[j].Severity
		}
		if items[i].FilePath != items[j].FilePath {
			return items[i].FilePath < items[j].FilePath
		}
		return items[i].Line < items[j].Line
	})

	p.Items = items
	if p.SelectedIndex >= len(items) {
		p.SelectedIndex = max(0, len(items)-1)
	}
}

// ErrorCount returns number of errors (Severity == 1).
func (p *ProblemsPanel) ErrorCount() int {
	c := 0
	for _, it := range p.Items {
		if it.Severity == lsp.SeverityError {
			c++
		}
	}
	return c
}

// WarningCount returns number of warnings (Severity == 2).
func (p *ProblemsPanel) WarningCount() int {
	c := 0
	for _, it := range p.Items {
		if it.Severity == lsp.SeverityWarning {
			c++
		}
	}
	return c
}

// HandleKey handles navigation inside the problems panel.
func (p *ProblemsPanel) HandleKey(k input.Key) bool {
	if !p.Open {
		return false
	}

	if k.Type == input.KeyEsc {
		p.Open = false
		return true
	}

	switch k.Type {
	case input.KeyUp:
		if p.SelectedIndex > 0 {
			p.SelectedIndex--
			if p.SelectedIndex < p.ScrollOffset {
				p.ScrollOffset = p.SelectedIndex
			}
		}
		return true
	case input.KeyDown:
		if p.SelectedIndex < len(p.Items)-1 {
			p.SelectedIndex++
		}
		return true
	case input.KeyPgUp:
		p.SelectedIndex = max(0, p.SelectedIndex-8)
		p.ScrollOffset = max(0, p.ScrollOffset-8)
		return true
	case input.KeyPgDown:
		p.SelectedIndex = min(len(p.Items)-1, p.SelectedIndex+8)
		return true
	case input.KeyHome:
		p.SelectedIndex = 0
		p.ScrollOffset = 0
		return true
	case input.KeyEnd:
		p.SelectedIndex = max(0, len(p.Items)-1)
		return true
	case input.KeyEnter:
		if len(p.Items) > 0 && p.SelectedIndex >= 0 && p.SelectedIndex < len(p.Items) {
			target := p.Items[p.SelectedIndex]
			if p.OnJump != nil {
				p.OnJump(target.FilePath, target.Line, target.Col)
			}
		}
		return true
	}

	return false
}

// Render draws the Problems drawer at (x, y, w, h).
func (p *ProblemsPanel) Render(buf *buffer.Buffer, x, y, w, h int, theme ui.Theme) {
	if !p.Open || h < 2 || w < 20 {
		return
	}

	bg := cell.Color{Type: cell.ColorRGB, Value: theme.StatusBarBg}
	borderFg := cell.Color{Type: cell.ColorRGB, Value: theme.BorderColor}
	textFg := cell.Color{Type: cell.ColorRGB, Value: theme.Foreground}
	dimFg := cell.Color{Type: cell.ColorRGB, Value: theme.Comment}
	selBg := cell.Color{Type: cell.ColorRGB, Value: theme.PopupSelBg}

	errFg := cell.Color{Type: cell.ColorRGB, Value: theme.DiagnosticError}
	warnFg := cell.Color{Type: cell.ColorRGB, Value: theme.DiagnosticWarn}
	infoFg := cell.Color{Type: cell.ColorRGB, Value: theme.DiagnosticInfo}

	// 1. Header bar
	headerTitle := fmt.Sprintf(" %s ", fmt.Sprintf(i18n.T("problems.title"), p.ErrorCount(), p.WarningCount()))
	headerRunes := []rune(headerTitle)
	closeBtn := fmt.Sprintf(" %s ", i18n.T("problems.close"))
	closeRunes := []rune(closeBtn)

	for col := 0; col < w; col++ {
		screenX := x + col
		r := '─'
		fg := borderFg
		if col == 0 {
			r = '┌'
		} else if col < len(headerRunes) {
			r = headerRunes[col]
			fg = textFg
		} else if col >= w-len(closeRunes) {
			idx := col - (w - len(closeRunes))
			if idx >= 0 && idx < len(closeRunes) {
				r = closeRunes[idx]
				fg = errFg
			}
		}
		buf.SetRune(screenX, y, r, fg, bg, cell.AttrBold)
	}

	// 2. Problem items
	contentH := h - 1
	if p.SelectedIndex >= p.ScrollOffset+contentH {
		p.ScrollOffset = p.SelectedIndex - contentH + 1
	}
	if p.SelectedIndex < p.ScrollOffset {
		p.ScrollOffset = p.SelectedIndex
	}

	for row := 0; row < contentH; row++ {
		screenY := y + 1 + row
		idx := p.ScrollOffset + row

		// Clear line background
		for col := 0; col < w; col++ {
			buf.SetRune(x+col, screenY, ' ', textFg, bg, cell.AttrNone)
		}

		if idx >= len(p.Items) {
			if len(p.Items) == 0 && row == 0 {
				noProb := i18n.T("problems.clean")
				for ci, cr := range []rune(noProb) {
					if x+2+ci < x+w-2 {
						buf.SetRune(x+2+ci, screenY, cr, cell.Color{Type: cell.ColorRGB, Value: theme.String}, bg, cell.AttrNone)
					}
				}
			}
			continue
		}

		item := p.Items[idx]
		isSel := idx == p.SelectedIndex
		rowBg := bg
		if isSel {
			rowBg = selBg
		}

		for col := 0; col < w; col++ {
			buf.SetRune(x+col, screenY, ' ', textFg, rowBg, cell.AttrNone)
		}

		// Severity Tag: [ERROR], [WARN], [INFO]
		tag := i18n.T("problems.tag_error")
		tagFg := errFg
		if item.Severity == lsp.SeverityWarning {
			tag = i18n.T("problems.tag_warn")
			tagFg = warnFg
		} else if item.Severity >= lsp.SeverityInformation {
			tag = i18n.T("problems.tag_info")
			tagFg = infoFg
		}

		curCol := x + 1
		for _, r := range []rune(tag) {
			if curCol < x+w-1 {
				buf.SetRune(curCol, screenY, r, tagFg, rowBg, cell.AttrBold)
				curCol++
			}
		}

		// Message
		msgRunes := []rune(item.Message)
		for _, r := range msgRunes {
			if curCol < x+w-25 {
				buf.SetRune(curCol, screenY, r, textFg, rowBg, cell.AttrNone)
				curCol++
			}
		}

		// Location: "file.go:12:4"
		locStr := fmt.Sprintf(" %s:%d:%d", item.FilePath, item.Line+1, item.Col+1)
		locRunes := []rune(locStr)
		locCol := x + w - len(locRunes) - 1
		if locCol > curCol+1 {
			for li, lr := range locRunes {
				buf.SetRune(locCol+li, screenY, lr, dimFg, rowBg, cell.AttrItalic)
			}
		}
	}
}
