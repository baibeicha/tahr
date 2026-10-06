package tui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/i18n"
	"tahr/internal/core/logviewer"
	"tahr/internal/ui"
)

// LogDockPosition indicates whether the log panel is docked at the bottom or right sidebar.
type LogDockPosition int

const (
	LogDockBottom LogDockPosition = iota
	LogDockRight
)

// logPillButton represents a clickable filter button on the filter bar.
type logPillButton struct {
	ID     string
	Label  string
	Active bool
	X, Y   int
	Width  int
}

// LogPanel is a dockable tool window for viewing, filtering, and streaming logs.
// It can be docked to the Bottom Panel (drawer) or Right Sidebar.
type LogPanel struct {
	mu   sync.RWMutex
	Open bool
	Dock LogDockPosition

	// Dimensions
	Height int // Height for bottom drawer (default 10)
	Width  int // Width for right sidebar (default 50)

	// Engine
	Ring   *logviewer.RingBuffer
	Tailer *logviewer.Tailer

	// Filter & Search State
	ActiveLevelFilter logviewer.LogLevel // LevelUnknown = All, LevelError, LevelWarn, LevelInfo
	SearchText        string
	SearchIsRegex     bool
	SearchFocused     bool
	SearchCursor      int

	// Scrolling & Tail State
	AutoScroll    bool // When true, automatically follows latest log lines at bottom
	ScrollOffset  int  // Vertical scroll offset in filtered lines list
	SelectedIndex int  // Selected line index in filtered lines list (-1 if none)

	// Cached filtered lines
	cachedFiltered []logviewer.LogLine
	lastRingCount  int
	lastFilterRev  int
	filterRev      int

	// Mouse hitboxes
	pillHitboxes []logPillButton
	searchHitbox struct{ X, Y, W, H int }
	closeHitbox  struct{ X, Y, W, H int }
	linesHitbox  struct{ X, Y, W, H int }

	// Callbacks
	OnClose      func()
	OnLineSelect func(line logviewer.LogLine)
}

// NewLogPanel creates a new LogPanel with a clean ring buffer and zero mock lines.
func NewLogPanel() *LogPanel {
	return &LogPanel{
		Open:              false,
		Dock:              LogDockBottom,
		Height:            10,
		Width:             50,
		Ring:              logviewer.NewRingBuffer(logviewer.DefaultCapacity),
		ActiveLevelFilter: logviewer.LevelUnknown,
		AutoScroll:        true,
		SelectedIndex:     -1,
		pillHitboxes:      make([]logPillButton, 0),
	}
}

// Toggle toggles the open/close state of the log panel.
func (p *LogPanel) Toggle() {
	p.Open = !p.Open
}

// SetTailer attaches an asynchronous file tailer to stream logs into this panel.
func (p *LogPanel) SetTailer(tailer *logviewer.Tailer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Tailer = tailer
}

// Clear flushes all lines from the buffer and resets scroll position.
func (p *LogPanel) Clear() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.Ring != nil {
		p.Ring.Clear()
	}
	p.cachedFiltered = nil
	p.ScrollOffset = 0
	p.SelectedIndex = -1
	p.lastRingCount = 0
	p.filterRev++
}

// AppendLog adds a log line directly to the panel's ring buffer.
func (p *LogPanel) AppendLog(raw string) {
	if p.Ring != nil {
		p.Ring.Append(raw)
		p.markDirty()
	}
}

func (p *LogPanel) markDirty() {
	p.mu.Lock()
	p.filterRev++
	p.mu.Unlock()
}

