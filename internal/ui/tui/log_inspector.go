package tui

import (
	"fmt"
	"strings"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/i18n"
	"tahr/internal/ui"
)

// LogInspectorModal displays detailed diagnostic logs, crash traces, and system metadata.
type LogInspectorModal struct {
	Open        bool
	Title       string
	ActiveTab   int // 0: Logs / Trace, 1: Metadata, 2: Raw JSON
	ScrollY     int
	SearchQuery string
	Searching   bool

	tab1Lines []string
	tab2Lines []string
	tab3Lines []string
}

// NewLogInspectorModal creates a new inspector modal instance.
func NewLogInspectorModal() *LogInspectorModal {
	return &LogInspectorModal{
		Open:      false,
		Title:     i18n.T("modal.inspector.title"),
		ActiveTab: 0,
	}
}

// SetContent sets the content for the inspector tabs.
func (m *LogInspectorModal) SetContent(title string, traceAndLogs []string, metadata []string, rawJSON string) {
	m.Open = true
	if title != "" {
		m.Title = title
	}
	m.ActiveTab = 0
	m.ScrollY = 0
	m.SearchQuery = ""
	m.Searching = false

	m.tab1Lines = traceAndLogs
	m.tab2Lines = metadata

	if rawJSON != "" {
		m.tab3Lines = strings.Split(rawJSON, "\n")
	} else {
		m.tab3Lines = []string{i18n.T("modal.inspector.no_json")}
	}
}

func (m *LogInspectorModal) currentLines() []string {
	var src []string
	switch m.ActiveTab {
	case 0:
		src = m.tab1Lines
	case 1:
		src = m.tab2Lines
	case 2:
		src = m.tab3Lines
	}

	if m.SearchQuery == "" {
		return src
	}

	lowerQ := strings.ToLower(m.SearchQuery)
	var filtered []string
	for _, l := range src {
		if strings.Contains(strings.ToLower(l), lowerQ) {
			filtered = append(filtered, l)
		}
	}
	return filtered
}

// HandleKey processes keys for scrolling, switching tabs, search, and close.
func (m *LogInspectorModal) HandleKey(key input.Key) bool {
	if !m.Open {
		return false
	}

	if m.Searching {
		switch key.Type {
		case input.KeyEsc, input.KeyEnter:
			m.Searching = false
			return true
		case input.KeyBackspace:
			runes := []rune(m.SearchQuery)
			if len(runes) > 0 {
				m.SearchQuery = string(runes[:len(runes)-1])
			}
			return true
		default:
			if key.Rune != 0 {
				m.SearchQuery += string(key.Rune)
				return true
			}
		}
		return true
	}

	if key.Rune == 'q' || key.Rune == 'Q' {
		m.Open = false
		return true
	}

	if key.Rune == '/' {
		m.Searching = true
		m.SearchQuery = ""
		return true
	}

	switch key.Type {
	case input.KeyEsc:
		m.Open = false
		return true

	case input.KeyTab:
		m.ActiveTab = (m.ActiveTab + 1) % 3
		m.ScrollY = 0
		return true

	case input.KeyBacktab:
		m.ActiveTab = (m.ActiveTab + 2) % 3
		m.ScrollY = 0
		return true

	case input.KeyUp:
		if m.ScrollY > 0 {
			m.ScrollY--
		}
		return true

	case input.KeyDown:
		lines := m.currentLines()
		if m.ScrollY < len(lines)-1 {
			m.ScrollY++
		}
		return true

	case input.KeyPgUp:
		m.ScrollY -= 15
		if m.ScrollY < 0 {
			m.ScrollY = 0
		}
		return true

	case input.KeyPgDown:
		lines := m.currentLines()
		m.ScrollY += 15
		if m.ScrollY >= len(lines) {
			m.ScrollY = max(0, len(lines)-1)
		}
		return true

	case input.KeyHome:
		m.ScrollY = 0
		return true

	case input.KeyEnd:
		lines := m.currentLines()
		m.ScrollY = max(0, len(lines)-1)
		return true
	}

	return true
}

// HandleMouse processes mouse clicks and scrolling.
func (m *LogInspectorModal) HandleMouse(ev input.Mouse, screenW, screenH int) bool {
	if !m.Open {
		return false
	}

	modalW := 86
	if modalW > screenW-4 {
		modalW = screenW - 4
	}
	modalH := 24
	if modalH > screenH-4 {
		modalH = screenH - 4
	}
	startX := (screenW - modalW) / 2
	startY := (screenH - modalH) / 2

	// Mouse wheel
	if ev.Button == input.MouseWheelUp {
		if m.ScrollY > 0 {
			m.ScrollY = max(0, m.ScrollY-3)
		}
		return true
	}
	if ev.Button == input.MouseWheelDown {
		lines := m.currentLines()
		if m.ScrollY < len(lines)-1 {
			m.ScrollY = min(len(lines)-1, m.ScrollY+3)
		}
		return true
	}

	if ev.Button == input.MouseLeft {
		// Close button at top right
		if ev.Y == startY && ev.X == startX+modalW-2 {
			m.Open = false
			return true
		}

		// Tab clicks (startY+2)
		if ev.Y == startY+2 {
			if ev.X >= startX+3 && ev.X <= startX+17 {
				m.ActiveTab = 0
				m.ScrollY = 0
				return true
			}
			if ev.X >= startX+19 && ev.X <= startX+40 {
				m.ActiveTab = 1
				m.ScrollY = 0
				return true
			}
			if ev.X >= startX+42 && ev.X <= startX+55 {
				m.ActiveTab = 2
				m.ScrollY = 0
				return true
			}
		}

		// Close button at bottom right (startY+modalH-2)
		if ev.Y == startY+modalH-2 && ev.X >= startX+modalW-18 && ev.X <= startX+modalW-3 {
			m.Open = false
			return true
		}
	}

	return true
}

