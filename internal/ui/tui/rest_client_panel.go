package tui

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"tahr/internal/core/clipboard"
	"tahr/internal/core/i18n"
	"tahr/internal/core/restclient"
	"tahr/internal/ui"
)

// RESTClientTab defines the visible inspector tab.
type RESTClientTab int

const (
	RESTTabBody    RESTClientTab = 0
	RESTTabHeaders RESTClientTab = 1
	RESTTabRequest RESTClientTab = 2
)

// RESTActionHit tracks screen regions for interactive mouse clicks.
type RESTActionHit struct {
	Action string // "tab_body", "tab_headers", "tab_request", "copy", "clear", "close"
	X, Y   int
	W      int
}

// RESTClientPanel provides a clean, professional HTTP response inspector for split panes or tool drawers.
type RESTClientPanel struct {
	Open          bool
	Height        int // Used when rendered in bottom drawer mode
	ActiveTab     RESTClientTab
	Response      *restclient.Response
	ScrollY       int
	ScrollX       int
	Theme         *ui.Theme

	// Clickable regions tracked during render
	ActionHits []RESTActionHit

	// Event callbacks
	OnCopy  func(text string)
	OnClear func()
	OnClose func()
}

// NewRESTClientPanel initializes an empty REST Client inspector panel.
func NewRESTClientPanel(theme *ui.Theme) *RESTClientPanel {
	return &RESTClientPanel{
		Open:       false,
		Height:     14,
		ActiveTab:  RESTTabBody,
		Response:   nil,
		Theme:      theme,
		ActionHits: make([]RESTActionHit, 0, 16),
	}
}

// SetResponse mounts a new HTTP execution response and resets scroll offsets.
func (p *RESTClientPanel) SetResponse(resp *restclient.Response) {
	p.Response = resp
	p.ScrollY = 0
	p.ScrollX = 0
}

// Clear clears the current response.
func (p *RESTClientPanel) Clear() {
	p.Response = nil
	p.ScrollY = 0
	p.ScrollX = 0
	if p.OnClear != nil {
		p.OnClear()
	}
}

// Toggle toggles the drawer visibility.
func (p *RESTClientPanel) Toggle() {
	p.Open = !p.Open
}

// HandleKey handles arrow key navigation, page scrolling, and tab switching.
func (p *RESTClientPanel) HandleKey(k input.Key) bool {
	switch k.Type {
	case input.KeyEsc:
		p.Open = false
		if p.OnClose != nil {
			p.OnClose()
		}
		return true

	case input.KeyTab:
		p.ActiveTab = (p.ActiveTab + 1) % 3
		p.ScrollY = 0
		return true

	case input.KeyBacktab:
		p.ActiveTab = (p.ActiveTab + 2) % 3
		p.ScrollY = 0
		return true

	case input.KeyUp:
		if p.ScrollY > 0 {
			p.ScrollY--
		}
		return true

	case input.KeyDown:
		maxScroll := p.maxScrollLines()
		if p.ScrollY < maxScroll {
			p.ScrollY++
		}
		return true

	case input.KeyPgUp:
		p.ScrollY = max(0, p.ScrollY-10)
		return true

	case input.KeyPgDown:
		maxScroll := p.maxScrollLines()
		p.ScrollY = min(maxScroll, p.ScrollY+10)
		return true

	case input.KeyHome:
		p.ScrollY = 0
		return true

	case input.KeyEnd:
		p.ScrollY = p.maxScrollLines()
		return true
	}

	return false
}

// HandleMouse processes mouse clicks and wheel scrolling.
func (p *RESTClientPanel) HandleMouse(ev input.Mouse, screenW, screenH int) bool {
	// 1. Mouse wheel scrolling
	if ev.Button == input.MouseWheelUp {
		if p.ScrollY > 0 {
			p.ScrollY = max(0, p.ScrollY-3)
		}
		return true
	}
	if ev.Button == input.MouseWheelDown {
		maxScroll := p.maxScrollLines()
		if p.ScrollY < maxScroll {
			p.ScrollY = min(maxScroll, p.ScrollY+3)
		}
		return true
	}

	// 2. Click actions
	if ev.Button == input.MouseLeft {
		return p.HandleClick(ev.X, ev.Y)
	}

	return false
}

