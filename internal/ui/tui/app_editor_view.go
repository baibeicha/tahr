package tui

import (
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/mattn/go-runewidth"

	"tahr/internal/core"
	corebuf "tahr/internal/core/buffer"
	"tahr/internal/core/dag"
	"tahr/internal/core/coverage"
	"tahr/internal/core/db"
	"tahr/internal/core/git"
	"tahr/internal/core/gitlens"
	"tahr/internal/core/syntax"
)

// toColor converts a 24-bit 0xRRGGBB integer into goatui cell.Color.
func toColor(val uint32) cell.Color {
	return cell.Color{
		Type:  cell.ColorRGB,
		Value: val,
	}
}

// rainbowPalette defines high-contrast cyclic colors for nested bracket pairs.
var rainbowPalette = []uint32{
	0xF6C177, // Gold / Warm Yellow
	0xC4A7E7, // Iris / Purple
	0x9CCFD8, // Foam / Cyan
	0x31748F, // Pine / Teal
	0xEB6F92, // Love / Coral
}

// calculateLineRainbowDepths computes nesting depth for brackets in lineStr.
func calculateLineRainbowDepths(lineStr string) map[int]int {
	runes := []rune(lineStr)
	depths := make(map[int]int)
	depth := 0
	for col, r := range runes {
		switch r {
		case '(', '[', '{':
			depths[col] = depth
			depth++
		case ')', ']', '}':
			depth--
			if depth < 0 {
				depth = 0
			}
			depths[col] = depth
		}
	}
	return depths
}

type minimapHitbox struct {
	paneIdx    int
	minX       int
	maxX       int
	minY       int
	maxY       int
	totalLines int
}

// stepSmoothScroll performs 120 FPS sub-line exponential damping interpolation towards target viewport.
func (m *AppModel) stepSmoothScroll() bool {
	if m.splits == nil {
		return false
	}
	needsRedraw := false
	for i := range m.splits.Panes {
		p := &m.splits.Panes[i]
		diff := p.TargetViewportY - p.SmoothScrollY
		if math.Abs(diff) > 0.05 {
			p.SmoothScrollY += diff * 0.40
			p.ViewportY = int(math.Round(p.SmoothScrollY))
			if i == m.splits.ActiveIndex {
				m.viewportY = p.ViewportY
			}
			needsRedraw = true
		} else {
			p.SmoothScrollY = p.TargetViewportY
			p.ViewportY = int(p.TargetViewportY)
			if i == m.splits.ActiveIndex {
				m.viewportY = p.ViewportY
			}
		}
	}
	return needsRedraw
}

// ensureCursorVisible calculates viewport scrolling to keep active cursor on screen.
func (m *AppModel) ensureCursorVisible() {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		return
	}

	head := sels[len(sels)-1].Head
	editorHeight := m.height - 2
	if m.outputOpen {
		editorHeight -= m.outputHeight
	}
	if editorHeight < 1 {
		editorHeight = 1
	}

	sideW := 0
	if m.sidebarOpen {
		sideW = m.sidebarWidth + 1
	}
	gutterWidth := m.gutterWidth()
	editorWidth := m.width - sideW - gutterWidth - 1
	if editorWidth < 1 {
		editorWidth = 1
	}

	if m.splits != nil && m.splits.TotalPanes() > 1 {
		if ap := m.splits.ActivePane(); ap != nil && ap.Bounds.Width > 0 && ap.Bounds.Height > 0 {
			editorHeight = ap.Bounds.Height - 1
			if editorHeight < 1 {
				editorHeight = 1
			}
			editorWidth = ap.Bounds.Width - gutterWidth - 1
			if editorWidth < 1 {
				editorWidth = 1
			}
		}
	}

	scrolloff := 0
	if m.settings != nil {
		scrolloff = m.settings.Current.ScrolloffY
	}
	if scrolloff*2 >= editorHeight {
		scrolloff = max(0, (editorHeight-1)/2)
	}

	if head.Line < m.viewportY+scrolloff {
		m.viewportY = max(0, head.Line-scrolloff)
	} else if head.Line >= m.viewportY+editorHeight-scrolloff {
		m.viewportY = head.Line - editorHeight + 1 + scrolloff
	}

	if head.Column < m.viewportX {
		m.viewportX = head.Column
	} else if head.Column >= m.viewportX+editorWidth {
		m.viewportX = head.Column - editorWidth + 1
	}

	if m.settings != nil && m.settings.Current.WordWrap {
		m.viewportX = 0
	}
	doc.Viewport.ScrolloffY = scrolloff
	doc.Viewport.Height = editorHeight
	doc.Viewport.Width = editorWidth

	if m.splits != nil {
		if ap := m.splits.ActivePane(); ap != nil {
			ap.ViewportX = m.viewportX
			ap.ViewportY = m.viewportY
			ap.TargetViewportY = float64(m.viewportY)
			ap.SmoothScrollY = float64(m.viewportY)
			ap.ScrollVelocity = 0
		}
	}
}

// syncViewportOffsets sets TargetViewportY and SmoothScrollY to match ViewportY.
func (m *AppModel) syncViewportOffsets() {
	if m.splits != nil {
		for i := range m.splits.Panes {
			p := &m.splits.Panes[i]
			p.TargetViewportY = float64(p.ViewportY)
			p.SmoothScrollY = float64(p.ViewportY)
			p.ScrollVelocity = 0
		}
		if ap := m.splits.ActivePane(); ap != nil {
			ap.TargetViewportY = float64(m.viewportY)
			ap.SmoothScrollY = float64(m.viewportY)
			ap.ScrollVelocity = 0
		}
	}
}