// Render draws the log inspector sub-modal into the GoatUI buffer.
func (m *LogInspectorModal) Render(buf *buffer.Buffer, screenW, screenH int, theme *ui.Theme) {
	if !m.Open {
		return
	}

	modalW := 86
	if modalW > screenW-4 {
		modalW = screenW - 4
	}
	modalH := 24
	if modalH > screenH-4 {
		modalH = screenH - 4
	}
	startX := (screenW - modalW) / 2
	if startX < 2 {
		startX = 2
	}
	startY := (screenH - modalH) / 2
	if startY < 1 {
		startY = 1
	}

	bg := toColor(theme.PopupBg)
	fg := toColor(theme.Foreground)
	borderFg := toColor(theme.BorderColor)
	accentFg := toColor(theme.Keyword)
	commentCol := toColor(theme.Comment)
	tabSelBg := toColor(theme.PopupSelBg)
	viewBg := toColor(theme.Background)
	matchFg := toColor(theme.String)

	// Draw outer border
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
			} else if y == startY || y == startY+modalH-1 || y == startY+3 || y == startY+modalH-3 {
				r = '─'
			} else if x == startX || x == startX+modalW-1 {
				r = '│'
			}
			buf.SetRune(x, y, r, f, b, cell.AttrNone)
		}
	}

	// Title
	title := fmt.Sprintf(" %s ", m.Title)
	for i, r := range []rune(title) {
		if startX+2+i < startX+modalW-4 {
			buf.SetRune(startX+2+i, startY, r, accentFg, bg, cell.AttrBold)
		}
	}
	buf.SetRune(startX+modalW-2, startY, '✕', toColor(theme.DiagnosticError), bg, cell.AttrBold)

	// Tab Bar (startY+2)
	tabs := []string{i18n.T("modal.inspector.tab_logs"), i18n.T("modal.inspector.tab_env"), i18n.T("modal.inspector.tab_json")}
	tabX := startX + 3
	for i, tab := range tabs {
		tFg, tBg := fg, bg
		if i == m.ActiveTab {
			tFg = accentFg
			tBg = tabSelBg
		}
		for _, r := range []rune(tab) {
			buf.SetRune(tabX, startY+1, r, tFg, tBg, cell.AttrBold)
			tabX++
		}
		tabX += 2
	}

	// Content area
	contentStartY := startY + 4
	contentH := modalH - 7
	lines := m.currentLines()

	for row := 0; row < contentH; row++ {
		lineIdx := m.ScrollY + row
		drawY := contentStartY + row

		// Fill line background
		for x := startX + 2; x < startX+modalW-2; x++ {
			buf.SetRune(x, drawY, ' ', fg, viewBg, cell.AttrNone)
		}

		if lineIdx < len(lines) {
			text := lines[lineIdx]
			runes := []rune(text)
			lineW := modalW - 6
			if len(runes) > lineW {
				runes = runes[:lineW]
			}

			// Check search highlighting
			lowerLine := strings.ToLower(string(runes))
			lowerQ := strings.ToLower(m.SearchQuery)

			for col, r := range runes {
				fCol := fg
				if m.SearchQuery != "" && strings.Contains(lowerLine, lowerQ) {
					fCol = matchFg
				}
				buf.SetRune(startX+3+col, drawY, r, fCol, viewBg, cell.AttrNone)
			}
		}
	}

	// Scrollbar
	if len(lines) > contentH {
		thumbH := max(1, contentH*contentH/len(lines))
		thumbY := contentStartY + (m.ScrollY * (contentH - thumbH) / max(1, len(lines)-contentH))
		for y := contentStartY; y < contentStartY+contentH; y++ {
			r := '│'
			f := borderFg
			if y >= thumbY && y < thumbY+thumbH {
				r = '█'
				f = accentFg
			}
			buf.SetRune(startX+modalW-3, y, r, f, viewBg, cell.AttrNone)
		}
	}

	// Bottom search bar or status
	statY := startY + modalH - 2
	if m.Searching {
		prompt := i18n.T("modal.inspector.search_prompt", m.SearchQuery)
		drawString(buf, startX+3, statY, prompt, accentFg, bg, cell.AttrBold, modalW-24)
	} else if m.SearchQuery != "" {
		info := i18n.T("modal.inspector.filter_stats", m.SearchQuery, len(lines))
		drawString(buf, startX+3, statY, info, commentCol, bg, cell.AttrNone, modalW-24)
	} else {
		hint := i18n.T("modal.inspector.nav_hint")
		drawString(buf, startX+3, statY, hint, commentCol, bg, cell.AttrNone, modalW-24)
	}

	// Close button strictly without square brackets
	btnText := i18n.T("btn.close_esc")
	btnX := startX + modalW - len([]rune(btnText)) - 2
	drawButton(buf, btnX, statY, btnText, fg, tabSelBg)
}