// HandleClick checks registered action hits and triggers corresponding actions.
func (p *RESTClientPanel) HandleClick(x, y int) bool {
	for _, hit := range p.ActionHits {
		if y == hit.Y && x >= hit.X && x < hit.X+hit.W {
			switch hit.Action {
			case "tab_body":
				p.ActiveTab = RESTTabBody
				p.ScrollY = 0
				return true
			case "tab_headers":
				p.ActiveTab = RESTTabHeaders
				p.ScrollY = 0
				return true
			case "tab_request":
				p.ActiveTab = RESTTabRequest
				p.ScrollY = 0
				return true
			case "copy":
				p.copyActiveContent()
				return true
			case "clear":
				p.Clear()
				return true
			case "close":
				p.Open = false
				if p.OnClose != nil {
					p.OnClose()
				}
				return true
			}
		}
	}
	return false
}

// copyActiveContent copies current tab text to system clipboard.
func (p *RESTClientPanel) copyActiveContent() {
	if p.Response == nil {
		return
	}
	var text string
	switch p.ActiveTab {
	case RESTTabBody:
		if p.Response.PrettyBody != "" {
			text = p.Response.PrettyBody
		} else {
			text = p.Response.BodyString
		}
	case RESTTabHeaders:
		var sb strings.Builder
		for k, v := range p.Response.Headers {
			sb.WriteString(fmt.Sprintf("%s: %s\n", k, strings.Join(v, ", ")))
		}
		text = sb.String()
	case RESTTabRequest:
		text = p.Response.RawRequest
	}

	if text != "" {
		_ = clipboard.Write(text)
		if p.OnCopy != nil {
			p.OnCopy(text)
		}
	}
}

// maxScrollLines returns the maximum scroll index based on current tab content.
func (p *RESTClientPanel) maxScrollLines() int {
	if p.Response == nil {
		return 0
	}
	linesCount := 0
	switch p.ActiveTab {
	case RESTTabBody:
		body := p.Response.PrettyBody
		if body == "" {
			body = p.Response.BodyString
		}
		linesCount = len(strings.Split(body, "\n"))
	case RESTTabHeaders:
		linesCount = len(p.Response.Headers)
	case RESTTabRequest:
		linesCount = len(strings.Split(p.Response.RawRequest, "\n"))
	}
	return max(0, linesCount-1)
}

// Render renders the panel inside the specified bounds (split pane mode).
func (p *RESTClientPanel) Render(buf *buffer.Buffer, bounds buffer.Rect) {
	p.RenderAt(buf, bounds.X, bounds.Y, bounds.Width, bounds.Height, p.Theme)
}