// performUndo performs an undo operation with user feedback via status message and toast notifications.
func (m *AppModel) performUndo() {
	if m.eng == nil {
		return
	}
	doc := m.eng.ActiveDocument()
	if doc == nil {
		m.statusMessage = "No active document"
		if m.toasts != nil {
			m.toasts.Warn("UNDO", "No active document")
		}
		return
	}
	if !doc.Buffer.Undo() {
		m.statusMessage = "Already at oldest change"
		if m.toasts != nil {
			m.toasts.Warn("UNDO", "Already at oldest change")
		}
		return
	}
	doc.Viewport.GutterWidth = core.ComputeGutterWidth(doc.Buffer.TotalLines())
	doc.Viewport.ScrollToCursor(doc.Buffer)

	m.notifyLSPChange()
	m.ensureCursorVisible()
	m.syncViewportOffsets()
	m.statusMessage = "Undo"
	if m.toasts != nil {
		m.toasts.Info("UNDO", "Undo")
	}
}

// performRedo performs a redo operation with user feedback via status message and toast notifications.
func (m *AppModel) performRedo() {
	if m.eng == nil {
		return
	}
	doc := m.eng.ActiveDocument()
	if doc == nil {
		m.statusMessage = "No active document"
		if m.toasts != nil {
			m.toasts.Warn("REDO", "No active document")
		}
		return
	}
	if !doc.Buffer.Redo() {
		m.statusMessage = "Already at newest change"
		if m.toasts != nil {
			m.toasts.Warn("REDO", "Already at newest change")
		}
		return
	}
	doc.Viewport.GutterWidth = core.ComputeGutterWidth(doc.Buffer.TotalLines())
	doc.Viewport.ScrollToCursor(doc.Buffer)

	m.notifyLSPChange()
	m.ensureCursorVisible()
	m.syncViewportOffsets()
	m.statusMessage = "Redo"
	if m.toasts != nil {
		m.toasts.Info("REDO", "Redo")
	}
}

// gutterWidth returns width required for line numbers and status glyphs.
func (m *AppModel) gutterWidth() int {
	if m.settings != nil && strings.ToLower(m.settings.Current.LineNumbers) == "none" {
		return 3
	}
	doc := m.eng.ActiveDocument()
	total := 1
	if doc != nil {
		total = doc.Buffer.TotalLines()
	}
	w := len(fmt.Sprintf("%d", total)) + 3
	if w < 5 {
		return 5
	}
	return w
}

// expandDiagnosticRange widens single-column diagnostic points to cover the full identifier/token.
func expandDiagnosticRange(visChars []visChar, startCol, endCol int) (int, int) {
	if endCol-startCol > 1 {
		return startCol, endCol
	}
	targetIdx := -1
	for i, vc := range visChars {
		if vc.origCol == startCol {
			targetIdx = i
			break
		}
	}
	if targetIdx == -1 {
		if endCol <= startCol {
			return startCol, startCol + 1
		}
		return startCol, endCol
	}
	isWordChar := func(r rune) bool {
		return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
	}
	if !isWordChar(visChars[targetIdx].r) {
		if endCol <= startCol {
			return startCol, startCol + 1
		}
		return startCol, endCol
	}
	left := targetIdx
	for left > 0 && isWordChar(visChars[left-1].r) {
		left--
	}
	right := targetIdx
	for right < len(visChars) && isWordChar(visChars[right].r) {
		right++
	}
	if right > left {
		startOrig := visChars[left].origCol
		endOrig := visChars[right-1].origCol + 1
		return startOrig, endOrig
	}
	if endCol <= startCol {
		return startCol, startCol + 1
	}
	return startCol, endCol
}

func (m *AppModel) getOrCreateImageViewer(filePath string) *ImageViewerState {
	if m.imageStateCache == nil {
		m.imageStateCache = make(map[string]*ImageViewerState)
	}
	iv, ok := m.imageStateCache[filePath]
	if !ok {
		iv = NewImageViewerState(filePath)
		m.imageStateCache[filePath] = iv
	}
	return iv
}