// getFilteredLines returns the filtered subset of lines from the ring buffer.
func (p *LogPanel) getFilteredLines() []logviewer.LogLine {
	p.mu.RLock()
	ringCount := 0
	if p.Ring != nil {
		ringCount = p.Ring.Len()
	}
	if p.cachedFiltered != nil && p.lastRingCount == ringCount && p.lastFilterRev == p.filterRev {
		lines := p.cachedFiltered
		p.mu.RUnlock()
		return lines
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.Ring == nil {
		p.cachedFiltered = nil
		return nil
	}

	allLines := p.Ring.All()
	opts := logviewer.NewFilterOptions(p.ActiveLevelFilter, p.SearchText, p.SearchIsRegex)
	filtered := logviewer.FilterLines(allLines, opts)

	p.cachedFiltered = filtered
	p.lastRingCount = p.Ring.Len()
	p.lastFilterRev = p.filterRev
	return filtered
}

// ScrollUp scrolls up by delta lines and pauses auto-scrolling to inspect history.
func (p *LogPanel) ScrollUp(delta int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.AutoScroll = false
	p.ScrollOffset -= delta
	if p.ScrollOffset < 0 {
		p.ScrollOffset = 0
	}
}

// ScrollDown scrolls down by delta lines. Resumes auto-scroll if bottom is reached.
func (p *LogPanel) ScrollDown(delta int, contentH int) {
	filtered := p.getFilteredLines()
	total := len(filtered)
	maxScroll := total - contentH
	if maxScroll < 0 {
		maxScroll = 0
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.ScrollOffset += delta
	if p.ScrollOffset >= maxScroll {
		p.ScrollOffset = maxScroll
		p.AutoScroll = true // Resumes auto-scrolling at bottom
	}
}

// ScrollToTop jumps to the oldest log lines.
func (p *LogPanel) ScrollToTop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.AutoScroll = false
	p.ScrollOffset = 0
}

// ScrollToBottom jumps to the newest log lines and resumes auto-scrolling.
func (p *LogPanel) ScrollToBottom(contentH int) {
	filtered := p.getFilteredLines()
	maxScroll := len(filtered) - contentH
	if maxScroll < 0 {
		maxScroll = 0
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.ScrollOffset = maxScroll
	p.AutoScroll = true
}

// HandleKey processes keyboard navigation, filter shortcuts, and search input.
func (p *LogPanel) HandleKey(k input.Key) bool {
	if !p.Open {
		return false
	}

	// 1. Search input focus mode
	if p.SearchFocused {
		switch k.Type {
		case input.KeyEsc:
			p.SearchFocused = false
			return true
		case input.KeyEnter:
			p.SearchFocused = false
			p.markDirty()
			return true
		case input.KeyBackspace:
			runes := []rune(p.SearchText)
			if p.SearchCursor > 0 && len(runes) > 0 {
				newRunes := append(runes[:p.SearchCursor-1], runes[p.SearchCursor:]...)
				p.SearchText = string(newRunes)
				p.SearchCursor--
				p.markDirty()
			}
			return true
		case input.KeyDelete:
			runes := []rune(p.SearchText)
			if p.SearchCursor < len(runes) {
				newRunes := append(runes[:p.SearchCursor], runes[p.SearchCursor+1:]...)
				p.SearchText = string(newRunes)
				p.markDirty()
			}
			return true
		case input.KeyLeft:
			if p.SearchCursor > 0 {
				p.SearchCursor--
			}
			return true
		case input.KeyRight:
			if p.SearchCursor < len([]rune(p.SearchText)) {
				p.SearchCursor++
			}
			return true
		default:
			if k.Rune >= 32 {
				runes := []rune(p.SearchText)
				newRunes := make([]rune, len(runes)+1)
				copy(newRunes, runes[:p.SearchCursor])
				newRunes[p.SearchCursor] = k.Rune
				copy(newRunes[p.SearchCursor+1:], runes[p.SearchCursor:])
				p.SearchText = string(newRunes)
				p.SearchCursor++
				p.markDirty()
				return true
			}
		}
		return false
	}

	// 2. Navigation & Shortcuts when search is not focused
	contentH := p.Height - 2
	if contentH < 1 {
		contentH = 6
	}

	switch k.Type {
	case input.KeyEsc:
		p.Open = false
		if p.OnClose != nil {
			p.OnClose()
		}
		return true
	case input.KeyUp:
		p.ScrollUp(1)
		return true
	case input.KeyDown:
		p.ScrollDown(1, contentH)
		return true
	case input.KeyPgUp:
		p.ScrollUp(contentH)
		return true
	case input.KeyPgDown:
		p.ScrollDown(contentH, contentH)
		return true
	case input.KeyHome:
		p.ScrollToTop()
		return true
	case input.KeyEnd:
		p.ScrollToBottom(contentH)
		return true
	}

	// Character shortcuts
	switch k.Rune {
	case '/', 'f', 'F':
		p.SearchFocused = true
		p.SearchCursor = len([]rune(p.SearchText))
		return true
	case 't', 'T':
		p.AutoScroll = !p.AutoScroll
		if p.AutoScroll {
			p.ScrollToBottom(contentH)
		}
		return true
	case 'c', 'C':
		p.Clear()
		return true
	case 'a', 'A':
		p.ActiveLevelFilter = logviewer.LevelUnknown
		p.markDirty()
		return true
	case 'e', 'E':
		p.ActiveLevelFilter = logviewer.LevelError
		p.markDirty()
		return true
	case 'w', 'W':
		p.ActiveLevelFilter = logviewer.LevelWarn
		p.markDirty()
		return true
	case 'i', 'I':
		p.ActiveLevelFilter = logviewer.LevelInfo
		p.markDirty()
		return true
	}

	return false
}

// HandleMouse handles mouse clicks and wheel scrolling for the log panel.
func (p *LogPanel) HandleMouse(ev input.Mouse, screenX, screenY, w, h int) bool {
	if !p.Open {
		return false
	}

	// Check if mouse event is within panel bounds
	if ev.X < screenX || ev.X >= screenX+w || ev.Y < screenY || ev.Y >= screenY+h {
		return false
	}

	contentH := h - 2
	if contentH < 1 {
		contentH = 1
	}

	// 1. Mouse wheel scrolling
	if ev.Button == input.MouseWheelUp {
		p.ScrollUp(3)
		return true
	}
	if ev.Button == input.MouseWheelDown {
		p.ScrollDown(3, contentH)
		return true
	}

	// 2. Left click handling
	if ev.Button == input.MouseLeft && ev.Action == input.MousePress {
		// Close button
		if p.closeHitbox.W > 0 &&
			ev.X >= p.closeHitbox.X && ev.X < p.closeHitbox.X+p.closeHitbox.W &&
			ev.Y >= p.closeHitbox.Y && ev.Y < p.closeHitbox.Y+p.closeHitbox.H {
			p.Open = false
			if p.OnClose != nil {
				p.OnClose()
			}
			return true
		}

		// Pill buttons
		for _, pill := range p.pillHitboxes {
			if ev.X >= pill.X && ev.X < pill.X+pill.Width && ev.Y == pill.Y {
				switch pill.ID {
				case "all":
					p.ActiveLevelFilter = logviewer.LevelUnknown
					p.markDirty()
				case "error":
					p.ActiveLevelFilter = logviewer.LevelError
					p.markDirty()
				case "warn":
					p.ActiveLevelFilter = logviewer.LevelWarn
					p.markDirty()
				case "info":
					p.ActiveLevelFilter = logviewer.LevelInfo
					p.markDirty()
				case "tail":
					p.AutoScroll = !p.AutoScroll
					if p.AutoScroll {
						p.ScrollToBottom(contentH)
					}
				case "clear":
					p.Clear()
				case "regex":
					p.SearchIsRegex = !p.SearchIsRegex
					p.markDirty()
				}
				p.SearchFocused = false
				return true
			}
		}

		// Search input box
		if p.searchHitbox.W > 0 &&
			ev.X >= p.searchHitbox.X && ev.X < p.searchHitbox.X+p.searchHitbox.W &&
			ev.Y >= p.searchHitbox.Y && ev.Y < p.searchHitbox.Y+p.searchHitbox.H {
			p.SearchFocused = true
			p.SearchCursor = len([]rune(p.SearchText))
			return true
		}

		// Log lines click
		if p.linesHitbox.H > 0 &&
			ev.X >= p.linesHitbox.X && ev.X < p.linesHitbox.X+p.linesHitbox.W &&
			ev.Y >= p.linesHitbox.Y && ev.Y < p.linesHitbox.Y+p.linesHitbox.H {
			p.SearchFocused = false
			clickedRow := ev.Y - p.linesHitbox.Y
			lineIdx := p.ScrollOffset + clickedRow
			filtered := p.getFilteredLines()
			if lineIdx >= 0 && lineIdx < len(filtered) {
				p.SelectedIndex = lineIdx
				if p.OnLineSelect != nil {
					p.OnLineSelect(filtered[lineIdx])
				}
			}
			return true
		}
	}

	return true
}

// Render draws the LogPanel tool window at screen coordinates (x, y, w, h).
func (p *LogPanel) Render(buf *buffer.Buffer, x, y, w, h int, theme ui.Theme) {
	if !p.Open || h < 3 || w < 20 {
		return
	}

	p.pillHitboxes = p.pillHitboxes[:0]

	bg := cell.Color{Type: cell.ColorRGB, Value: theme.StatusBarBg}
	panelBg := cell.Color{Type: cell.ColorRGB, Value: theme.Background}
	borderFg := cell.Color{Type: cell.ColorRGB, Value: theme.BorderColor}
	textFg := cell.Color{Type: cell.ColorRGB, Value: theme.Foreground}
	dimFg := cell.Color{Type: cell.ColorRGB, Value: theme.Comment}
	selBg := cell.Color{Type: cell.ColorRGB, Value: theme.PopupSelBg}
	highlightBg := cell.Color{Type: cell.ColorRGB, Value: theme.OccurrenceBg}

	errColor := cell.Color{Type: cell.ColorRGB, Value: theme.DiagnosticError}
	warnColor := cell.Color{Type: cell.ColorRGB, Value: theme.DiagnosticWarn}
	infoColor := cell.Color{Type: cell.ColorRGB, Value: theme.DiagnosticInfo}
	debugColor := cell.Color{Type: cell.ColorRGB, Value: theme.Comment}
	accentColor := cell.Color{Type: cell.ColorRGB, Value: theme.Function}

	// 1. Draw top border and header
	for col := 0; col < w; col++ {
		buf.SetRune(x+col, y, '─', borderFg, bg, cell.AttrNone)
	}
	buf.SetRune(x, y, '┌', borderFg, bg, cell.AttrNone)

	// Close button at top right
	closeLabel := " ✕ "
	closeX := x + w - len(closeLabel)
	for i, r := range []rune(closeLabel) {
		buf.SetRune(closeX+i, y, r, errColor, bg, cell.AttrBold)
	}
	p.closeHitbox = struct{ X, Y, W, H int }{X: closeX, Y: y, W: len(closeLabel), H: 1}

	// 2. Filter bar (row y+1)
	filterBarY := y + 1
	for col := 0; col < w; col++ {
		buf.SetRune(x+col, filterBarY, ' ', textFg, bg, cell.AttrNone)
	}

	curX := x + 1

	// Title
	title := " LOGS "
	for _, r := range []rune(title) {
		if curX < x+w-1 {
			buf.SetRune(curX, filterBarY, r, textFg, bg, cell.AttrBold)
			curX++
		}
	}
	curX++

	// Clean pill buttons without brackets!
	// Format:  All ,  Error ,  Warn ,  Info ,  Tail ,  Clear
	type pillDef struct {
		id     string
		label  string
		active bool
		actFg  cell.Color
		actBg  cell.Color
	}

	pills := []pillDef{
		{id: "all", label: " All ", active: p.ActiveLevelFilter == logviewer.LevelUnknown, actFg: textFg, actBg: selBg},
		{id: "error", label: " Error ", active: p.ActiveLevelFilter == logviewer.LevelError, actFg: panelBg, actBg: errColor},
		{id: "warn", label: " Warn ", active: p.ActiveLevelFilter == logviewer.LevelWarn, actFg: panelBg, actBg: warnColor},
		{id: "info", label: " Info ", active: p.ActiveLevelFilter == logviewer.LevelInfo, actFg: panelBg, actBg: infoColor},
		{id: "tail", label: " Tail ", active: p.AutoScroll, actFg: panelBg, actBg: accentColor},
		{id: "clear", label: " Clear ", active: false, actFg: textFg, actBg: selBg},
	}

	for _, pdef := range pills {
		lblRunes := []rune(pdef.label)
		lblLen := len(lblRunes)

		if curX+lblLen >= x+w-25 && curX > x+35 {
			break
		}

		pfg := dimFg
		pbg := bg
		attr := cell.AttrNone
		if pdef.active {
			pfg = pdef.actFg
			pbg = pdef.actBg
			attr = cell.AttrBold
		}

		p.pillHitboxes = append(p.pillHitboxes, logPillButton{
			ID:     pdef.id,
			Label:  pdef.label,
			Active: pdef.active,
			X:      curX,
			Y:      filterBarY,
			Width:  lblLen,
		})

		for _, r := range lblRunes {
			if curX < x+w-1 {
				buf.SetRune(curX, filterBarY, r, pfg, pbg, attr)
				curX++
			}
		}
		curX++ // spacing between pills
	}

	// Search input box on filter bar
	if curX < x+w-15 {
		searchLabel := "Search: "
		for _, r := range []rune(searchLabel) {
			if curX < x+w-1 {
				buf.SetRune(curX, filterBarY, r, dimFg, bg, cell.AttrNone)
				curX++
			}
		}

		searchBoxStartX := curX
		searchBoxW := x + w - curX - 10
		if searchBoxW > 35 {
			searchBoxW = 35
		}
		if searchBoxW < 8 {
			searchBoxW = 8
		}

		searchBg := panelBg
		if p.SearchFocused {
			searchBg = selBg
		}

		for c := 0; c < searchBoxW; c++ {
			if curX+c < x+w-1 {
				buf.SetRune(curX+c, filterBarY, ' ', textFg, searchBg, cell.AttrNone)
			}
		}

		displayQuery := p.SearchText
		if displayQuery == "" && !p.SearchFocused {
			displayQuery = "type to search"
		}
		queryRunes := []rune(displayQuery)
		qFg := textFg
		if p.SearchText == "" {
			qFg = dimFg
		}

		for i, r := range queryRunes {
			if i < searchBoxW-1 && curX+i < x+w-1 {
				buf.SetRune(curX+i, filterBarY, r, qFg, searchBg, cell.AttrNone)
			}
		}

		// Cursor
		if p.SearchFocused && p.SearchCursor >= 0 && p.SearchCursor < searchBoxW {
			cursorRune := ' '
			if p.SearchCursor < len(queryRunes) && p.SearchText != "" {
				cursorRune = queryRunes[p.SearchCursor]
			}
			buf.SetRune(curX+p.SearchCursor, filterBarY, cursorRune, bg, textFg, cell.AttrBold)
		}

		p.searchHitbox = struct{ X, Y, W, H int }{X: searchBoxStartX, Y: filterBarY, W: searchBoxW, H: 1}
		curX += searchBoxW + 1

		// RegEx pill
		regexLabel := " RegEx "
		regexRunes := []rune(regexLabel)
		regFg := dimFg
		regBg := bg
		regAttr := cell.AttrNone
		if p.SearchIsRegex {
			regFg = panelBg
			regBg = accentColor
			regAttr = cell.AttrBold
		}
		if curX+len(regexRunes) < x+w-1 {
			p.pillHitboxes = append(p.pillHitboxes, logPillButton{
				ID:     "regex",
				Label:  regexLabel,
				Active: p.SearchIsRegex,
				X:      curX,
				Y:      filterBarY,
				Width:  len(regexRunes),
			})
			for _, r := range regexRunes {
				buf.SetRune(curX, filterBarY, r, regFg, regBg, regAttr)
				curX++
			}
		}
	}

	// 3. Log lines viewport
	contentTop := y + 2
	contentH := h - 2
	p.linesHitbox = struct{ X, Y, W, H int }{X: x, Y: contentTop, W: w, H: contentH}

	filtered := p.getFilteredLines()
	total := len(filtered)

	// Auto-scroll clamping
	if p.AutoScroll {
		p.ScrollOffset = total - contentH
		if p.ScrollOffset < 0 {
			p.ScrollOffset = 0
		}
	} else {
		maxScroll := total - contentH
		if maxScroll < 0 {
			maxScroll = 0
		}
		if p.ScrollOffset > maxScroll {
			p.ScrollOffset = maxScroll
		}
		if p.ScrollOffset < 0 {
			p.ScrollOffset = 0
		}
	}

	for row := 0; row < contentH; row++ {
		screenY := contentTop + row
		idx := p.ScrollOffset + row

		// Clear row
		for col := 0; col < w; col++ {
			buf.SetRune(x+col, screenY, ' ', textFg, panelBg, cell.AttrNone)
		}

		if idx >= total {
			if total == 0 {
				emptyLines := []string{
					i18n.T("log.empty_title"),
					"───────────────────────────────",
					i18n.T("log.tail_waiting"),
					"",
					i18n.T("log.level_filters"),
					i18n.T("log.filter_all"),
					i18n.T("log.filter_severity"),
					i18n.T("log.filter_search"),
					i18n.T("log.filter_clear"),
				}
				if p.SearchText != "" || p.ActiveLevelFilter != logviewer.LevelUnknown {
					filterName := "All"
					switch p.ActiveLevelFilter {
					case logviewer.LevelError:
						filterName = "Error"
					case logviewer.LevelWarn:
						filterName = "Warn"
					case logviewer.LevelInfo:
						filterName = "Info"
					}
					emptyLines = []string{
						i18n.T("log.no_matches_title"),
						"───────────────────────────────",
						i18n.T("log.no_matches"),
						fmt.Sprintf(i18n.T("log.filter_level_fmt"), filterName),
						fmt.Sprintf(i18n.T("log.filter_search_fmt"), p.SearchText),
						"",
						i18n.T("log.reset_hint"),
					}
				}
				if row < len(emptyLines) {
					el := emptyLines[row]
					c := dimFg
					attr := cell.AttrNone
					if row == 0 {
						c = warnColor
						attr = cell.AttrBold
					} else if el == i18n.T("log.level_filters") || el == i18n.T("log.no_matches") {
						c = accentColor
					} else if strings.HasPrefix(el, "•") {
						c = textFg
					}
					for ci, cr := range []rune(el) {
						if x+2+ci < x+w-2 {
							buf.SetRune(x+2+ci, screenY, cr, c, panelBg, attr)
						}
					}
				}
			}
			continue
		}

		line := filtered[idx]
		isSel := idx == p.SelectedIndex
		rowBg := panelBg
		if isSel {
			rowBg = selBg
		}

		for col := 0; col < w; col++ {
			buf.SetRune(x+col, screenY, ' ', textFg, rowBg, cell.AttrNone)
		}

		lineCol := x + 1

		// Severity badge: ERROR red, WARN yellow, INFO cyan, DEBUG dim
		badge := line.Level.Badge()
		badgeFg := dimFg
		badgeBg := rowBg
		badgeAttr := cell.AttrNone

		switch line.Level {
		case logviewer.LevelFatal:
			badgeFg = errColor
			badgeAttr = cell.AttrBold
		case logviewer.LevelError:
			badgeFg = errColor
			badgeAttr = cell.AttrBold
		case logviewer.LevelWarn:
			badgeFg = warnColor
			badgeAttr = cell.AttrBold
		case logviewer.LevelInfo:
			badgeFg = infoColor
		case logviewer.LevelDebug:
			badgeFg = debugColor
		}

		for _, r := range []rune(badge) {
			if lineCol < x+w-1 {
				buf.SetRune(lineCol, screenY, r, badgeFg, badgeBg, badgeAttr)
				lineCol++
			}
		}
		lineCol++ // gap after badge

		// Timestamp (if available)
		if !line.Timestamp.IsZero() {
			tsStr := line.Timestamp.Format("15:04:05.000")
			for _, r := range []rune(tsStr) {
				if lineCol < x+w-1 {
					buf.SetRune(lineCol, screenY, r, dimFg, rowBg, cell.AttrNone)
					lineCol++
				}
			}
			lineCol += 2 // gap after timestamp
		}

		// Message text
		msg := line.Message
		if msg == "" {
			msg = line.Raw
		}
		msgRunes := []rune(msg)

		// Search highlighting
		highlightIndices := make(map[int]bool)
		if p.SearchText != "" {
			searchLower := strings.ToLower(p.SearchText)
			msgLower := strings.ToLower(msg)
			startIdx := 0
			for {
				pos := strings.Index(msgLower[startIdx:], searchLower)
				if pos == -1 {
					break
				}
				actualPos := startIdx + pos
				for k := 0; k < len(searchLower); k++ {
					highlightIndices[actualPos+k] = true
				}
				startIdx = actualPos + len(searchLower)
			}
		}

		for mi, r := range msgRunes {
			if lineCol >= x+w-1 {
				break
			}
			cellFg := textFg
			cellBg := rowBg
			cellAttr := cell.AttrNone
			if highlightIndices[mi] {
				cellBg = highlightBg
				cellAttr = cell.AttrBold
			}
			buf.SetRune(lineCol, screenY, r, cellFg, cellBg, cellAttr)
			lineCol++
		}
	}
}

// StatusSummary returns a concise status string showing buffer size and filter state.
func (p *LogPanel) StatusSummary() string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	total := 0
	if p.Ring != nil {
		total = p.Ring.Len()
	}
	filtered := len(p.cachedFiltered)

	tailStatus := "PAUSED"
	if p.AutoScroll {
		tailStatus = "TAIL"
	}

	filterName := "ALL"
	switch p.ActiveLevelFilter {
	case logviewer.LevelError:
		filterName = "ERROR"
	case logviewer.LevelWarn:
		filterName = "WARN"
	case logviewer.LevelInfo:
		filterName = "INFO"
	}

	return fmt.Sprintf("%d/%d lines | %s | %s", filtered, total, filterName, tailStatus)
}