// RenderAt renders the REST Client panel at the given rectangle.
func (p *RESTClientPanel) RenderAt(buf *buffer.Buffer, startX, startY, width, height int, theme *ui.Theme) {
	if width < 15 || height < 3 {
		return
	}
	p.ActionHits = p.ActionHits[:0]

	bg := toColor(theme.Background)
	fg := toColor(theme.Foreground)
	borderFg := toColor(theme.BorderColor)
	gutterBg := toColor(theme.GutterBg)
	selBg := toColor(theme.PopupSelBg)
	commentFg := toColor(theme.Comment)

	// Fill background
	for y := startY; y < startY+height; y++ {
		for x := startX; x < startX+width; x++ {
			buf.SetRune(x, y, ' ', fg, bg, cell.AttrNone)
		}
	}

	// 1. Top Status Bar (Row startY)
	for x := startX; x < startX+width; x++ {
		buf.SetRune(x, startY, ' ', fg, gutterBg, cell.AttrNone)
	}

	curX := startX + 1

	// Title
	title := "REST Client"
	for _, r := range []rune(title) {
		if curX < startX+width-1 {
			buf.SetRune(curX, startY, r, toColor(theme.Function), gutterBg, cell.AttrBold)
			curX++
		}
	}
	curX += 2

	// Status Pill (clean badge without square brackets)
	var pillText string
	var pillFg, pillBg cell.Color

	if p.Response == nil {
		pillText = " IDLE "
		pillFg = commentFg
		pillBg = toColor(theme.SelectionBg)
	} else if p.Response.IsSuccess() {
		pillText = fmt.Sprintf(" %s ", p.Response.StatusPill())
		pillBg = cell.Color{Type: cell.ColorRGB, Value: 0x1A4731} // Dark solid green
		pillFg = cell.Color{Type: cell.ColorRGB, Value: 0x7EE787} // Light green
	} else if p.Response.IsClientError() {
		pillText = fmt.Sprintf(" %s ", p.Response.StatusPill())
		pillBg = cell.Color{Type: cell.ColorRGB, Value: 0x4D3800} // Dark amber
		pillFg = cell.Color{Type: cell.ColorRGB, Value: 0xE3B341} // Warm yellow
	} else {
		pillText = fmt.Sprintf(" %s ", p.Response.StatusPill())
		pillBg = cell.Color{Type: cell.ColorRGB, Value: 0x4D1B22} // Dark red
		pillFg = cell.Color{Type: cell.ColorRGB, Value: 0xF85149} // Bright red
	}

	for _, r := range []rune(pillText) {
		if curX < startX+width-1 {
			buf.SetRune(curX, startY, r, pillFg, pillBg, cell.AttrBold)
			curX++
		}
	}
	curX += 2

	// Duration and Size Badges (clean badges without square brackets)
	if p.Response != nil {
		durBadge := fmt.Sprintf(" %s ", p.Response.FormatDuration())
		durBg := toColor(theme.SelectionBg)
		for _, r := range []rune(durBadge) {
			if curX < startX+width-1 {
				buf.SetRune(curX, startY, r, fg, durBg, cell.AttrNone)
				curX++
			}
		}
		curX += 2

		sizeBadge := fmt.Sprintf(" %s ", p.Response.FormatSize())
		sizeBg := toColor(theme.SelectionBg)
		for _, r := range []rune(sizeBadge) {
			if curX < startX+width-1 {
				buf.SetRune(curX, startY, r, fg, sizeBg, cell.AttrNone)
				curX++
			}
		}
		curX += 2
	}

	// Action Badges on Top Right (Copy, Clear, Close) - strictly NO brackets
	btnCopy := " Copy "
	btnClear := " Clear "
	btnClose := " ✕ "
	rightW := len([]rune(btnCopy)) + 1 + len([]rune(btnClear)) + 1 + len([]rune(btnClose)) + 1
	btnStartX := startX + width - rightW
	if btnStartX > curX {
		bx := btnStartX

		// Copy Button
		p.ActionHits = append(p.ActionHits, RESTActionHit{Action: "copy", X: bx, Y: startY, W: len([]rune(btnCopy))})
		for _, r := range []rune(btnCopy) {
			buf.SetRune(bx, startY, r, fg, selBg, cell.AttrNone)
			bx++
		}
		bx++

		// Clear Button
		p.ActionHits = append(p.ActionHits, RESTActionHit{Action: "clear", X: bx, Y: startY, W: len([]rune(btnClear))})
		for _, r := range []rune(btnClear) {
			buf.SetRune(bx, startY, r, fg, selBg, cell.AttrNone)
			bx++
		}
		bx++

		// Close Button
		p.ActionHits = append(p.ActionHits, RESTActionHit{Action: "close", X: bx, Y: startY, W: len([]rune(btnClose))})
		for _, r := range []rune(btnClose) {
			buf.SetRune(bx, startY, r, toColor(theme.DiagnosticError), selBg, cell.AttrBold)
			bx++
		}
	}

	// 2. Tabs Bar (Row startY + 1)
	tabBarY := startY + 1
	for x := startX; x < startX+width; x++ {
		buf.SetRune(x, tabBarY, '─', borderFg, bg, cell.AttrNone)
	}

	tabX := startX + 1
	tabs := []struct {
		tab   RESTClientTab
		label string
		act   string
	}{
		{RESTTabBody, " Body ", "tab_body"},
		{RESTTabHeaders, " Headers ", "tab_headers"},
		{RESTTabRequest, " Request ", "tab_request"},
	}

	for _, t := range tabs {
		isSel := p.ActiveTab == t.tab
		tFg := commentFg
		tBg := bg
		tAttr := cell.AttrNone

		if isSel {
			tFg = toColor(theme.Function)
			tBg = selBg
			tAttr = cell.AttrBold
		}

		p.ActionHits = append(p.ActionHits, RESTActionHit{
			Action: t.act,
			X:      tabX,
			Y:      tabBarY,
			W:      len([]rune(t.label)),
		})

		for _, r := range []rune(t.label) {
			if tabX < startX+width-1 {
				buf.SetRune(tabX, tabBarY, r, tFg, tBg, tAttr)
				tabX++
			}
		}
		tabX += 1
	}

	// 3. Tab Content Area
	contentY := startY + 2
	contentH := height - 3 // leave 1 row at bottom for navigation hints
	if contentH <= 0 {
		return
	}

	switch p.ActiveTab {
	case RESTTabBody:
		p.renderBodyTab(buf, startX, contentY, width, contentH, theme)
	case RESTTabHeaders:
		p.renderHeadersTab(buf, startX, contentY, width, contentH, theme)
	case RESTTabRequest:
		p.renderRequestTab(buf, startX, contentY, width, contentH, theme)
	}

	// 4. Footer Guide Bar (Row startY + height - 1)
	footY := startY + height - 1
	for x := startX; x < startX+width; x++ {
		buf.SetRune(x, footY, ' ', fg, gutterBg, cell.AttrNone)
	}
	navGuide := " ↑↓ Scroll │ Tab Switch View │ Click Badges to Copy/Clear"
	for i, r := range []rune(navGuide) {
		if startX+i < startX+width-1 {
			buf.SetRune(startX+i, footY, r, commentFg, gutterBg, cell.AttrNone)
		}
	}
}