// renderPane renders a single split pane (header, gutter, code lines, syntax spans, selection, cursor, and scrollbar).
func (m *AppModel) renderPane(buf *buffer.Buffer, pane *SplitPane, doc *core.Document, isActivePane bool, isMultiPane bool) {
	if pane == nil || pane.Bounds.Width < 3 || pane.Bounds.Height < 1 {
		return
	}

	bx := pane.Bounds.X
	by := pane.Bounds.Y
	bw := pane.Bounds.Width
	bh := pane.Bounds.Height

	borderFg := toColor(m.theme.BorderColor)
	gutterBg := toColor(m.theme.GutterBg)
	gutterFg := toColor(m.theme.LineNumber)
	textBg := toColor(m.theme.Background)
	textFg := toColor(m.theme.Foreground)

	// Draw Right border divider if not reaching terminal edge
	if bx+bw < m.width {
		for y := by; y < by+bh; y++ {
			buf.SetRune(bx+bw, y, '│', borderFg, gutterBg, cell.AttrNone)
		}
	}
	// Draw Bottom border divider if not reaching status bar
	if by+bh < m.height-1 {
		for x := bx; x < bx+bw; x++ {
			buf.SetRune(x, by+bh, '─', borderFg, gutterBg, cell.AttrNone)
		}
	}

	contentTop := by
	contentHeight := bh
	if isMultiPane {
		// Draw Pane Mini Header at row by
		titleStr := PaneTitleForPane(pane, doc)
		if doc != nil {
			cursorLine := 0
			sels := doc.Buffer.GetSelections()
			if len(sels) > 0 {
				cursorLine = sels[0].Head.Line
			}
			var sampleLines []string
			tot := doc.Buffer.TotalLines()
			for i := 0; i <= cursorLine && i < tot; i++ {
				b, _ := doc.Buffer.GetLine(i)
				sampleLines = append(sampleLines, string(b))
			}
			var crumbs []syntax.BreadcrumbItem
			ext := filepath.Ext(doc.FilePath)
			if m.pluginMgr == nil || (ext != "" && m.pluginMgr.IsExtensionActive(ext)) {
				crumbs = syntax.ExtractBreadcrumbs(doc.FilePath, sampleLines, cursorLine)
			} else if base := filepath.Base(doc.FilePath); base != "" && base != "." {
				crumbs = []syntax.BreadcrumbItem{{Kind: "file", Name: base, Icon: "file", Line: 0}}
			}
			if len(crumbs) > 0 {
				titleStr += " " + syntax.FormatBreadcrumbs(crumbs)
			}
		}
		titleRunes := []rune(titleStr)
		headerBg := gutterBg
		headerFg := gutterFg
		headerAttr := cell.AttrNone
		if isActivePane {
			headerBg = toColor(m.theme.SelectionBg)
			headerFg = toColor(m.theme.Function)
			headerAttr = cell.AttrBold
		}
		for x := 0; x < bw; x++ {
			r := '─'
			fg := borderFg
			bg := gutterBg
			attr := cell.AttrNone
			if x < len(titleRunes) {
				r = titleRunes[x]
				fg = headerFg
				bg = headerBg
				attr = headerAttr
			}
			buf.SetRune(bx+x, by, r, fg, bg, attr)
		}
		// Draw close button ✕ at top right of pane header
		if bw >= 5 {
			closeBtnX := bx + bw - 2
			buf.SetRune(closeBtnX, by, '✕', toColor(m.theme.DiagnosticError), headerBg, cell.AttrBold)
		}
		contentTop++
		contentHeight--
	}

	if contentHeight < 1 {
		return
	}

	// Interactive Views in Split Pane (DAG Canvas, Project Graph, etc.)
	if pane.IsView() {
		viewArea := buffer.NewRect(bx, contentTop, bw, contentHeight)
		switch pane.ViewID {
		case "dag-canvas", "db-canvas":
			if m.dagCanvasWidget == nil {
				m.dagCanvasWidget = NewDAGCanvasWidget(dag.NewGraphModel(), &m.theme)
			}
			m.dagCanvasWidget.Render(buf, viewArea)
			return
		case "project-graph":
			if m.projectGraphPanel == nil {
				m.projectGraphPanel = NewProjectGraphPanel(&m.theme)
			}
			if len(m.projectGraphPanel.Canvas.Model.Nodes) == 0 {
				docToUse := doc
				if docToUse == nil && len(m.splits.Panes) > 0 {
					docToUse = m.findDocument(m.splits.Panes[0].DocID)
				}
				m.projectGraphPanel.RebuildWithLSP(docToUse, m.workspaceDir, m.lspClient)
			}
			m.projectGraphPanel.Render(buf, viewArea)
			return
		}
	}

	// Interactive Project Graph in Editor Tab
	if doc != nil && (filepath.Base(doc.FilePath) == "project.graph" || strings.HasSuffix(doc.FilePath, ".graph")) {
		if m.projectGraphPanel == nil {
			m.projectGraphPanel = NewProjectGraphPanel(&m.theme)
		}
		if len(m.projectGraphPanel.Canvas.Model.Nodes) == 0 {
			var docToUse *core.Document
			for _, d := range m.eng.Documents() {
				b := filepath.Base(d.FilePath)
				if b != "project.graph" && !strings.HasSuffix(b, ".graph") && b != "schema.erd" && !strings.HasSuffix(b, ".erd") {
					docToUse = d
					break
				}
			}
			m.projectGraphPanel.RebuildWithLSP(docToUse, m.workspaceDir, m.lspClient)
		}
		paneArea := buffer.NewRect(bx, contentTop, bw, contentHeight)
		m.projectGraphPanel.Render(buf, paneArea)
		return
	}

	// Interactive ER Diagram in Editor Tab
	if doc != nil && (filepath.Base(doc.FilePath) == "schema.erd" || strings.HasSuffix(doc.FilePath, ".erd")) {
		if m.dagCanvasWidget == nil {
			schema := db.LoadHybridSchema(m.workspaceDir)
			m.dagCanvasWidget = NewDAGCanvasWidget(schema.ToGraphModel(), &m.theme)
		}
		paneArea := buffer.NewRect(bx, contentTop, bw, contentHeight)
		m.dagCanvasWidget.Render(buf, paneArea)
		return
	}

	// Interactive DataGrid Table Viewer in Editor Tab
	if doc != nil && strings.HasSuffix(doc.FilePath, ".datagrid") {
		base := filepath.Base(doc.FilePath)
		tblName := strings.TrimSuffix(base, ".datagrid")
		grid := m.getOrCreateDataGrid(tblName)
		paneArea := buffer.NewRect(bx, contentTop, bw, contentHeight)
		grid.Render(buf, paneArea)
		return
	}

	// Image Viewer in Pane
	if doc != nil && IsImageFile(doc.FilePath) {
		iv := m.getOrCreateImageViewer(doc.FilePath)
		paneArea := buffer.NewRect(bx, contentTop, bw, contentHeight)
		iv.Render(buf, paneArea, &m.theme)
		return
	}

	// Live Markdown ANSI/ASCII Preview in Split Pane 1
	if m.mdPreviewOpen && pane.Index == 1 {
		doc0 := m.eng.ActiveDocument()
		if doc0 != nil {
			var fullMd strings.Builder
			tot := doc0.Buffer.TotalLines()
			for li := 0; li < tot; li++ {
				lb, _ := doc0.Buffer.GetLine(li)
				fullMd.WriteString(string(lb))
				fullMd.WriteString("\n")
			}
			m.mdRenderer.Parse(fullMd.String())
			m.mdRenderer.Render(buf, bx+1, contentTop, bw-2, contentHeight, pane.ViewportY, &m.theme)
			return
		}
	}

	if doc == nil {
		for y := 0; y < contentHeight; y++ {
			for x := 0; x < bw; x++ {
				buf.SetRune(bx+x, contentTop+y, ' ', textFg, textBg, cell.AttrNone)
			}
		}
		emptyMsg := " [No buffer open] "
		emptyRunes := []rune(emptyMsg)
		midY := contentTop + contentHeight/2
		midX := bx + max(0, (bw-len(emptyRunes))/2)
		for i, r := range emptyRunes {
			if midX+i < bx+bw {
				buf.SetRune(midX+i, midY, r, toColor(m.theme.Comment), textBg, cell.AttrDim)
			}
		}
		return
	}

	totalLines := doc.Buffer.TotalLines()
	gutterWidth := m.gutterWidth()
	if gutterWidth > bw-2 {
		gutterWidth = max(2, bw-2)
	}

	minimapW := 0
	if m.settings == nil || m.settings.Current.ShowMinimap {
		if bw >= 40 {
			minimapW = 4
		}
	}
	textWidth := bw - gutterWidth - 1 - minimapW
	if textWidth < 1 {
		textWidth = 1
	}

	vpX := pane.ViewportX
	vpY := pane.ViewportY
	if isActivePane {
		vpX = m.viewportX
		vpY = m.viewportY
		pane.ViewportX = vpX
		pane.ViewportY = vpY
	}

	doc.Viewport.Height = contentHeight
	doc.Viewport.Width = textWidth
	if isActivePane && pane.Index == 0 {
		m.editorColorSwatches = m.editorColorSwatches[:0]
	}

	if minimapW > 0 && doc != nil {
		minimapX := bx + bw - 1 - minimapW
		m.minimapHitboxes = append(m.minimapHitboxes, minimapHitbox{
			paneIdx:    pane.Index,
			minX:       minimapX,
			maxX:       bx + bw - 2,
			minY:       contentTop,
			maxY:       contentTop + contentHeight - 1,
			totalLines: totalLines,
		})
		getLine := func(idx int) string {
			b, _ := doc.Buffer.GetLine(idx)
			return string(b)
		}
		RenderMinimap(buf, m.theme, minimapX, contentTop, minimapW, contentHeight, totalLines, vpY, contentHeight, getLine)
	}

	sels := doc.Buffer.GetSelections()

	highlightQuery := ""
	if m.findReplaceModal != nil && m.findReplaceModal.Open && m.findReplaceModal.FindQuery != "" {
		highlightQuery = m.findReplaceModal.FindQuery
	} else if len(sels) > 0 && !sels[0].IsEmpty() {
		highlightQuery = m.selectedText()
	} else {
		highlightQuery = m.wordUnderCursor()
	}
	qRunes := []rune(highlightQuery)

	var activeDiff *git.FileGitDiff
	if doc != nil && doc.FilePath != "" {
		if m.gitWatcher != nil {
			activeDiff = m.gitWatcher.GetFileDiff(doc.FilePath)
		}
	}
	var gitlensDiff *gitlens.FileGutterDiff
	if m.gitlensTracker != nil && doc != nil && doc.FilePath != "" {
		gitlensDiff = m.gitlensTracker.GetFileDiff(doc.FilePath)
	}

	for row := 0; row < contentHeight; row++ {
		lineIdx := vpY + row
		screenY := contentTop + row

		icon := ' '
		gFg := gutterFg
		hasLineErr := false
		hasBookmark := false
		if m.bookmarkStore != nil && doc != nil && doc.FilePath != "" {
			if bm := m.bookmarkStore.GetBookmarkAt(doc.FilePath, lineIdx+1); bm != nil {
				hasBookmark = true
			}
		}

		if m.stoppedMarkerLine == lineIdx {
			icon = '>'
			gFg = toColor(m.theme.DiagnosticWarn)
		} else if m.breakpoints[lineIdx] {
			icon = '●'
			gFg = toColor(m.theme.DiagnosticError)
		} else if hasBookmark {
			icon = '●'
			gFg = toColor(0xF6C177) // Warm gold bookmark indicator
		} else if lineDiags, ok := m.getDiagnosticsForLine(lineIdx); ok && len(lineDiags) > 0 {
			for _, ld := range lineDiags {
				if ld.Severity == 1 || ld.Severity == 0 {
					hasLineErr = true
					break
				}
			}
			if hasLineErr {
				icon = ' '
				gFg = toColor(0xff5555)
			} else {
				icon = ' '
				gFg = toColor(m.theme.DiagnosticWarn)
			}
		}

		cursorLine := 0
		if len(sels) > 0 {
			cursorLine = sels[0].Head.Line
		}
		lineMode := "absolute"
		if m.settings != nil && m.settings.Current.LineNumbers != "" {
			lineMode = strings.ToLower(m.settings.Current.LineNumbers)
		}

		lineNumStr := ""
		if lineIdx < totalLines {
			switch lineMode {
			case "none":
				lineNumStr = fmt.Sprintf("%*s %c", gutterWidth-3, "", icon)
			case "relative":
				displayNum := lineIdx + 1
				if lineIdx != cursorLine {
					diff := lineIdx - cursorLine
					if diff < 0 {
						diff = -diff
					}
					displayNum = diff
				}
				lineNumStr = fmt.Sprintf("%*d %c", gutterWidth-3, displayNum, icon)
			default: // "absolute"
				lineNumStr = fmt.Sprintf("%*d %c", gutterWidth-3, lineIdx+1, icon)
			}
		} else {
			lineNumStr = fmt.Sprintf("%*s  ", gutterWidth-2, "~")
		}
		lineNumRunes := []rune(lineNumStr)

		gitMarker := ' '
		gitFg := gutterFg
		if gitlensDiff != nil {
			gm, st := gitlensDiff.GetMarker(lineIdx)
			if gm != gitlens.MarkerNone {
				gitMarker = gm
				switch st {
				case string(gitlens.StatusAdded):
					gitFg = toColor(m.theme.String) // Green +
				case string(gitlens.StatusModified):
					gitFg = toColor(m.theme.Function) // Cyan/Blue ~
				case string(gitlens.StatusDeleted):
					gitFg = toColor(m.theme.DiagnosticError) // Red -
				}
			}
		} else if activeDiff != nil {
			dk := activeDiff.Lines[lineIdx]
			switch dk {
			case git.DiffAdded:
				gitMarker = '+'
				gitFg = toColor(m.theme.String) // Green
			case git.DiffModified:
				gitMarker = '~'
				gitFg = toColor(m.theme.Function) // Blue/Cyan
			case git.DiffDeleted:
				gitMarker = '-'
				gitFg = toColor(m.theme.DiagnosticError) // Red
			}
		}

		covMarker := ' '
		covFg := gutterFg
		if m.coverageEngine != nil && doc != nil && doc.FilePath != "" {
			covSt := m.coverageEngine.GetLineStatus(doc.FilePath, lineIdx)
			switch covSt {
			case coverage.LineCovered:
				covMarker = '│'
				covFg = toColor(m.theme.String) // Green covered bar
			case coverage.LineUncovered:
				covMarker = '│'
				covFg = toColor(m.theme.DiagnosticError) // Red uncovered bar
			}
		}

		for gx := 0; gx < gutterWidth; gx++ {
			r := ' '
			fg := gutterFg
			if gx < len(lineNumRunes) {
				r = lineNumRunes[gx]
				if r == icon && icon != ' ' {
					fg = gFg
				} else if hasLineErr && r >= '0' && r <= '9' {
					fg = gFg
				}
			}
			if gx == gutterWidth-2 && covMarker != ' ' {
				r = covMarker
				fg = covFg
			}
			if gx == gutterWidth-1 && gitMarker != ' ' {
				r = gitMarker
				fg = gitFg
			}
			buf.SetRune(bx+gx, screenY, r, fg, gutterBg, cell.AttrNone)
		}

		lineBg := textBg
		lineFg := textFg
		if m.stoppedMarkerLine == lineIdx {
			lineBg = toColor(m.theme.SelectionBg)
		}

		var spans []syntax.Span
		var hexMatches []CodeColorMatch
		var visChars []visChar
		var occIntervals [][2]int
		var lineRainbowDepths map[int]int
		tabSize := 4
		if m.settings != nil && m.settings.Current.TabSize > 0 {
			tabSize = m.settings.Current.TabSize
		}
		leadingSpaces := 0

		if lineIdx < totalLines {
			lineBytes, _ := doc.Buffer.GetLine(lineIdx)
			lineStr := string(lineBytes)
			rawRunes := []rune(lineStr)
			lang := filepath.Ext(doc.FilePath)
			if m.pluginMgr == nil || (lang != "" && m.pluginMgr.IsExtensionActive(lang)) {
				spans = m.treeEngine.HighlightLine(lang, lineStr)
				if len(spans) == 0 && lang != "" {
					spans = m.highlighter.HighlightLine(lang, lineStr)
				}
			}
			hexMatches = FindHexColorsInLine(lineStr)

			if m.settings == nil || m.settings.Current.RainbowDelimiters {
				lineRainbowDepths = calculateLineRainbowDepths(lineStr)
			}

			for _, r := range rawRunes {
				if r == ' ' {
					leadingSpaces++
				} else if r == '\t' {
					leadingSpaces += tabSize
				} else {
					break
				}
			}

			vCol := 0
			for origCol, r := range rawRunes {
				if r == '\r' || r == '\n' {
					continue
				}
				if r == '\t' {
					tabSpaces := tabSize - (vCol % tabSize)
					for s := 0; s < tabSpaces; s++ {
						visChars = append(visChars, visChar{r: ' ', origCol: origCol})
						vCol++
					}
				} else {
					w := runewidth.RuneWidth(r)
					if w == 2 {
						visChars = append(visChars, visChar{r: r, origCol: origCol})
						visChars = append(visChars, visChar{r: ' ', origCol: origCol})
						vCol += 2
					} else if w == 0 {
						// Zero-width combining rune: associate with previous column if present
					} else {
						visChars = append(visChars, visChar{r: r, origCol: origCol})
						vCol++
					}
				}
			}

			if len(qRunes) > 0 && len(rawRunes) >= len(qRunes) {
				qLen := len(qRunes)
				matchCase := true
				if m.findReplaceModal != nil && m.findReplaceModal.Open {
					matchCase = m.findReplaceModal.MatchCase
				}
				for i := 0; i <= len(rawRunes)-qLen; i++ {
					matched := true
					for j := 0; j < qLen; j++ {
						rj := rawRunes[i+j]
						qj := qRunes[j]
						if matchCase {
							if rj != qj {
								matched = false
								break
							}
						} else {
							if unicode.ToLower(rj) != unicode.ToLower(qj) {
								matched = false
								break
							}
						}
					}
					if matched && m.findReplaceModal != nil && m.findReplaceModal.Open && m.findReplaceModal.WholeWord {
						isWord := func(r rune) bool {
							return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
						}
						if i > 0 && isWord(rawRunes[i-1]) {
							matched = false
						}
						if i+qLen < len(rawRunes) && isWord(rawRunes[i+qLen]) {
							matched = false
						}
					}
					if matched {
						occIntervals = append(occIntervals, [2]int{i, i + qLen})
					}
				}
			}
		}

		cursorVisCol := -1
		for col := 0; col < textWidth; col++ {
			charIdx := vpX + col
			ch := ' '
			origCol := -1
			if charIdx < len(visChars) {
				ch = visChars[charIdx].r
				origCol = visChars[charIdx].origCol
			}

			isCursor := false
			isSel := false
			for _, s := range sels {
				if lineIdx == s.Head.Line {
					headVisCol := doc.Buffer.VisualColumn(s.Head.Line, s.Head.Column)
					if charIdx == headVisCol {
						isCursor = true
						cursorVisCol = col
					}
				}
				start := s.Start()
				end := s.End()
				if !s.IsEmpty() && lineIdx >= start.Line && lineIdx <= end.Line {
					if origCol >= 0 {
						if start.Line == end.Line {
							if origCol >= start.Column && origCol < end.Column {
								isSel = true
							}
						} else if lineIdx == start.Line {
							if origCol >= start.Column {
								isSel = true
							}
						} else if lineIdx == end.Line {
							if origCol < end.Column {
								isSel = true
							}
						} else {
							isSel = true
						}
					}
				}
			}

			isOccurrence := false
			if origCol >= 0 && len(occIntervals) > 0 {
				for _, iv := range occIntervals {
					if origCol >= iv[0] && origCol < iv[1] {
						isOccurrence = true
						break
					}
				}
			}

			cellBg := lineBg
			if isOccurrence {
				occBg := m.theme.OccurrenceBg
				if occBg == 0 {
					occBg = 0x393b4f
				}
				cellBg = toColor(occBg)
			}
			if isSel {
				cellBg = toColor(m.theme.SelectionBg)
			}

			cellFg := lineFg
			attr := cell.AttrNone
			if origCol >= 0 {
				semanticMatched := false
				if semList, ok := m.semanticTokens[lineIdx]; ok {
					for _, sem := range semList {
						if origCol >= sem.CharStart && origCol < sem.CharStart+sem.Length {
							switch sem.TokenType {
							case "type", "class", "interface", "struct":
								cellFg = toColor(m.theme.Type)
							case "function", "method":
								cellFg = toColor(m.theme.Function)
							case "parameter":
								cellFg = toColor(m.theme.Foreground)
								attr = cell.AttrItalic
							case "variable", "property":
								cellFg = toColor(m.theme.Foreground)
							case "keyword":
								cellFg = toColor(m.theme.Keyword)
							case "string":
								cellFg = toColor(m.theme.String)
							case "number":
								cellFg = toColor(m.theme.Constant)
							case "comment":
								cellFg = toColor(m.theme.Comment)
							case "operator":
								cellFg = toColor(m.theme.DiagnosticInfo)
							}
							semanticMatched = true
							break
						}
					}
				}

				if !semanticMatched {
					for _, sp := range spans {
						if origCol >= sp.StartCol && origCol < sp.EndCol {
							switch sp.Type {
							case syntax.TokenKeyword:
								cellFg = toColor(m.theme.Keyword)
							case syntax.TokenFunction:
								cellFg = toColor(m.theme.Function)
							case syntax.TokenTypeIdent:
								cellFg = toColor(m.theme.Type)
							case syntax.TokenString:
								cellFg = toColor(m.theme.String)
							case syntax.TokenNumber, syntax.TokenConstant:
								cellFg = toColor(m.theme.Constant)
							case syntax.TokenComment:
								cellFg = toColor(m.theme.Comment)
							case syntax.TokenOperator:
								cellFg = toColor(m.theme.DiagnosticInfo)
							}
							break
						}
					}
				}

				if lineDiags, ok := m.getDiagnosticsForLine(lineIdx); ok {
					for _, ld := range lineDiags {
						startCol, endCol := expandDiagnosticRange(visChars, ld.Range.Start.Character, ld.Range.End.Character)
						if origCol >= startCol && origCol < endCol {
							if ld.Severity == 1 || ld.Severity == 0 {
								cellFg = toColor(0xff5555)
								attr |= cell.AttrUnderline | cell.AttrBold
							} else {
								cellFg = toColor(m.theme.DiagnosticWarn)
								attr |= cell.AttrUnderline
							}
							break
						}
					}
				}
			}

			// Rainbow Delimiters
			if (m.settings == nil || m.settings.Current.RainbowDelimiters) && origCol >= 0 && len(lineRainbowDepths) > 0 {
				if depth, ok := lineRainbowDepths[origCol]; ok {
					cellFg = toColor(rainbowPalette[depth%len(rainbowPalette)])
					attr |= cell.AttrBold
				}
			}

			// Virtual Text: Conceal (e.g. .env secrets masking)
			if doc.VirtualText != nil && (m.settings == nil || m.settings.Current.VirtualText) && origCol >= 0 {
				anns := doc.VirtualText.GetLineAnnotations(lineIdx)
				for _, ann := range anns {
					if ann.Kind == corebuf.AnnotationConceal && origCol >= ann.Col && origCol < ann.EndCol {
						ch = '•'
						cellFg = toColor(m.theme.Comment)
						break
					}
				}
			}

			// Indent Guides: vertical guide line at indentation multiples
			if (m.settings == nil || m.settings.Current.IndentGuides) && ch == ' ' && charIdx < leadingSpaces && charIdx > 0 && (charIdx%tabSize == 0) && !isCursor && !isSel {
				ch = '│'
				cellFg = toColor(m.theme.Comment)
				attr = cell.AttrDim
			}

			if origCol >= 0 && len(hexMatches) > 0 {
				for hmIdx := range hexMatches {
					hm := &hexMatches[hmIdx]
					if origCol >= hm.StartRune && origCol < hm.EndRune {
						m.editorColorSwatches = append(m.editorColorSwatches, EditorColorSwatch{
							PaneIndex: pane.Index,
							ScreenX:   bx + gutterWidth + col,
							ScreenY:   screenY,
							Line:      lineIdx,
							StartCol:  hm.StartRune,
							EndCol:    hm.EndRune,
							StartByte: hm.StartByte,
							EndByte:   hm.EndByte,
							Hex:       hm.Hex,
							Color:     hm.Color,
						})
						if origCol == hm.StartRune {
							ch = '■'
							cellFg = hm.Color
							attr = cell.AttrBold
						} else {
							cellFg = hm.Color
							attr = cell.AttrBold
						}
						break
					}
				}
			}

			attrVal := cell.AttrNone
			if attr != cell.AttrNone {
				attrVal = attr
			}
			if isCursor {
				if isActivePane && !m.sidebarFocused {
					m.activeCursorScreenX = bx + gutterWidth + col
					m.activeCursorScreenY = screenY

					cursorStyle := "block"
					if m.settings != nil && m.settings.Current.CursorStyle != "" {
						cursorStyle = strings.ToLower(m.settings.Current.CursorStyle)
					}
					switch cursorStyle {
					case "underline":
						attrVal |= cell.AttrUnderline | cell.AttrBold
						if ch == ' ' {
							ch = '_'
							cellFg = toColor(m.theme.Foreground)
						}
					case "bar":
						// Hardware terminal cursor renders the native bar beam (shape 5).
						// Do not invert character into a block!
						if ch == ' ' {
							cellFg = toColor(m.theme.Foreground)
						}
					default: // "block"
						attrVal = cell.AttrReverse
						if ch == ' ' {
							cellFg = toColor(m.theme.Foreground)
							cellBg = toColor(m.theme.Background)
						}
					}
				} else {
					attrVal = cell.AttrDim | cell.AttrReverse
				}
			}

			buf.SetRune(bx+gutterWidth+col, screenY, ch, cellFg, cellBg, attrVal)
		}

		// Inline Ghost Text: if active cursor is on this row and ghost text exists
		if isActivePane && cursorVisCol >= 0 && m.ghostText != "" && !m.sidebarFocused {
			ghostRunes := []rune(m.ghostText)
			cursorStyle := "block"
			if m.settings != nil && m.settings.Current.CursorStyle != "" {
				cursorStyle = strings.ToLower(m.settings.Current.CursorStyle)
			}
			if cursorStyle == "bar" {
				for gi, gr := range ghostRunes {
					gcol := cursorVisCol + 1 + gi
					if gcol < textWidth {
						buf.SetRune(bx+gutterWidth+gcol, screenY, gr, toColor(m.theme.Comment), textBg, cell.AttrDim)
					}
				}
			} else {
				for gi, gr := range ghostRunes {
					gcol := cursorVisCol + gi
					if gcol < textWidth {
						if gi == 0 {
							if cursorStyle == "underline" {
								buf.SetRune(bx+gutterWidth+gcol, screenY, gr, toColor(m.theme.Comment), textBg, cell.AttrDim|cell.AttrUnderline)
							} else {
								buf.SetRune(bx+gutterWidth+gcol, screenY, gr, toColor(m.theme.Background), toColor(m.theme.Foreground), cell.AttrNone)
							}
						} else {
							buf.SetRune(bx+gutterWidth+gcol, screenY, gr, toColor(m.theme.Comment), textBg, cell.AttrDim)
						}
					}
				}
			}
		}

		// Inlay Hints: render parameter and type annotations inline in subtle dim foreground
		if hints, ok := m.inlayHints[lineIdx]; ok {
			lineEndCol := len(visChars) - vpX
			if lineEndCol < 0 {
				lineEndCol = 0
			}
			hintCol := lineEndCol + 2
			for _, h := range hints {
				label := h.TextLabel()
				if label != "" && hintCol < textWidth-1 {
					hintRunes := []rune(" " + label + " ")
					for hi, hr := range hintRunes {
						drawCol := hintCol + hi
						if drawCol < textWidth {
							buf.SetRune(bx+gutterWidth+drawCol, screenY, hr, toColor(m.theme.Comment), textBg, cell.AttrDim|cell.AttrItalic)
						}
					}
					hintCol += len(hintRunes) + 1
				}
			}
		}

		// Virtual Text (End of Line: Git Inline Blame, DAP debug inspection)
		if doc.VirtualText != nil && (m.settings == nil || m.settings.Current.VirtualText) {
			lineAnns := doc.VirtualText.GetLineAnnotations(lineIdx)
			eolStartCol := len(visChars) - vpX
			if eolStartCol < 0 {
				eolStartCol = 0
			}
			eolStartCol += 2
			for _, ann := range lineAnns {
				if ann.Kind == corebuf.AnnotationEndOfLine && ann.Text != "" && eolStartCol < textWidth-1 {
					eolRunes := []rune(ann.Text)
					for ei, er := range eolRunes {
						drawCol := eolStartCol + ei
						if drawCol < textWidth {
							annColor := toColor(m.theme.Comment)
							if ann.FgColor != 0 {
								annColor = toColor(ann.FgColor)
							}
							buf.SetRune(bx+gutterWidth+drawCol, screenY, er, annColor, textBg, cell.AttrDim|cell.AttrItalic)
						}
					}
					eolStartCol += len(eolRunes) + 2
				}
			}
		}

		// Vertical Scrollbar on right edge of pane
		thumbPos := 0
		if totalLines > 1 {
			thumbPos = (vpY * contentHeight) / totalLines
		}
		sbChar := '│'
		if row == thumbPos {
			sbChar = '█'
		}
		buf.SetRune(bx+bw-1, screenY, sbChar, toColor(m.theme.LineNumber), gutterBg, cell.AttrNone)
	}
}