// renderBodyTab renders the auto-formatted response body with syntax coloring.
func (p *RESTClientPanel) renderBodyTab(buf *buffer.Buffer, startX, startY, width, height int, theme *ui.Theme) {
	bg := toColor(theme.Background)
	fg := toColor(theme.Foreground)
	commentFg := toColor(theme.Comment)
	lineNumFg := toColor(theme.LineNumber)

	if p.Response == nil {
		p.drawEmptyPlaceholder(buf, startX, startY, width, height, "No active HTTP response. Execute a request from a .http or .rest file.", commentFg, bg)
		return
	}

	bodyText := p.Response.PrettyBody
	if bodyText == "" {
		bodyText = p.Response.BodyString
	}
	if bodyText == "" {
		p.drawEmptyPlaceholder(buf, startX, startY, width, height, "(Empty response body)", commentFg, bg)
		return
	}

	lines := strings.Split(bodyText, "\n")
	totalLines := len(lines)
	isJSON := strings.Contains(strings.ToLower(p.Response.ContentType), "json") || strings.HasPrefix(strings.TrimSpace(bodyText), "{") || strings.HasPrefix(strings.TrimSpace(bodyText), "[")
	isXML := strings.Contains(strings.ToLower(p.Response.ContentType), "xml") || strings.HasPrefix(strings.TrimSpace(bodyText), "<")

	gutterW := 5
	availW := width - gutterW - 2
	if availW < 10 {
		availW = 10
	}

	for row := 0; row < height; row++ {
		lineIdx := p.ScrollY + row
		screenY := startY + row

		if lineIdx >= totalLines {
			continue
		}

		line := lines[lineIdx]

		// Draw line number gutter
		numStr := fmt.Sprintf("%4d │", lineIdx+1)
		for i, r := range []rune(numStr) {
			if startX+i < startX+gutterW+1 {
				buf.SetRune(startX+i, screenY, r, lineNumFg, bg, cell.AttrNone)
			}
		}

		// Draw syntax-colored line content
		contentX := startX + gutterW + 1
		if isJSON {
			p.drawColoredJSONLine(buf, contentX, screenY, line, availW, theme)
		} else if isXML {
			p.drawColoredXMLLine(buf, contentX, screenY, line, availW, theme)
		} else {
			// Plain text
			runes := []rune(line)
			for i := 0; i < len(runes) && i < availW; i++ {
				buf.SetRune(contentX+i, screenY, runes[i], fg, bg, cell.AttrNone)
			}
		}
	}

	// Draw scrollbar if necessary
	p.drawScrollbar(buf, startX+width-1, startY, height, totalLines, p.ScrollY, theme)
}

// renderHeadersTab renders response headers in a clean key-value table.
func (p *RESTClientPanel) renderHeadersTab(buf *buffer.Buffer, startX, startY, width, height int, theme *ui.Theme) {
	bg := toColor(theme.Background)
	fg := toColor(theme.Foreground)
	commentFg := toColor(theme.Comment)
	hdrBg := toColor(theme.GutterBg)
	hdrFg := toColor(theme.Function)
	borderFg := toColor(theme.BorderColor)
	keyFg := toColor(theme.Keyword)
	valFg := toColor(theme.Foreground)

	if p.Response == nil || len(p.Response.Headers) == 0 {
		p.drawEmptyPlaceholder(buf, startX, startY, width, height, "No response headers available.", commentFg, bg)
		return
	}

	type hPair struct {
		key string
		val string
	}
	var pairs []hPair
	for k, vList := range p.Response.Headers {
		pairs = append(pairs, hPair{key: k, val: strings.Join(vList, ", ")})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return strings.ToLower(pairs[i].key) < strings.ToLower(pairs[j].key)
	})

	keyColW := 26
	if keyColW > width/3 {
		keyColW = width / 3
	}
	if keyColW < 12 {
		keyColW = 12
	}

	// Table Header (Row 0)
	hdrY := startY
	tableHdr := fmt.Sprintf(" %-*s │ %s", keyColW-1, "Header Key", "Header Value")
	for i, r := range []rune(tableHdr) {
		if startX+i < startX+width {
			buf.SetRune(startX+i, hdrY, r, hdrFg, hdrBg, cell.AttrBold)
		}
	}
	for x := startX + len([]rune(tableHdr)); x < startX+width; x++ {
		buf.SetRune(x, hdrY, ' ', fg, hdrBg, cell.AttrNone)
	}

	// Table Divider (Row 1)
	divY := startY + 1
	if height > 1 {
		for x := startX; x < startX+width; x++ {
			buf.SetRune(x, divY, '─', borderFg, bg, cell.AttrNone)
		}
		if startX+keyColW < startX+width {
			buf.SetRune(startX+keyColW, divY, '┼', borderFg, bg, cell.AttrNone)
		}
	}

	// Table Rows
	dataStartY := startY + 2
	dataH := height - 2
	for row := 0; row < dataH; row++ {
		idx := p.ScrollY + row
		screenY := dataStartY + row

		if idx >= len(pairs) {
			continue
		}

		pair := pairs[idx]
		rBg := bg
		if idx%2 == 1 {
			rBg = toColor(theme.CursorLineBg)
		}

		// Clear row background
		for x := startX; x < startX+width; x++ {
			buf.SetRune(x, screenY, ' ', fg, rBg, cell.AttrNone)
		}

		// Key column
		keyRunes := []rune(fmt.Sprintf(" %-*s", keyColW-1, pair.key))
		for i := 0; i < len(keyRunes) && i < keyColW; i++ {
			buf.SetRune(startX+i, screenY, keyRunes[i], keyFg, rBg, cell.AttrBold)
		}

		// Divider
		if startX+keyColW < startX+width {
			buf.SetRune(startX+keyColW, screenY, '│', borderFg, rBg, cell.AttrNone)
		}

		// Value column
		valX := startX + keyColW + 2
		valRunes := []rune(pair.val)
		availValW := width - keyColW - 3
		for i := 0; i < len(valRunes) && i < availValW; i++ {
			buf.SetRune(valX+i, screenY, valRunes[i], valFg, rBg, cell.AttrNone)
		}
	}

	p.drawScrollbar(buf, startX+width-1, dataStartY, dataH, len(pairs), p.ScrollY, theme)
}