// openEditorColorPicker opens the color picker modal for a detected hex color swatch in the active document.
func (m *AppModel) openEditorColorPicker(swatch EditorColorSwatch) {
	if m.editorColorPicker == nil {
		m.editorColorPicker = NewColorPickerModal()
	}
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}

	m.editorColorPicker.OpenForColor(swatch.Hex, fmt.Sprintf("Code Color (%s)", swatch.Hex), swatch.Hex, func(key, newHex string) {
		curDoc := m.eng.ActiveDocument()
		if curDoc == nil {
			return
		}
		lineBytes, err := curDoc.Buffer.GetLine(swatch.Line)
		if err != nil {
			return
		}
		lineStr := string(lineBytes)
		matches := FindHexColorsInLine(lineStr)
		for _, match := range matches {
			if match.StartRune == swatch.StartCol || match.Hex == swatch.Hex {
				lineStartByte, err := curDoc.Buffer.ByteOffsetForLine(swatch.Line)
				if err != nil {
					break
				}
				startByte := lineStartByte + match.StartByte
				deleteLen := match.EndByte - match.StartByte
				_ = curDoc.Buffer.ApplyEdit(startByte, deleteLen, newHex)
				m.notifyLSPChange()
				m.statusMessage = fmt.Sprintf("Changed %s to %s", swatch.Hex, newHex)
				m.toasts.Info("COLOR", fmt.Sprintf("Applied %s", newHex))
				break
			}
		}
	})
	m.editorColorPicker.OnPreview = func(key, newHex string) {
		m.statusMessage = fmt.Sprintf("Previewing color: %s", newHex)
	}
}