// renderRequestTab renders the raw outgoing HTTP request.
func (p *RESTClientPanel) renderRequestTab(buf *buffer.Buffer, startX, startY, width, height int, theme *ui.Theme) {
	bg := toColor(theme.Background)
	fg := toColor(theme.Foreground)
	commentFg := toColor(theme.Comment)
	lineNumFg := toColor(theme.LineNumber)
	methodFg := toColor(theme.Keyword)
	urlFg := toColor(theme.Function)
	protoFg := toColor(theme.Comment)

	if p.Response == nil || p.Response.RawRequest == "" {
		p.drawEmptyPlaceholder(buf, startX, startY, width, height, "No raw sent request available.", commentFg, bg)
		return
	}

	lines := strings.Split(p.Response.RawRequest, "\n")
	totalLines := len(lines)

	gutterW := 5
	availW := width - gutterW - 2
	if availW < 10 {
		availW = 10
	}

	for row := 0; row < height; row++ {
		lineIdx := p.ScrollY + row
		screenY := startY + row

		if lineIdx >= totalLines {
			continue
		}

		line := strings.TrimRight(lines[lineIdx], "\r")

		// Line number gutter
		numStr := fmt.Sprintf("%4d │", lineIdx+1)
		for i, r := range []rune(numStr) {
			if startX+i < startX+gutterW+1 {
				buf.SetRune(startX+i, screenY, r, lineNumFg, bg, cell.AttrNone)
			}
		}

		contentX := startX + gutterW + 1

		// First line is HTTP Request line: "METHOD URL PROTO"
		if lineIdx == 0 {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				drawX := contentX
				for _, r := range []rune(fields[0] + " ") {
					if drawX < startX+width-1 {
						buf.SetRune(drawX, screenY, r, methodFg, bg, cell.AttrBold)
						drawX++
					}
				}
				for _, r := range []rune(fields[1] + " ") {
					if drawX < startX+width-1 {
						buf.SetRune(drawX, screenY, r, urlFg, bg, cell.AttrNone)
						drawX++
					}
				}
				if len(fields) > 2 {
					for _, r := range []rune(fields[2]) {
						if drawX < startX+width-1 {
							buf.SetRune(drawX, screenY, r, protoFg, bg, cell.AttrNone)
							drawX++
						}
					}
				}
				continue
			}
		}

		// Header lines: "Header-Key: Value"
		colonIdx := strings.Index(line, ":")
		if colonIdx > 0 && !strings.HasPrefix(line, "{") && !strings.HasPrefix(line, "<") {
			hKey := line[:colonIdx]
			hVal := line[colonIdx:]
			drawX := contentX
			for _, r := range []rune(hKey) {
				if drawX < startX+width-1 {
					buf.SetRune(drawX, screenY, r, toColor(theme.Keyword), bg, cell.AttrBold)
					drawX++
				}
			}
			for _, r := range []rune(hVal) {
				if drawX < startX+width-1 {
					buf.SetRune(drawX, screenY, r, fg, bg, cell.AttrNone)
					drawX++
				}
			}
			continue
		}

		// Body or other lines
		runes := []rune(line)
		for i := 0; i < len(runes) && i < availW; i++ {
			buf.SetRune(contentX+i, screenY, runes[i], fg, bg, cell.AttrNone)
		}
	}

	p.drawScrollbar(buf, startX+width-1, startY, height, totalLines, p.ScrollY, theme)
}

// drawColoredJSONLine performs token-based syntax coloring for JSON response lines.
func (p *RESTClientPanel) drawColoredJSONLine(buf *buffer.Buffer, startX, y int, line string, maxW int, theme *ui.Theme) {
	bg := toColor(theme.Background)
	fg := toColor(theme.Foreground)
	keyFg := toColor(theme.Function)
	strFg := toColor(theme.String)
	numFg := toColor(theme.Constant)
	kwFg := toColor(theme.Keyword)

	runes := []rune(line)
	n := len(runes)
	curX := startX

	i := 0
	for i < n && (curX-startX) < maxW {
		r := runes[i]

		// Strings (quoted)
		if r == '"' {
			// Find closing quote
			j := i + 1
			for j < n {
				if runes[j] == '\\' && j+1 < n {
					j += 2
					continue
				}
				if runes[j] == '"' {
					j++
					break
				}
				j++
			}
			strToken := string(runes[i:j])

			// Check if this string is a JSON key (followed by optional spaces and colon)
			isKey := false
			k := j
			for k < n && unicode.IsSpace(runes[k]) {
				k++
			}
			if k < n && runes[k] == ':' {
				isKey = true
			}

			sColor := strFg
			if isKey {
				sColor = keyFg
			}

			for _, sr := range []rune(strToken) {
				if (curX - startX) < maxW {
					buf.SetRune(curX, y, sr, sColor, bg, cell.AttrNone)
					curX++
				}
			}
			i = j
			continue
		}

		// Numbers: -?[0-9]+(\.[0-9]+)?
		if (unicode.IsDigit(r) || r == '-') && (i == 0 || !unicode.IsLetter(runes[i-1])) {
			j := i
			for j < n && (unicode.IsDigit(runes[j]) || runes[j] == '.' || runes[j] == '-' || runes[j] == 'e' || runes[j] == 'E' || runes[j] == '+') {
				j++
			}
			for _, nr := range runes[i:j] {
				if (curX - startX) < maxW {
					buf.SetRune(curX, y, nr, numFg, bg, cell.AttrNone)
					curX++
				}
			}
			i = j
			continue
		}

		// Booleans / null keywords
		if unicode.IsLetter(r) {
			j := i
			for j < n && unicode.IsLetter(runes[j]) {
				j++
			}
			word := string(runes[i:j])
			wColor := fg
			if word == "true" || word == "false" || word == "null" {
				wColor = kwFg
			}
			for _, wr := range []rune(word) {
				if (curX - startX) < maxW {
					buf.SetRune(curX, y, wr, wColor, bg, cell.AttrNone)
					curX++
				}
			}
			i = j
			continue
		}

		// Punctuation and spaces
		buf.SetRune(curX, y, r, fg, bg, cell.AttrNone)
		curX++
		i++
	}
}