// openEditorColorPickerAtCursor checks if the cursor is on a hex color and opens the color picker.
func (m *AppModel) openEditorColorPickerAtCursor() {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		return
	}
	head := sels[len(sels)-1].Head
	lineBytes, err := doc.Buffer.GetLine(head.Line)
	if err != nil {
		return
	}
	lineStr := string(lineBytes)
	matches := FindHexColorsInLine(lineStr)
	for _, match := range matches {
		if head.Column >= match.StartRune && head.Column <= match.EndRune {
			swatch := EditorColorSwatch{
				Line:      head.Line,
				StartCol:  match.StartRune,
				EndCol:    match.EndRune,
				StartByte: match.StartByte,
				EndByte:   match.EndByte,
				Hex:       match.Hex,
				Color:     match.Color,
			}
			m.openEditorColorPicker(swatch)
			return
		}
	}

	// Fallback: if cursor is not on an existing hex color, open picker to insert a new hex color
	if m.editorColorPicker == nil {
		m.editorColorPicker = NewColorPickerModal()
	}
	m.editorColorPicker.OpenForColor("#89b4fa", "Insert Hex Color", "#89b4fa", func(key, newHex string) {
		_ = m.eng.Dispatch(core.Command{ID: core.CmdInsertText, Args: newHex})
		m.notifyLSPChange()
		m.statusMessage = fmt.Sprintf("Inserted color %s", newHex)
		m.toasts.Info("COLOR", fmt.Sprintf("Inserted %s", newHex))
	})
}