// drawColoredXMLLine performs syntax coloring for XML tags and attributes.
func (p *RESTClientPanel) drawColoredXMLLine(buf *buffer.Buffer, startX, y int, line string, maxW int, theme *ui.Theme) {
	bg := toColor(theme.Background)
	fg := toColor(theme.Foreground)
	tagFg := toColor(theme.Keyword)
	attrFg := toColor(theme.Function)
	strFg := toColor(theme.String)

	runes := []rune(line)
	n := len(runes)
	curX := startX
	inTag := false

	i := 0
	for i < n && (curX-startX) < maxW {
		r := runes[i]

		if r == '<' {
			inTag = true
			buf.SetRune(curX, y, r, tagFg, bg, cell.AttrBold)
			curX++
			i++
			continue
		}
		if r == '>' {
			inTag = false
			buf.SetRune(curX, y, r, tagFg, bg, cell.AttrBold)
			curX++
			i++
			continue
		}

		if inTag {
			if r == '"' {
				// Attribute string
				j := i + 1
				for j < n && runes[j] != '"' {
					j++
				}
				if j < n {
					j++
				}
				for _, sr := range runes[i:j] {
					if (curX - startX) < maxW {
						buf.SetRune(curX, y, sr, strFg, bg, cell.AttrNone)
						curX++
					}
				}
				i = j
				continue
			}

			// Tag name or attribute name
			if unicode.IsLetter(r) || r == '/' || r == ':' || r == '-' {
				j := i
				for j < n && (unicode.IsLetter(runes[j]) || unicode.IsDigit(runes[j]) || runes[j] == '/' || runes[j] == ':' || runes[j] == '-') {
					j++
				}
				c := tagFg
				if i > 0 && unicode.IsSpace(runes[i-1]) {
					c = attrFg
				}
				for _, tr := range runes[i:j] {
					if (curX - startX) < maxW {
						buf.SetRune(curX, y, tr, c, bg, cell.AttrNone)
						curX++
					}
				}
				i = j
				continue
			}
		}

		buf.SetRune(curX, y, r, fg, bg, cell.AttrNone)
		curX++
		i++
	}
}

// drawScrollbar draws a vertical scrollbar thumb.
func (p *RESTClientPanel) drawScrollbar(buf *buffer.Buffer, x, y, h, totalLines, scrollY int, theme *ui.Theme) {
	if totalLines <= h || h <= 0 {
		return
	}
	trackFg := toColor(theme.BorderColor)
	thumbFg := toColor(theme.Keyword)
	bg := toColor(theme.Background)

	thumbH := max(1, h*h/totalLines)
	maxScroll := max(1, totalLines-h)
	thumbY := y + (scrollY * (h - thumbH) / maxScroll)

	for row := 0; row < h; row++ {
		curY := y + row
		r := '│'
		f := trackFg
		if curY >= thumbY && curY < thumbY+thumbH {
			r = '█'
			f = thumbFg
		}
		buf.SetRune(x, curY, r, f, bg, cell.AttrNone)
	}
}

// drawEmptyPlaceholder renders an informative multi-line placeholder when no data is loaded.
func (p *RESTClientPanel) drawEmptyPlaceholder(buf *buffer.Buffer, startX, startY, width, height int, msg string, fg, bg cell.Color) {
	emptyTitle := i18n.T("rest.empty_title")
	instructions := i18n.T("rest.instructions")
	lines := []string{
		emptyTitle,
		"───────────────────────────────",
		instructions,
		i18n.T("rest.hint_open"),
		i18n.T("rest.hint_cursor"),
		i18n.T("rest.hint_send"),
		i18n.T("rest.hint_output"),
	}
	if msg != "" && !strings.Contains(msg, "No active HTTP response") {
		lines = strings.Split(msg, "\n")
	}

	for row, l := range lines {
		curY := startY + 1 + row
		if curY >= startY+height {
			break
		}
		c := fg
		attr := cell.AttrNone
		if row == 0 {
			if p.Theme != nil {
				c = toColor(p.Theme.DiagnosticWarn)
			}
			attr = cell.AttrBold
		} else if l == instructions {
			if p.Theme != nil {
				c = toColor(p.Theme.Function)
			}
		} else if strings.HasPrefix(l, "•") {
			if p.Theme != nil {
				c = toColor(p.Theme.Foreground)
			}
		}
		for i, r := range []rune(l) {
			if startX+2+i < startX+width-2 {
				buf.SetRune(startX+2+i, curY, r, c, bg, attr)
			}
		}
	}
}
