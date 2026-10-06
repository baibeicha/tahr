package tui

import (
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/core/cell"
	"github.com/baibeicha/goatui/pkg/tea"
	"github.com/mattn/go-runewidth"

	"tahr/internal/core"
	"tahr/internal/core/git"
	"tahr/internal/core/i18n"
	"tahr/internal/core/plugin"
	"tahr/internal/core/syntax"
)

// BoxStyle defines frame corner and border glyphs.
type BoxStyle struct {
	TopLeft     rune
	TopRight    rune
	BottomLeft  rune
	BottomRight rune
	Horiz       rune
	Vert        rune
}

// DefaultBoxChars returns the default rounded box characters.
func DefaultBoxChars() BoxStyle {
	return BoxStyle{
		TopLeft:     '╭',
		TopRight:    '╮',
		BottomLeft:  '╰',
		BottomRight: '╯',
		Horiz:       '─',
		Vert:        '│',
	}
}

// BoxChars returns the active box characters based on settings.BorderRounded.
func (m *AppModel) BoxChars() BoxStyle {
	rounded := true
	if m.settings != nil {
		rounded = m.settings.Current.BorderRounded
	}
	if rounded {
		return BoxStyle{
			TopLeft:     '╭',
			TopRight:    '╮',
			BottomLeft:  '╰',
			BottomRight: '╯',
			Horiz:       '─',
			Vert:        '│',
		}
	}
	return BoxStyle{
		TopLeft:     '┌',
		TopRight:    '┐',
		BottomLeft:  '└',
		BottomRight: '┘',
		Horiz:       '─',
		Vert:        '│',
	}
}

// DECSCUSREscapeCode returns the terminal cursor shape escape sequence based on settings.
func (m *AppModel) DECSCUSREscapeCode() string {
	if m.settings == nil {
		return "\x1b[1 q"
	}
	blink := m.settings.Current.CursorBlink
	switch m.settings.Current.CursorStyle {
	case "underline":
		if blink {
			return "\x1b[3 q"
		}
		return "\x1b[4 q"
	case "bar":
		if blink {
			return "\x1b[5 q"
		}
		return "\x1b[6 q"
	default: // "block"
		if blink {
			return "\x1b[1 q"
		}
		return "\x1b[2 q"
	}
}

type contextMenuItem struct {
	label  string
	action string
}

// getToolchainLabel returns the language or toolchain label for the given document.
// The label and its environment/SDK properties are declared dynamically by installed plugins.
func (m *AppModel) getToolchainLabel(doc *core.Document) string {
	if doc == nil || doc.FilePath == "" {
		return "Plain Text"
	}

	ext := strings.ToLower(filepath.Ext(doc.FilePath))
	baseName := filepath.Base(doc.FilePath)

	if m.pluginMgr != nil {
		if label, ok := m.pluginMgr.GetToolchainLabel(ext, baseName, m.workspaceDir); ok && label != "" {
			return label
		}
	}

	return "Plain Text"
}

func splitModeTitle(sm *SplitManager) string {
	if sm == nil {
		return i18n.T("split.mode_single")
	}
	switch sm.Mode {
	case SplitSingle:
		return i18n.T("split.mode_single")
	case Split2Cols:
		return i18n.T("split.mode_2cols")
	case Split2Rows:
		return i18n.T("split.mode_2rows")
	case Split3Cols:
		return i18n.T("split.mode_3cols")
	case Split4Grid:
		return i18n.T("split.mode_4grid")
	case Split5Panes:
		return i18n.T("split.mode_5panes")
	case Split6Grid:
		return i18n.T("split.mode_6grid")
	default:
		return i18n.T("split.mode_single")
	}
}

// View renders the application into the GoatUI Frame.
func (m *AppModel) View(f *tea.Frame) {
	if f == nil || f.Buffer == nil {
		return
	}
	buf := f.Buffer
	w := m.width
	h := m.height
	if w <= 0 || h <= 0 {
		return
	}

	// 0. Render Startup Splash Screen if active
	if m.splash != nil && m.splash.Active {
		m.splash.Render(buf, w, h, &m.theme)
		return
	}

	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	m.activeCursorScreenX = -1
	m.activeCursorScreenY = -1
	if m.gitWatcher != nil && doc.FilePath != "" {
		m.gitWatcher.SetActiveFile(doc.FilePath)
	}

	editorTop := 2
	statusBarY := h - 1

	m.minimapHitboxes = nil

	usableHeight := statusBarY - editorTop
	if usableHeight < 1 {
		usableHeight = 1
	}
	editorHeight := usableHeight
	if m.outputOpen {
		drawerH := m.outputHeight
		if drawerH > usableHeight-3 {
			drawerH = max(3, usableHeight-3)
		}
		editorHeight = usableHeight - drawerH
	}
	if m.terminal != nil && m.terminal.Open {
		termH := m.terminal.Height
		if termH > editorHeight-3 {
			termH = max(3, editorHeight-3)
		}
		editorHeight -= termH
	}
	if m.dapHUD != nil && m.dapHUD.Open {
		hudH := m.dapHUD.Height
		if hudH > editorHeight-3 {
			hudH = max(3, editorHeight-3)
		}
		editorHeight -= hudH
	}

	// 1. Draw Title Header Bar with Clean Pill Buttons (Row 0)
	headerBg := toColor(m.theme.StatusBarBg)
	headerFg := toColor(m.theme.StatusBarFg)
	btnBg := toColor(m.theme.PopupSelBg)

	for x := 0; x < w; x++ {
		buf.SetRune(x, 0, ' ', headerFg, headerBg, cell.AttrNone)
	}

	titlePrefix := " ≡ "
	for i, r := range titlePrefix {
		if i < w {
			buf.SetRune(i, 0, r, toColor(m.theme.Foreground), headerBg, cell.AttrBold)
		}
	}

	splitPanes := 1
	if m.splits != nil {
		splitPanes = m.splits.TotalPanes()
	}
	splitLabel := fmt.Sprintf(" %s ", i18n.T("toolbar.split", splitPanes))

	activeProfName := i18n.T("toolbar.no_profile")
	if m.launchConfig != nil {
		if act := m.launchConfig.GetActive(); act != nil && act.Name != "" {
			activeProfName = act.Name
		} else if len(m.launchConfig.Configurations) > 0 {
			activeProfName = m.launchConfig.Configurations[0].Name
		}
	}
	displayProfName := activeProfName
	if len([]rune(displayProfName)) > 14 {
		displayProfName = string([]rune(displayProfName)[:11]) + "..."
	}

	m.toolbarButtons = nil
	m.toolbarButtons = append(m.toolbarButtons, toolbarBtn{
		id:   "menu",
		minX: 0,
		maxX: len([]rune(titlePrefix)) - 1,
	})

	leftButtons := []struct {
		id    string
		label string
		fg    cell.Color
		bg    cell.Color
	}{
		{"find_files", fmt.Sprintf(" %s ", i18n.T("toolbar.find")), toColor(m.theme.Foreground), btnBg},
		{"split", splitLabel, toColor(m.theme.Function), btnBg},
		{"term", fmt.Sprintf(" %s ", i18n.T("toolbar.term")), toColor(m.theme.Constant), btnBg},
		{"build", fmt.Sprintf(" %s ", i18n.T("toolbar.build")), toColor(m.theme.DiagnosticWarn), btnBg},
	}

	rightButtons := []struct {
		id    string
		label string
		fg    cell.Color
		bg    cell.Color
	}{
		{"profile_select", fmt.Sprintf(" %s ▼ ", displayProfName), toColor(m.theme.Keyword), btnBg},
		{"run", fmt.Sprintf(" %s ", i18n.T("toolbar.run")), toColor(m.theme.String), btnBg},
		{"hud", fmt.Sprintf(" %s ", i18n.T("toolbar.debug")), toColor(m.theme.DiagnosticError), btnBg},
	}

	curX := len([]rune(titlePrefix))
	for _, b := range leftButtons {
		startX := curX
		for _, r := range b.label {
			if curX < w {
				buf.SetRune(curX, 0, r, b.fg, b.bg, cell.AttrBold)
				curX++
			}
		}
		m.toolbarButtons = append(m.toolbarButtons, toolbarBtn{
			id:   b.id,
			minX: startX,
			maxX: curX - 1,
		})
		if curX < w {
			buf.SetRune(curX, 0, ' ', headerFg, headerBg, cell.AttrNone)
			curX++
		}
	}

	// Calculate right-side buttons starting position
	rightTotalW := 0
	for _, b := range rightButtons {
		rightTotalW += len([]rune(b.label)) + 1
	}

	rightStartX := w - rightTotalW
	if rightStartX < curX+1 {
		rightStartX = curX + 1
	}

	rx := rightStartX
	for _, b := range rightButtons {
		startX := rx
		for _, r := range b.label {
			if rx < w {
				buf.SetRune(rx, 0, r, b.fg, b.bg, cell.AttrBold)
				rx++
			}
		}
		m.toolbarButtons = append(m.toolbarButtons, toolbarBtn{
			id:   b.id,
			minX: startX,
			maxX: rx - 1,
		})
		if rx < w {
			buf.SetRune(rx, 0, ' ', headerFg, headerBg, cell.AttrNone)
			rx++
		}
	}

	filename := doc.FilePath
	if filename == "" {
		filename = "untitled"
	}
	if doc.Buffer.IsModified() {
		filename += " ●"
	}
	fileLabel := fmt.Sprintf(" %s ", filepath.Base(filename))
	fileLen := len([]rune(fileLabel))
	availSpace := rightStartX - curX
	if availSpace > fileLen+2 {
		fileX := curX + (availSpace-fileLen)/2
		for i, r := range fileLabel {
			buf.SetRune(fileX+i, 0, r, toColor(m.theme.Foreground), headerBg, cell.AttrBold)
		}
	}

	// 2. Draw Open Files Tab Bar (Row 1)
	m.tabHitboxes = nil
	tabBarY := 1
	tabBarBg := toColor(m.theme.GutterBg)
	tabBarFg := toColor(m.theme.LineNumber)
	for x := 0; x < w; x++ {
		buf.SetRune(x, tabBarY, ' ', tabBarFg, tabBarBg, cell.AttrNone)
	}

	curTabX := 1
	docs := m.eng.Documents()
	for _, d := range docs {
		baseName := filepath.Base(d.FilePath)
		if baseName == "" || baseName == "." {
			baseName = "untitled"
		}
		isActive := (doc != nil && d.ID == doc.ID)
		isMod := d.Buffer.IsModified()

		tabBg := tabBarBg
		tabFg := tabBarFg
		attr := cell.AttrNone
		if isActive {
			tabBg = toColor(m.theme.Background)
			tabFg = toColor(m.theme.Foreground)
			attr = cell.AttrBold
		}

		startX := curTabX
		buf.SetRune(curTabX, tabBarY, ' ', tabFg, tabBg, attr)
		curTabX++

		for _, r := range baseName {
			if curTabX < w-6 {
				buf.SetRune(curTabX, tabBarY, r, tabFg, tabBg, attr)
				curTabX++
			}
		}

		if isMod {
			buf.SetRune(curTabX, tabBarY, ' ', tabFg, tabBg, attr)
			curTabX++
			buf.SetRune(curTabX, tabBarY, '●', toColor(m.theme.DiagnosticWarn), tabBg, attr)
			curTabX++
		}

		buf.SetRune(curTabX, tabBarY, ' ', tabFg, tabBg, attr)
		curTabX++
		closeX := curTabX
		buf.SetRune(curTabX, tabBarY, '×', toColor(m.theme.DiagnosticError), tabBg, attr)
		curTabX++

		buf.SetRune(curTabX, tabBarY, ' ', tabFg, tabBg, attr)
		curTabX++
		endX := curTabX - 1

		m.tabHitboxes = append(m.tabHitboxes, tabHitbox{
			docID:  d.ID,
			minX:   startX,
			maxX:   endX,
			closeX: closeX,
		})

		if curTabX < w-1 {
			buf.SetRune(curTabX, tabBarY, '│', toColor(m.theme.BorderColor), tabBarBg, cell.AttrNone)
			curTabX++
		}
	}

	// Breadcrumbs on Row 1 (Tab Bar right side)
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
			crumbStr := syntax.FormatBreadcrumbs(crumbs)
			crumbRunes := []rune(" " + crumbStr + " ")
			startCrumbX := w - len(crumbRunes) - 1
			if startCrumbX > curTabX+2 {
				for i, r := range crumbRunes {
					buf.SetRune(startCrumbX+i, tabBarY, r, toColor(m.theme.Comment), tabBarBg, cell.AttrNone)
				}
			}
		}
	}

	// 3. Activity Bar (Clean, uncluttered primary navigation)
	isTreeRight := (m.settings != nil && m.settings.Current.TreePosition == "right")
	stripLeftW := 0
	if w >= 70 {
		stripLeftW = 3
	}

	if stripLeftW > 0 {
		stripBg := toColor(m.theme.GutterBg)
		stripFg := toColor(m.theme.LineNumber)
		borderFg := toColor(m.theme.BorderColor)
		activeStripBg := toColor(m.theme.PopupSelBg)
		activeStripFg := toColor(m.theme.PopupSelFg)

		for y := editorTop; y < statusBarY; y++ {
			buf.SetRune(0, y, ' ', stripFg, stripBg, cell.AttrNone)
			buf.SetRune(1, y, ' ', stripFg, stripBg, cell.AttrNone)
			buf.SetRune(2, y, '│', borderFg, stripBg, cell.AttrNone)
		}

		// Project Explorer: EX (row 2)
		prjBg := stripBg
		prjFg := stripFg
		if m.sidebarOpen && m.sidebarMode == 0 {
			prjBg = activeStripBg
			prjFg = activeStripFg
		}
		buf.SetRune(0, 2, 'E', prjFg, prjBg, cell.AttrBold)
		buf.SetRune(1, 2, 'X', prjFg, prjBg, cell.AttrBold)

		// Structure: ST (row 4)
		stBg := stripBg
		stFg := stripFg
		if m.sidebarOpen && m.sidebarMode == 1 {
			stBg = activeStripBg
			stFg = activeStripFg
		}
		buf.SetRune(0, 4, 'S', stFg, stBg, cell.AttrBold)
		buf.SetRune(1, 4, 'T', stFg, stBg, cell.AttrBold)

		// Version Control: ⎇ (row 6)
		buf.SetRune(0, 6, '⎇', toColor(m.theme.DiagnosticWarn), stripBg, cell.AttrNone)
		buf.SetRune(1, 6, ' ', stripFg, stripBg, cell.AttrNone)

		// Plugins Marketplace: PL (row 8)
		buf.SetRune(0, 8, 'P', toColor(m.theme.Keyword), stripBg, cell.AttrNone)
		buf.SetRune(1, 8, 'L', toColor(m.theme.Keyword), stripBg, cell.AttrNone)

		// Settings: ⚙ at bottom of left activity bar
		if statusBarY-2 > 8 {
			buf.SetRune(0, statusBarY-2, '⚙', toColor(m.theme.Function), stripBg, cell.AttrNone)
			buf.SetRune(1, statusBarY-2, ' ', stripFg, stripBg, cell.AttrNone)
		}
	}

	// 3.1 Right Activity Strip (Tool window switcher: AI, DB, Graphs, Plugins)
	stripRightW := 0
	if w >= 70 {
		stripRightW = 3
	}

	if stripRightW > 0 {
		stripBg := toColor(m.theme.GutterBg)
		stripFg := toColor(m.theme.LineNumber)
		borderFg := toColor(m.theme.BorderColor)
		activeStripBg := toColor(m.theme.PopupSelBg)
		activeStripFg := toColor(m.theme.PopupSelFg)

		rx := w - stripRightW
		for y := editorTop; y < statusBarY; y++ {
			buf.SetRune(rx, y, '│', borderFg, stripBg, cell.AttrNone)
			buf.SetRune(rx+1, y, ' ', stripFg, stripBg, cell.AttrNone)
			buf.SetRune(rx+2, y, ' ', stripFg, stripBg, cell.AttrNone)
		}

		// Render deduplicated right activity strip badges
		items := m.getRightStripItems()
		row := 2
		for _, it := range items {
			if row < statusBarY-3 {
				itBg := stripBg
				itFg := stripFg
				if m.rightSidebarOpen && (m.rightSidebarMode == it.mode ||
					(it.mode == "db-inspector" && m.rightSidebarMode == "db") ||
					(it.mode == "ai-chat" && m.rightSidebarMode == "ai") ||
					(it.mode == "project-graphs" && m.rightSidebarMode == "graphs")) {
					itBg = activeStripBg
					itFg = activeStripFg
				}
				buf.SetRune(rx+1, row, it.r1, itFg, itBg, cell.AttrBold)
				buf.SetRune(rx+2, row, it.r2, itFg, itBg, cell.AttrBold)
				row += 2
			}
		}

		// Collapse/Expand toggle icon at bottom
		if statusBarY-2 > 6 {
			toggleIcon := '«'
			if m.rightSidebarOpen {
				toggleIcon = '»'
			}
			buf.SetRune(rx+1, statusBarY-2, toggleIcon, toColor(m.theme.Function), stripBg, cell.AttrNone)
		}
	}

	// 4. Sidebar width with smooth 120 FPS animation
	sideW := int(math.Round(m.sidebarAnimWidth))
	rightSideW := int(math.Round(m.rightSidebarAnimWidth))

	editorLeft := stripLeftW
	editorRight := w - stripRightW
	treeStartX := stripLeftW

	if isTreeRight {
		treeStartX = w - stripRightW - sideW
		editorLeft = stripLeftW
		editorRight = treeStartX
		if sideW > 0 {
			editorRight = treeStartX - 1 // space for divider
		}
	} else {
		treeStartX = stripLeftW
		editorLeft = stripLeftW
		if sideW > 0 {
			editorLeft = stripLeftW + sideW + 1 // space for divider
		}
		if rightSideW > 0 {
			editorRight = w - stripRightW - rightSideW - 1 // space for divider
		} else {
			editorRight = w - stripRightW
		}
	}

	// Draw Right Sidebar (if open and width > 0)
	if rightSideW > 0 {
		rightStartX := w - stripRightW - rightSideW
		sidebarBg := toColor(m.theme.GutterBg)
		borderFg := toColor(m.theme.BorderColor)
		// Vertical divider between editor and right sidebar
		if rightStartX-1 >= 0 && rightStartX-1 < w {
			for y := editorTop; y < statusBarY; y++ {
				buf.SetRune(rightStartX-1, y, '│', borderFg, sidebarBg, cell.AttrNone)
			}
		}
		m.renderRightSidebar(buf, rightStartX, editorTop, rightSideW, editorHeight)
	}

	// Draw Project Tree Sidebar (if open and width > 0)
	if sideW > 0 {
		sidebarBg := toColor(m.theme.GutterBg)
		sidebarFg := toColor(m.theme.Foreground)
		borderFg := toColor(m.theme.BorderColor)
		selBg := toColor(m.theme.PopupSelBg)
		selFg := toColor(m.theme.PopupSelFg)

		// Header row with dock and resize buttons
		headerTitle := " PROJECT EXPLORER"
		if m.sidebarMode == 1 {
			headerTitle = " DOCUMENT STRUCTURE"
		}
		expRunes := []rune(headerTitle)
		btnCount := 5
		if sideW < 18 {
			btnCount = 1
		}
		dockBtnStart := treeStartX + sideW - btnCount

		for x := 0; x < sideW; x++ {
			buf.SetRune(treeStartX+x, editorTop, ' ', toColor(m.theme.Keyword), sidebarBg, cell.AttrBold)
		}
		for i, r := range expRunes {
			if treeStartX+i < dockBtnStart-1 {
				buf.SetRune(treeStartX+i, editorTop, r, toColor(m.theme.Keyword), sidebarBg, cell.AttrBold)
			}
		}

		// Render dense buttons directly on sidebar background: "◀ ▶ ⇄" (1 space between each button)
		btnFg := toColor(m.theme.Foreground)
		if sideW >= 18 {
			buf.SetRune(dockBtnStart, editorTop, '◀', btnFg, sidebarBg, cell.AttrBold)
			buf.SetRune(dockBtnStart+1, editorTop, ' ', btnFg, sidebarBg, cell.AttrNone)
			buf.SetRune(dockBtnStart+2, editorTop, '▶', btnFg, sidebarBg, cell.AttrBold)
			buf.SetRune(dockBtnStart+3, editorTop, ' ', btnFg, sidebarBg, cell.AttrNone)
			buf.SetRune(dockBtnStart+4, editorTop, '⇄', btnFg, sidebarBg, cell.AttrBold)
		} else {
			buf.SetRune(dockBtnStart, editorTop, '⇄', btnFg, sidebarBg, cell.AttrBold)
		}

		// Sidebar content: Structure panel or Tree nodes
		visibleTreeRows := editorHeight - 1
		if m.sidebarMode == 1 && m.structurePanel != nil {
			m.structurePanel.Render(buf, treeStartX, editorTop+1, sideW, visibleTreeRows, m.sidebarFocused, &m.theme)
		} else {
			m.clampSidebar()
			var fileStatuses map[string]string
			if m.gitWatcher != nil {
				fileStatuses = m.gitWatcher.GetFileStatuses()
			}
			for row := 0; row < visibleTreeRows; row++ {
				nodeIdx := m.sidebarScrollY + row
				screenY := editorTop + 1 + row

				nodeFg := sidebarFg
				nodeBg := sidebarBg
				isSel := (nodeIdx == m.treeSel && m.sidebarFocused)

				isOpenDoc := false
				if nodeIdx < len(m.treeFlat) {
					node := m.treeFlat[nodeIdx]
					if doc != nil && doc.FilePath != "" && !node.IsDir {
						if filepath.Clean(node.Path) == filepath.Clean(doc.FilePath) {
							isOpenDoc = true
						}
					}
				}

				attr := cell.AttrNone
				if isSel {
					nodeBg = selBg
					nodeFg = selFg
					attr = cell.AttrBold
				} else if isOpenDoc {
					// Soft background highlight for open document, NO '*' marker!
					nodeBg = toColor(m.theme.SelectionBg)
					nodeFg = toColor(m.theme.Function)
					attr = cell.AttrBold
				}

				lineStr := ""
				var statusBadge string
				var badgeColor cell.Color
				if nodeIdx < len(m.treeFlat) {
					node := m.treeFlat[nodeIdx]
					indent := strings.Repeat("  ", node.Depth)
					iconStyle := "minimal"
					if m.settings != nil && m.settings.Current.FileIconStyle != "" {
						iconStyle = m.settings.Current.FileIconStyle
					}
					icon := GetFileIconWithStyle(node, iconStyle)
					lineStr = fmt.Sprintf("%s%s%s", indent, icon, node.Name)

					if fileStatuses != nil {
						cleanPath := filepath.Clean(node.Path)
						st, ok := fileStatuses[cleanPath]
						if !ok {
							st, ok = fileStatuses[strings.ToLower(cleanPath)]
						}
						if !ok && node.IsDir {
							dirPrefix := cleanPath + string(filepath.Separator)
							dirPrefixLower := strings.ToLower(dirPrefix)
							for path, s := range fileStatuses {
								if strings.HasPrefix(path, dirPrefix) || strings.HasPrefix(strings.ToLower(path), dirPrefixLower) {
									st = s
									ok = true
									if s == "M" {
										break // Modified has highest priority
									}
								}
							}
						}
						if ok {
							switch st {
							case "M":
								statusBadge = " M "
								badgeColor = toColor(m.theme.DiagnosticWarn) // Yellow
							case "U":
								statusBadge = " U "
								badgeColor = toColor(m.theme.String) // Green
							case "A":
								statusBadge = " A "
								badgeColor = toColor(m.theme.DiagnosticInfo) // Cyan
							}
						}
					}
				}
				lineRunes := []rune(lineStr)
				badgeRunes := []rune(statusBadge)

				// Ensure badge is not cut off if line text is long
				if len(badgeRunes) > 0 && len(lineRunes)+len(badgeRunes) > sideW && sideW > len(badgeRunes) {
					lineRunes = lineRunes[:sideW-len(badgeRunes)]
				}

				for x := 0; x < sideW; x++ {
					r := ' '
					fg := nodeFg
					if x < len(lineRunes) {
						r = lineRunes[x]
					} else if x < len(lineRunes)+len(badgeRunes) {
						r = badgeRunes[x-len(lineRunes)]
						if statusBadge != "" {
							fg = badgeColor
						}
					}
					buf.SetRune(treeStartX+x, screenY, r, fg, nodeBg, attr)
				}
			}
		}

		// Vertical divider between sidebar and editor
		dividerX := treeStartX - 1
		if !isTreeRight {
			dividerX = treeStartX + sideW
		}
		if dividerX >= 0 && dividerX < w {
			for y := editorTop; y < editorTop+editorHeight; y++ {
				buf.SetRune(dividerX, y, '│', borderFg, sidebarBg, cell.AttrNone)
			}
		}
	}

	// 5. Draw Editor Split Panes (1 to 6 panes)
	editorArea := buffer.NewRect(editorLeft, editorTop, editorRight-editorLeft, editorHeight)
	activeDocID := ""
	if doc != nil {
		activeDocID = doc.ID
	}
	m.splits.UpdateLayout(editorArea, m.eng.Documents(), activeDocID)

	isMultiPane := (m.splits.TotalPanes() > 1)
	for i := range m.splits.Panes {
		p := &m.splits.Panes[i]
		paneDoc := m.findDocument(p.DocID)
		if paneDoc == nil {
			paneDoc = doc
		}
		isActivePane := (i == m.splits.ActiveIndex)
		m.renderPane(buf, p, paneDoc, isActivePane, isMultiPane)
	}

	// 6. Draw Build & Run Output Drawer (if open)
	if m.outputOpen {
		drawerH := m.outputHeight
		drawerTop := editorTop + editorHeight
		outBg := toColor(m.theme.StatusBarBg)
		outFg := toColor(m.theme.Foreground)
		borderFg := toColor(m.theme.BorderColor)
		titleFg := toColor(m.theme.Function)

		taskTitle := "OUTPUT TERMINAL"
		if m.runner != nil && m.runner.Title() != "" {
			taskTitle = m.runner.Title()
		}
		runningTag := " FINISHED "
		if m.runner != nil && m.runner.IsRunning() {
			runningTag = " RUNNING... "
			titleFg = toColor(m.theme.String)
		}
		box := m.BoxChars()
		headerLine := fmt.Sprintf("%c── %s %s ", box.TopLeft, taskTitle, runningTag)
		closeBtn := fmt.Sprintf(" ✕ Close %c", box.TopRight)
		headerRunes := []rune(headerLine)
		closeRunes := []rune(closeBtn)

		for x := 0; x < w; x++ {
			r := box.Horiz
			fg := borderFg
			if x == 0 {
				r = box.TopLeft
			} else if x < len(headerRunes) {
				r = headerRunes[x]
				fg = titleFg
			} else if x >= w-len(closeRunes) {
				idx := x - (w - len(closeRunes))
				if idx >= 0 && idx < len(closeRunes) {
					r = closeRunes[idx]
					fg = toColor(m.theme.DiagnosticError)
				}
			}
			buf.SetRune(x, drawerTop, r, fg, outBg, cell.AttrBold)
		}

		var lines []string
		if m.runner != nil {
			lines = m.runner.Lines()
		}
		visibleLogRows := drawerH - 1
		startIdx := 0
		if len(lines) > visibleLogRows {
			startIdx = len(lines) - visibleLogRows
		}

		for row := 0; row < visibleLogRows; row++ {
			lineIdx := startIdx + row
			screenY := drawerTop + 1 + row
			lineText := ""
			if lineIdx >= 0 && lineIdx < len(lines) {
				lineText = lines[lineIdx]
			}
			lineRunes := []rune(lineText)

			lineFg := outFg
			if strings.Contains(lineText, "error") || strings.Contains(lineText, "failed") {
				lineFg = toColor(m.theme.DiagnosticError)
			} else if strings.Contains(lineText, "success") || strings.Contains(lineText, "PASS") {
				lineFg = toColor(m.theme.String)
			}

			buf.SetRune(0, screenY, '│', borderFg, outBg, cell.AttrNone)
			for x := 1; x < w-1; x++ {
				r := ' '
				if x-1 < len(lineRunes) {
					r = lineRunes[x-1]
				}
				buf.SetRune(x, screenY, r, lineFg, outBg, cell.AttrNone)
			}
			buf.SetRune(w-1, screenY, '│', borderFg, outBg, cell.AttrNone)
		}
	}

	// 6.5. Draw Integrated Terminal Drawer (if open)
	currentBottom := statusBarY
	if m.terminal != nil && m.terminal.Open {
		termH := m.terminal.Height
		if termH > usableHeight-3 {
			termH = max(3, usableHeight-3)
		}
		termTop := currentBottom - termH
		currentBottom = termTop

		termBg := toColor(m.theme.Background)
		termFg := toColor(m.theme.Foreground)
		borderFg := toColor(m.theme.BorderColor)

		focusStatus := i18n.T("term.inactive")
		statusFg := toColor(m.theme.Comment)
		if m.terminalFocused {
			focusStatus = i18n.T("term.focused")
			statusFg = toColor(m.theme.Function)
		}

		box := m.BoxChars()
		m.termTabHitboxes = nil
		m.termHeaderBtnHitboxes = nil

		// Top-right buttons laid out dynamically right-to-left
		btnBg := termBg
		btnFg := toColor(m.theme.Foreground)

		type termBtnDef struct {
			id   string
			text string
		}
		rButtons := []termBtnDef{
			{"close", "✕"},
			{"kill", i18n.T("term.kill")},
			{"clear", i18n.T("term.clear")},
			{"down", "▼"},
			{"up", "▲"},
		}

		type placedHeaderBtn struct {
			id     string
			text   string
			startX int
			width  int
		}
		var placedBtns []placedHeaderBtn
		curRightX := w - 2

		for _, b := range rButtons {
			btnW := runewidth.StringWidth(b.text)
			btnStartX := curRightX - btnW
			if btnStartX < 10 {
				break
			}
			placedBtns = append(placedBtns, placedHeaderBtn{
				id:     b.id,
				text:   b.text,
				startX: btnStartX,
				width:  btnW,
			})
			m.termHeaderBtnHitboxes = append(m.termHeaderBtnHitboxes, termHeaderBtnHitbox{
				id:   b.id,
				minX: btnStartX,
				maxX: btnStartX + btnW - 1,
			})
			curRightX = btnStartX - 1 // 1 column spacing between buttons
		}

		btnZoneStart := curRightX + 1
		if btnZoneStart < 0 {
			btnZoneStart = 0
		}
		maxTabsW := btnZoneStart - 2
		if maxTabsW < 10 {
			maxTabsW = 10
		}

		prefix := fmt.Sprintf("%c── %s  %s  ", box.TopLeft, i18n.T("term.title"), focusStatus)
		pRunes := []rune(prefix)

		for x := 0; x < w; x++ {
			buf.SetRune(x, termTop, box.Horiz, borderFg, termBg, cell.AttrBold)
		}
		for i, r := range pRunes {
			if i < w {
				buf.SetRune(i, termTop, r, statusFg, termBg, cell.AttrBold)
			}
		}

		curX := len(pRunes)
		for ti, tInst := range m.terminal.Instances {
			tabName := tInst.Name
			if m.terminal.IsRenaming && m.terminal.RenamingIdx == ti {
				tabName = m.terminal.RenamingInput + "✎"
			}
			tabLabel := fmt.Sprintf(" %d: %s ✕ ", ti+1, tabName)
			tabRunes := []rune(tabLabel)
			if curX+len(tabRunes) >= maxTabsW {
				break
			}
			tabFg := toColor(m.theme.Comment)
			tabBg := termBg
			if ti == m.terminal.ActiveIdx {
				tabFg = toColor(m.theme.Foreground)
				tabBg = toColor(m.theme.PopupSelBg)
			}

			startTabX := curX
			for _, r := range tabRunes {
				buf.SetRune(curX, termTop, r, tabFg, tabBg, cell.AttrBold)
				curX++
			}
			endTabX := curX - 1
			closeX := endTabX - 1

			m.termTabHitboxes = append(m.termTabHitboxes, termTabHitbox{
				minX:        startTabX,
				maxX:        endTabX,
				closeMinX:   closeX - 1,
				closeMaxX:   closeX + 1,
				instanceIdx: ti,
			})

			if curX < maxTabsW {
				buf.SetRune(curX, termTop, ' ', borderFg, termBg, cell.AttrNone)
				curX++
			}
		}

		// + Add button
		addLabel := fmt.Sprintf(" %s ", i18n.T("term.add"))
		addRunes := []rune(addLabel)
		if curX+len(addRunes) < maxTabsW {
			startAddX := curX
			for _, r := range addRunes {
				buf.SetRune(curX, termTop, r, toColor(m.theme.String), toColor(m.theme.PopupSelBg), cell.AttrBold)
				curX++
			}
			m.termTabHitboxes = append(m.termTabHitboxes, termTabHitbox{
				minX:        startAddX,
				maxX:        curX - 1,
				instanceIdx: -1,
				isAdd:       true,
			})
			if curX < maxTabsW {
				buf.SetRune(curX, termTop, ' ', borderFg, termBg, cell.AttrNone)
				curX++
			}
		}

		// Split tag button
		splitTag := fmt.Sprintf(" %s ", i18n.T("term.split"))
		if m.terminal.SplitMode == TermSplitHorizontal {
			splitTag = fmt.Sprintf(" %s ", i18n.T("term.stack"))
		} else if m.terminal.SplitMode == TermSplitVertical {
			splitTag = fmt.Sprintf(" %s ", i18n.T("term.tabs"))
		}
		splitRunes := []rune(splitTag)
		if curX+len(splitRunes) < maxTabsW {
			startSplitX := curX
			for _, r := range splitRunes {
				buf.SetRune(curX, termTop, r, toColor(m.theme.Keyword), toColor(m.theme.PopupSelBg), cell.AttrBold)
				curX++
			}
			m.termTabHitboxes = append(m.termTabHitboxes, termTabHitbox{
				minX:        startSplitX,
				maxX:        curX - 1,
				instanceIdx: -1,
				isSplit:     true,
			})
		}

		// Clear spaces for the button zone so box.Horiz doesn't collide
		for x := btnZoneStart; x < w-1; x++ {
			if x >= 0 {
				buf.SetRune(x, termTop, ' ', borderFg, termBg, cell.AttrNone)
			}
		}
		// Draw placed buttons
		for _, pb := range placedBtns {
			col := 0
			for _, r := range []rune(pb.text) {
				rw := runewidth.RuneWidth(r)
				if pb.startX+col+rw < w {
					buf.SetRune(pb.startX+col, termTop, r, btnFg, btnBg, cell.AttrBold)
					for extra := 1; extra < rw; extra++ {
						buf.SetRune(pb.startX+col+extra, termTop, ' ', btnFg, btnBg, cell.AttrBold)
					}
				}
				col += rw
			}
		}
		// Draw top-right frame corner with correct border color
		if w > 0 {
			buf.SetRune(w-1, termTop, box.TopRight, borderFg, termBg, cell.AttrBold)
		}

		innerW := max(1, w-2)
		innerH := max(1, termH-1)
		m.terminal.Resize(innerW, innerH)

		// Render virtual terminal screen buffer (single or split views)
		if m.terminal.SplitMode == TermSplitHorizontal && len(m.terminal.Instances) >= 2 {
			colW := (innerW - 1) / 2
			m.terminal.Instances[0].VTerm.Render(buf, 1, termTop+1, colW, innerH, termFg, termBg, m.terminal.Open)
			splitBorderFg := borderFg
			if m.terminal.ActiveIdx == 0 {
				splitBorderFg = toColor(m.theme.Function)
			}
			for row := 0; row < innerH; row++ {
				buf.SetRune(1+colW, termTop+1+row, box.Vert, splitBorderFg, termBg, cell.AttrNone)
			}
			remW := innerW - colW - 1
			m.terminal.Instances[1].VTerm.Render(buf, 1+colW+1, termTop+1, remW, innerH, termFg, termBg, m.terminal.Open)
		} else if m.terminal.SplitMode == TermSplitVertical && len(m.terminal.Instances) >= 2 {
			rowH := (innerH - 1) / 2
			m.terminal.Instances[0].VTerm.Render(buf, 1, termTop+1, innerW, rowH, termFg, termBg, m.terminal.Open)
			splitBorderFg := borderFg
			if m.terminal.ActiveIdx == 0 {
				splitBorderFg = toColor(m.theme.Function)
			}
			for col := 0; col < innerW; col++ {
				buf.SetRune(1+col, termTop+1+rowH, box.Horiz, splitBorderFg, termBg, cell.AttrNone)
			}
			remH := innerH - rowH - 1
			m.terminal.Instances[1].VTerm.Render(buf, 1, termTop+1+rowH+1, innerW, remH, termFg, termBg, m.terminal.Open)
		} else {
			m.terminal.VTerm.Render(buf, 1, termTop+1, innerW, innerH, termFg, termBg, m.terminal.Open)
		}

		// Draw side borders
		for row := 0; row < innerH; row++ {
			buf.SetRune(0, termTop+1+row, box.Vert, borderFg, termBg, cell.AttrNone)
			buf.SetRune(w-1, termTop+1+row, box.Vert, borderFg, termBg, cell.AttrNone)
		}
	}

	// 6.6. Draw DAP Debugger HUD (if open)
	if m.dapHUD != nil && m.dapHUD.Open {
		hudH := m.dapHUD.Height
		if hudH > usableHeight-3 {
			hudH = max(3, usableHeight-3)
		}
		hudTop := currentBottom - hudH
		currentBottom = hudTop
		RenderDAPHUD(buf, m.theme, m.dapHUD, m.dapSession, 0, hudTop, w, hudH)
	}

	// 7. Draw Status Bar Footer (Bottom row)
	statusBg := toColor(m.theme.StatusBarBg)
	statusFg := toColor(m.theme.StatusBarFg)
	sels := doc.Buffer.GetSelections()
	cursorInfo := fmt.Sprintf(i18n.T("status.cursor_info"), 1, 1)
	if len(sels) > 0 {
		head := sels[0].Head
		if m.splits != nil && m.splits.TotalPanes() > 1 {
			cursorInfo = fmt.Sprintf(i18n.T("status.cursor_info_split"), m.splits.ActiveIndex+1, m.splits.TotalPanes(), head.Line+1, head.Column+1)
		} else {
			cursorInfo = fmt.Sprintf(i18n.T("status.cursor_info"), head.Line+1, head.Column+1)
		}
	}

	modeStr := i18n.T("status.mode.normal")
	if m.settings != nil && m.settings.Current.VimMode && m.vimFSM != nil {
		modeStr = fmt.Sprintf("VIM [%s]", m.vimFSM.Mode)
	} else if m.sidebarFocused {
		modeStr = i18n.T("status.mode.explorer")
	}
	if m.snippetSession != nil && m.snippetSession.Active {
		cursorInfo += " | " + m.snippetSession.FormatSnippetPrompt()
	}
	gitBadge := ""
	var br string
	if m.gitWatcher != nil {
		br = m.gitWatcher.GetBranch()
	}
	if br != "" {
		gitBadge = fmt.Sprintf("  %s", br)
		if doc != nil && doc.FilePath != "" {
			var diff *git.FileGitDiff
			if m.gitWatcher != nil {
				diff = m.gitWatcher.GetFileDiff(doc.FilePath)
			}
			if diff != nil && (diff.AddedCount > 0 || diff.ModifiedCount > 0 || diff.DeletedCount > 0) {
				gitBadge += fmt.Sprintf(" [+%d ~%d -%d]", diff.AddedCount, diff.ModifiedCount, diff.DeletedCount)
			}
		}
		gitBadge += " |"
	}
	leftStatus := fmt.Sprintf(" %s |%s %s | %s", modeStr, gitBadge, filename, cursorInfo)
	if diagMsg, ok := m.getDiagnosticAtCursor(); ok && m.statusMessage == "" {
		cleanedDiag := cleanDiagnosticMessage(diagMsg)
		prefix := fmt.Sprintf(" %s | %s | %s", modeStr, filename, cursorInfo)
		leftStatus = fmt.Sprintf("%s | [ERR] %s", prefix, cleanedDiag)
	} else if m.statusMessage != "" {
		leftStatus = fmt.Sprintf(" %s", cleanDiagnosticMessage(m.statusMessage))
	}

	lspStatus := i18n.T("status.lsp_off")
	if m.lspClient != nil {
		lspStatus = i18n.T("status.lsp_active")
	}
	splitTitle := splitModeTitle(m.splits)
	langTitle := m.getToolchainLabel(doc)
	probStatus := ""
	if m.problemsPanel != nil && (m.problemsPanel.ErrorCount() > 0 || m.problemsPanel.WarningCount() > 0) {
		probStatus = fmt.Sprintf("%d ✕ %d ▲", m.problemsPanel.ErrorCount(), m.problemsPanel.WarningCount())
	}

	parts := []string{}
	if probStatus != "" {
		parts = append(parts, probStatus)
	}
	if langTitle != "" {
		parts = append(parts, langTitle)
	}
	if lspStatus != "" {
		parts = append(parts, lspStatus)
	}
	if splitTitle != "" {
		parts = append(parts, splitTitle)
	}
	parts = append(parts, "UTF-8")

	buildRight := func(p []string) string {
		if len(p) == 0 {
			return " UTF-8 "
		}
		return " " + strings.Join(p, " | ") + " "
	}

	rightStatus := buildRight(parts)
	leftW := runewidth.StringWidth(leftStatus)

	for len(parts) > 1 && leftW+runewidth.StringWidth(rightStatus) >= w {
		dropped := false
		for i, seg := range parts {
			if seg == splitTitle {
				parts = append(parts[:i], parts[i+1:]...)
				dropped = true
				break
			}
		}
		if dropped {
			rightStatus = buildRight(parts)
			continue
		}
		for i, seg := range parts {
			if seg == lspStatus {
				parts = append(parts[:i], parts[i+1:]...)
				dropped = true
				break
			}
		}
		if dropped {
			rightStatus = buildRight(parts)
			continue
		}
		for i, seg := range parts {
			if seg == langTitle {
				parts = append(parts[:i], parts[i+1:]...)
				dropped = true
				break
			}
		}
		if dropped {
			rightStatus = buildRight(parts)
			continue
		}
		break
	}

	rightRunes := []rune(rightStatus)
	rightW := runewidth.StringWidth(rightStatus)
	leftRunes := []rune(leftStatus)

	// 1. Paint entire status bar background with statusBg
	for x := 0; x < w; x++ {
		buf.SetRune(x, statusBarY, ' ', statusFg, statusBg, cell.AttrNone)
	}

	if w > 0 {
		// 2. Right-align rightStatus so UTF-8 and diagnostics are NEVER cut off
		if rightW >= w {
			col := 0
			for _, r := range rightRunes {
				rw := runewidth.RuneWidth(r)
				if col+rw > w {
					break
				}
				buf.SetRune(col, statusBarY, r, statusFg, statusBg, cell.AttrNone)
				for extra := 1; extra < rw; extra++ {
					buf.SetRune(col+extra, statusBarY, ' ', statusFg, statusBg, cell.AttrNone)
				}
				col += rw
			}
		} else {
			rightStartX := w - rightW
			col := 0
			for _, r := range rightRunes {
				rw := runewidth.RuneWidth(r)
				buf.SetRune(rightStartX+col, statusBarY, r, statusFg, statusBg, cell.AttrNone)
				for extra := 1; extra < rw; extra++ {
					buf.SetRune(rightStartX+col+extra, statusBarY, ' ', statusFg, statusBg, cell.AttrNone)
				}
				col += rw
			}

			availLeftW := rightStartX - 1
			if availLeftW > 0 {
				errIdx := strings.Index(leftStatus, "[ERR]")
				errFg := toColor(m.theme.DiagnosticError)
				col = 0
				for idx, r := range leftRunes {
					rw := runewidth.RuneWidth(r)
					if col+rw > availLeftW {
						if col < availLeftW {
							buf.SetRune(col, statusBarY, '…', statusFg, statusBg, cell.AttrNone)
						}
						break
					}
					cellFg := statusFg
					cellAttr := cell.AttrNone
					if errIdx != -1 && idx >= errIdx {
						cellFg = errFg
						cellAttr = cell.AttrBold
					}
					buf.SetRune(col, statusBarY, r, cellFg, statusBg, cellAttr)
					for extra := 1; extra < rw; extra++ {
						buf.SetRune(col+extra, statusBarY, ' ', cellFg, statusBg, cellAttr)
					}
					col += rw
				}
			}
		}
	}

	// 8. Draw Omnibar Modal Dialog (if open)
	if m.omnibarOpen {
		m.renderOmnibar(buf, w, h)
	}

	// 9. Draw Floating Completion Popup (if open)
	if m.popupVisible && !m.omnibarOpen {
		m.renderPopup(buf, w, h, editorLeft+m.gutterWidth())
	}

	// 10. Draw Tree File Operations Prompt (if open)
	if m.treePromptOpen {
		m.renderTreePrompt(buf, w, h)
	}

	// 10.5. Draw Tree Right-Click Context Menu (if open)
	if m.contextMenuOpen {
		m.renderContextMenu(buf, w, h)
	}

	// 11. Draw JetBrains-style Settings Modal (if open)
	if m.settings != nil && m.settings.Open {
		m.settings.Render(buf, w, h,
			toColor(m.theme.Background),
			toColor(m.theme.Foreground),
			toColor(m.theme.BorderColor),
			toColor(m.theme.PopupSelBg),
			toColor(m.theme.PopupSelFg),
			toColor(m.theme.Function),
		)
	}

	// 11.5. Draw Fullscreen Extension Marketplace (if open)
	if m.marketplace != nil && m.marketplace.Open {
		m.marketplace.Render(buf, w, h, m.theme)
	}

	// 11.6. Draw Quick Fix & Code Actions Popup (if open)
	if m.quickFixModal != nil && m.quickFixModal.Open {
		m.quickFixModal.Render(buf, &m.theme)
	}

	// 11.7. Draw Interactive Git Modal (if open)
	if m.gitModal != nil && m.gitModal.Open {
		m.gitModal.Render(buf, w, h, &m.theme)
	}

	// 11.8. Draw In-Editor Color Picker Modal (if open)
	if m.editorColorPicker != nil && m.editorColorPicker.Open {
		m.editorColorPicker.Render(buf, w, h, m.theme)
	}

	// 11.9. Draw Missing Tool Prompt Modal (if open)
	if m.toolPrompt != nil && m.toolPrompt.Open {
		m.toolPrompt.Render(buf, w, h, &m.theme)
	}

	// 11.95. Draw Launch Configurations Modal (if open)
	if m.launchModal != nil && m.launchModal.Open {
		m.launchModal.Render(buf, w, h, &m.theme)
	}

	// 11.96. Draw Find & Replace Modal (if open)
	if m.findReplaceModal != nil && m.findReplaceModal.Open {
		m.findReplaceModal.Render(buf, w, h, &m.theme)
	}

	// 11.97. Draw Symbol Rename Modal (if open)
	if m.renameModal != nil && m.renameModal.Open {
		m.renameModal.Render(buf, w, h, &m.theme)
	}

	// 11.98. Draw New Project Modal (if open)
	if m.newProjectModal != nil && m.newProjectModal.Open {
		m.newProjectModal.Render(buf, w, h, &m.theme)
	}

	// 11.985. Draw Search in Files Modal (if open)
	if m.searchInFilesModal != nil && m.searchInFilesModal.Open {
		m.searchInFilesModal.Render(buf, w, h, m.theme)
	}

	// 11.986. Draw Problems Panel Drawer (if open)
	if m.problemsPanel != nil && m.problemsPanel.Open {
		m.problemsPanel.Render(buf, 0, h-1-m.problemsPanel.Height, w, m.problemsPanel.Height, m.theme)
	}

	// 11.987. Draw Bug Reporter Modal (if open)
	if m.bugReportModal != nil && m.bugReportModal.Open {
		m.bugReportModal.Render(buf, w, h, &m.theme)
	}

	// 11.988. Draw Crash Recovery Dialog (if open)
	if m.crashDialog != nil && m.crashDialog.Open {
		m.crashDialog.Render(buf, w, h, &m.theme)
	}

	// 11.989. Draw Log Inspector Modal (if open)
	if m.logInspector != nil && m.logInspector.Open {
		m.logInspector.Render(buf, w, h, &m.theme)
	}

	// 11.990. Draw P2P Multi-User Collaboration Modal (if open)
	if m.p2pModal != nil && m.p2pModal.Visible {
		m.p2pModal.Render(buf, w, h)
	}

	// 11.991. Draw DB Diff Modal (if open)
	if m.dbDiffModal != nil && m.dbDiffModal.Visible {
		m.dbDiffModal.Render(buf, w, h)
	}

	// 11.992. Draw Database Connection Modal (if visible)
	if m.dbConnectModal != nil && m.dbConnectModal.Visible {
		m.dbConnectModal.Render(buf, w, h)
	}

	// 11.993. Draw DevTools Modal (if open)
	if m.devtoolsModal != nil && m.devtoolsModal.Open {
		m.devtoolsModal.Render(buf, w, h, &m.theme)
	}

	// 11.994. Draw Regex Tester Modal (if open)
	if m.regexModal != nil && m.regexModal.Open {
		m.regexModal.Render(buf, w, h, &m.theme)
	}

	// 11.995. Draw Bookmarks Modal (if open)
	if m.bookmarksModal != nil && m.bookmarksModal.Open {
		m.bookmarksModal.Render(buf, w, h, &m.theme)
	}

	// 12. Draw Dropdowns (Top layer above editor)
	if m.mainMenuOpen {
		m.renderMainMenu(buf, w, h)
	}
	if m.profileDropdownOpen {
		m.renderProfileDropdown(buf, w, h)
	}

	// 12.5. Draw Toasts (Top Right)
	if m.toasts != nil {
		m.toasts.Tick(time.Now())
		m.toasts.Draw(buf, buffer.NewRect(0, 1, w, h-1))
	}

	// 13. Draw Mouse Hover Tooltip (Top layer)
	m.renderTooltip(buf, w, h)

	// 13.5. Draw Hover Documentation Popup (Floating minimal window)
	if m.hoverDoc != nil && m.hoverDoc.Open {
		m.hoverDoc.Render(buf, w, h, &m.theme)
	}

	// 14. Terminal Hardware Cursor Management
	isModalOpen := (m.mainMenuOpen || m.profileDropdownOpen ||
		(m.launchModal != nil && m.launchModal.Open) ||
		(m.findReplaceModal != nil && m.findReplaceModal.Open) ||
		(m.renameModal != nil && m.renameModal.Open) ||
		(m.searchInFilesModal != nil && m.searchInFilesModal.Open) ||
		(m.problemsPanel != nil && m.problemsPanel.Open) ||
		(m.bugReportModal != nil && m.bugReportModal.Open) ||
		(m.crashDialog != nil && m.crashDialog.Open) ||
		(m.logInspector != nil && m.logInspector.Open) ||
		(m.toolPrompt != nil && m.toolPrompt.Open) ||
		(m.editorColorPicker != nil && m.editorColorPicker.Open) ||
		(m.gitModal != nil && m.gitModal.Open) ||
		(m.quickFixModal != nil && m.quickFixModal.Open) ||
		(m.marketplace != nil && m.marketplace.Open) ||
		(m.dbConnectModal != nil && m.dbConnectModal.Visible) ||
		(m.p2pModal != nil && m.p2pModal.Visible) ||
		(m.devtoolsModal != nil && m.devtoolsModal.Open) ||
		(m.regexModal != nil && m.regexModal.Open) ||
		(m.bookmarksModal != nil && m.bookmarksModal.Open) ||
		m.sidebarFocused || m.omnibarOpen || m.treePromptOpen)

	if !isModalOpen && m.activeCursorScreenX >= 0 && m.activeCursorScreenX < w && m.activeCursorScreenY >= 0 && m.activeCursorScreenY < h {
		cursorStyle := "block"
		if m.settings != nil && m.settings.Current.CursorStyle != "" {
			cursorStyle = strings.ToLower(m.settings.Current.CursorStyle)
		}
		shape := 1
		switch cursorStyle {
		case "bar":
			shape = 5
		case "underline":
			shape = 3
		default:
			shape = 1
		}
		f.ShowCursor(m.activeCursorScreenX, m.activeCursorScreenY, shape)
	} else {
		f.HideCursor()
	}
}

// renderContextMenu draws the floating right-click popup menu for tree items.
func (m *AppModel) renderContextMenu(buf *buffer.Buffer, w, h int) {
	if !m.contextMenuOpen || len(m.contextMenuItems) == 0 {
		return
	}

	menuW := 22
	menuH := len(m.contextMenuItems) + 2
	startX := m.contextMenuX
	startY := m.contextMenuY

	if startX+menuW >= w {
		startX = max(0, w-menuW-1)
	}
	if startY+menuH >= h {
		startY = max(0, h-menuH-1)
	}

	boxBg := toColor(m.theme.PopupBg)
	boxFg := toColor(m.theme.PopupFg)
	borderFg := toColor(m.theme.BorderColor)
	selBg := toColor(m.theme.PopupSelBg)
	selFg := toColor(m.theme.PopupSelFg)

	for y := 0; y < menuH; y++ {
		for x := 0; x < menuW; x++ {
			ch := ' '
			fg := borderFg
			if y == 0 && x == 0 {
				ch = '╭'
			} else if y == 0 && x == menuW-1 {
				ch = '╮'
			} else if y == menuH-1 && x == 0 {
				ch = '╰'
			} else if y == menuH-1 && x == menuW-1 {
				ch = '╯'
			} else if y == 0 || y == menuH-1 {
				ch = '─'
			} else if x == 0 || x == menuW-1 {
				ch = '│'
			}
			buf.SetRune(startX+x, startY+y, ch, fg, boxBg, cell.AttrNone)
		}
	}

	title := " Explorer "
	if m.contextMenuTarget != "" {
		title = fmt.Sprintf(" %s ", filepath.Base(m.contextMenuTarget))
	}
	titleRunes := []rune(title)
	if len(titleRunes) > menuW-4 {
		titleRunes = titleRunes[:menuW-4]
	}
	for i, r := range titleRunes {
		buf.SetRune(startX+2+i, startY, r, toColor(m.theme.Keyword), boxBg, cell.AttrBold)
	}

	for idx, item := range m.contextMenuItems {
		itemBg := boxBg
		itemFg := boxFg
		prefix := "  "
		attr := cell.AttrNone
		if idx == m.contextMenuSel {
			itemBg = selBg
			itemFg = selFg
			prefix = "> "
			attr = cell.AttrBold
		}
		label := fmt.Sprintf("%s%s", prefix, item.label)
		labelRunes := []rune(label)
		for x := 0; x < menuW-2; x++ {
			r := ' '
			if x < len(labelRunes) {
				r = labelRunes[x]
			}
			buf.SetRune(startX+1+x, startY+1+idx, r, itemFg, itemBg, attr)
		}
	}
}

// renderTooltip draws the miniature single-line hover badge.
func (m *AppModel) renderTooltip(buf *buffer.Buffer, w, h int) {
	if m.tooltipText == "" || m.contextMenuOpen || m.omnibarOpen || (m.settings != nil && m.settings.Open) || m.treePromptOpen {
		return
	}
	text := fmt.Sprintf(" %s ", m.tooltipText)
	runes := []rune(text)
	textLen := buffer.StringWidth(text)
	tx := m.tooltipX
	ty := m.tooltipY

	if tx+textLen >= w {
		tx = w - textLen - 1
	}
	if tx < 0 {
		tx = 0
	}
	if ty >= h {
		ty = h - 1
	}
	if ty < 0 {
		ty = 0
	}

	bg := toColor(m.theme.PopupSelBg)
	fg := toColor(m.theme.PopupSelFg)
	curX := tx
	for _, r := range runes {
		rw := buffer.RuneWidth(r)
		if curX+rw <= w {
			buf.SetRune(curX, ty, r, fg, bg, cell.AttrBold)
			curX += max(1, rw)
		}
	}
}

// renderPopup renders floating completion/hover overlays.
func (m *AppModel) renderPopup(buf *buffer.Buffer, w, h int, gutterWidth int) {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return
	}
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		return
	}

	head := sels[0].Head
	popupX := gutterWidth + (head.Column - m.viewportX)
	popupY := 2 + (head.Line - m.viewportY) + 1

	popupWidth := 45
	if popupX+popupWidth >= w {
		popupX = w - popupWidth - 1
	}
	if popupX < gutterWidth {
		popupX = gutterWidth
	}

	visibleItemsCount := len(m.popupItems)
	if visibleItemsCount > 8 {
		visibleItemsCount = 8
	}
	popupHeight := visibleItemsCount + 2
	if popupY+popupHeight >= h-1 {
		popupY = 2 + (head.Line - m.viewportY) - popupHeight
	}
	if popupY < 2 {
		popupY = 2
	}

	popupBg := toColor(m.theme.PopupBg)
	popupFg := toColor(m.theme.PopupFg)
	selBg := toColor(m.theme.PopupSelBg)
	selFg := toColor(m.theme.PopupSelFg)
	borderFg := toColor(m.theme.BorderColor)

	for y := 0; y < popupHeight; y++ {
		for x := 0; x < popupWidth; x++ {
			ch := ' '
			fg := borderFg
			if y == 0 && x == 0 {
				ch = '╭'
			} else if y == 0 && x == popupWidth-1 {
				ch = '╮'
			} else if y == popupHeight-1 && x == 0 {
				ch = '╰'
			} else if y == popupHeight-1 && x == popupWidth-1 {
				ch = '╯'
			} else if y == 0 || y == popupHeight-1 {
				ch = '─'
			} else if x == 0 || x == popupWidth-1 {
				ch = '│'
			}
			buf.SetRune(popupX+x, popupY+y, ch, fg, popupBg, cell.AttrNone)
		}
	}

	startIdx := m.popupScrollOffset
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx+visibleItemsCount > len(m.popupItems) {
		startIdx = max(0, len(m.popupItems)-visibleItemsCount)
	}

	for row := 0; row < visibleItemsCount; row++ {
		idx := startIdx + row
		if idx >= len(m.popupItems) {
			break
		}
		item := m.popupItems[idx]
		itemBg := popupBg
		itemFg := popupFg
		if idx == m.popupActive {
			itemBg = selBg
			itemFg = selFg
		}
		lineText := fmt.Sprintf(" %s", item)
		if len(lineText) > popupWidth-2 {
			lineText = lineText[:popupWidth-2]
		}
		lineText = fmt.Sprintf("%-*s", popupWidth-2, lineText)
		for i, r := range lineText {
			buf.SetRune(popupX+1+i, popupY+1+row, r, itemFg, itemBg, cell.AttrNone)
		}

		if len(m.popupItems) > 8 {
			thumbPos := (startIdx * visibleItemsCount) / len(m.popupItems)
			sbChar := '│'
			if row == thumbPos {
				sbChar = '█'
			}
			buf.SetRune(popupX+popupWidth-1, popupY+1+row, sbChar, toColor(m.theme.LineNumber), popupBg, cell.AttrNone)
		}
	}
}

// selectedText returns the string of characters currently highlighted by the primary selection.
func (m *AppModel) selectedText() string {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return ""
	}
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		return ""
	}
	s := sels[0]
	if s.IsEmpty() {
		return ""
	}
	start := s.Start()
	end := s.End()
	var sb strings.Builder
	for line := start.Line; line <= end.Line; line++ {
		lineBytes, err := doc.Buffer.GetLine(line)
		if err != nil {
			continue
		}
		runes := []rune(string(lineBytes))
		fromCol := 0
		toCol := len(runes)
		if line == start.Line {
			fromCol = start.Column
		}
		if line == end.Line {
			toCol = end.Column
		}
		if fromCol < len(runes) {
			if toCol > len(runes) {
				toCol = len(runes)
			}
			if fromCol < toCol {
				sb.WriteString(string(runes[fromCol:toCol]))
			}
		}
	}
	return sb.String()
}

// wordUnderCursor extracts the identifier under the active cursor.
func (m *AppModel) wordUnderCursor() string {
	doc := m.eng.ActiveDocument()
	if doc == nil {
		return ""
	}
	sels := doc.Buffer.GetSelections()
	if len(sels) == 0 {
		return ""
	}
	head := sels[0].Head
	lineBytes, err := doc.Buffer.GetLine(head.Line)
	if err != nil {
		return ""
	}
	runes := []rune(string(lineBytes))
	if len(runes) == 0 || head.Column > len(runes) {
		return ""
	}

	col := head.Column
	if col == len(runes) && col > 0 {
		col--
	}

	start := col
	for start > 0 && (unicode.IsLetter(runes[start-1]) || unicode.IsDigit(runes[start-1]) || runes[start-1] == '_') {
		start--
	}
	end := col
	for end < len(runes) && (unicode.IsLetter(runes[end]) || unicode.IsDigit(runes[end]) || runes[end] == '_') {
		end++
	}
	if start >= end {
		return ""
	}
	return string(runes[start:end])
}

func getMainMenuItems() []menuItem {
	return []menuItem{
		{i18n.T("menu.open_folder"), "Ctrl+O", "open_folder"},
		{i18n.T("menu.new_project"), "", "new_project"},
		{i18n.T("menu.open_file"), "Ctrl+P", "open_file"},
		{i18n.T("menu.commands"), "Ctrl+Shift+P", "commands"},
		{i18n.T("menu.save_file"), "Ctrl+S", "save_file"},
		{i18n.T("menu.marketplace"), "Ctrl+Shift+X", "marketplace"},
		{i18n.T("menu.settings"), "Ctrl+,", "settings"},
		{i18n.T("menu.report_bug"), "", "report_bug"},
		{i18n.T("menu.view_logs"), "", "view_logs"},
		{i18n.T("menu.exit"), "Ctrl+Q", "exit"},
	}
}

func (m *AppModel) executeMainMenuItem(action string) {
	switch action {
	case "report_bug":
		m.openBugReportModal()
	case "view_logs":
		m.openLogInspector()
	case "open_folder":
		m.openOmnibar("open_dir")
	case "new_project":
		if m.newProjectModal != nil {
			var templates []plugin.ProjectTemplate
			if m.pluginMgr != nil {
				templates = m.pluginMgr.GetProjectTemplates()
			}
			sdkInfo := ""
			if m.sdkManager != nil && m.sdkManager.GoSDK() != nil {
				sdkInfo = fmt.Sprintf("%s (%s)", m.sdkManager.GoSDK().Version, m.sdkManager.GoSDK().BinaryPath)
			}
			m.newProjectModal.OpenWithTemplates(templates, m.workspaceDir, sdkInfo)
		}
	case "recent":
		m.openOmnibar("find")
	case "open_file":
		m.openOmnibar("files")
	case "commands":
		m.openOmnibar("commands")
	case "save_file":
		if doc := m.eng.ActiveDocument(); doc != nil {
			target := doc.FilePath
			if target == "" {
				target = "untitled.txt"
			}
			_ = m.eng.Dispatch(core.Command{ID: core.CmdFileSave, Args: target})
			m.toasts.Success(i18n.T("toast.saved"), filepath.Base(target))
			m.statusMessage = fmt.Sprintf(i18n.T("status.saved"), target)
		}
	case "marketplace":
		if m.marketplace != nil {
			m.marketplace.Open = true
			m.marketplace.Refresh()
		}
	case "settings":
		if m.settings != nil {
			m.settings.Open = true
		}
	case "exit":
		m.quitting = true
	}
}

func (m *AppModel) renderMainMenu(buf *buffer.Buffer, w, h int) {
	if !m.mainMenuOpen {
		return
	}
	items := getMainMenuItems()
	box := m.BoxChars()

	maxLabelW := 0
	maxShortcutW := 0
	for _, item := range items {
		lw := runewidth.StringWidth(item.label)
		if lw > maxLabelW {
			maxLabelW = lw
		}
		sw := runewidth.StringWidth(item.shortcut)
		if sw > maxShortcutW {
			maxShortcutW = sw
		}
	}
	menuW := maxLabelW + maxShortcutW + 6
	if menuW < 38 {
		menuW = 38
	}
	if menuW > w-2 {
		menuW = w - 2
	}

	menuX := 0
	menuY := 1
	menuH := len(items) + 2

	bg := toColor(m.theme.PopupBg)
	fg := toColor(m.theme.Foreground)
	borderFg := toColor(m.theme.BorderColor)
	selBg := toColor(m.theme.PopupSelBg)
	selFg := toColor(m.theme.PopupSelFg)
	shortcutFg := toColor(m.theme.Comment)

	for y := menuY; y < menuY+menuH && y < h; y++ {
		for x := menuX; x < menuX+menuW && x < w; x++ {
			r := ' '
			f := fg
			b := bg
			if y == menuY && x == menuX {
				r = box.TopLeft
				f = borderFg
			} else if y == menuY && x == menuX+menuW-1 {
				r = box.TopRight
				f = borderFg
			} else if y == menuY+menuH-1 && x == menuX {
				r = box.BottomLeft
				f = borderFg
			} else if y == menuY+menuH-1 && x == menuX+menuW-1 {
				r = box.BottomRight
				f = borderFg
			} else if y == menuY || y == menuY+menuH-1 {
				r = box.Horiz
				f = borderFg
			} else if x == menuX || x == menuX+menuW-1 {
				r = box.Vert
				f = borderFg
			}
			buf.SetRune(x, y, r, f, b, cell.AttrNone)
		}
	}

	for idx, item := range items {
		rowY := menuY + 1 + idx
		if rowY >= h-1 {
			break
		}
		isSel := (idx == m.mainMenuSel)
		rowBg := bg
		rowFg := fg
		attr := cell.AttrNone
		if isSel {
			rowBg = selBg
			rowFg = selFg
			attr = cell.AttrBold
		}

		for x := menuX + 1; x < menuX+menuW-1 && x < w; x++ {
			buf.SetRune(x, rowY, ' ', rowFg, rowBg, attr)
		}

		// Draw label
		labelRunes := []rune(" " + item.label)
		col := 0
		maxLabelEnd := menuW - maxShortcutW - 4
		for _, r := range labelRunes {
			rw := runewidth.RuneWidth(r)
			if col+rw <= maxLabelEnd {
				buf.SetRune(menuX+1+col, rowY, r, rowFg, rowBg, attr)
				for extra := 1; extra < rw; extra++ {
					buf.SetRune(menuX+1+col+extra, rowY, ' ', rowFg, rowBg, attr)
				}
				col += rw
			}
		}

		// Draw shortcut right-aligned
		if item.shortcut != "" {
			scRunes := []rune(item.shortcut)
			scWidth := runewidth.StringWidth(item.shortcut)
			scX := menuX + menuW - scWidth - 2
			scFg := shortcutFg
			if isSel {
				scFg = selFg
			}
			scCol := 0
			for _, r := range scRunes {
				rw := runewidth.RuneWidth(r)
				if scX+scCol+rw < menuX+menuW-1 {
					buf.SetRune(scX+scCol, rowY, r, scFg, rowBg, attr)
					for extra := 1; extra < rw; extra++ {
						buf.SetRune(scX+scCol+extra, rowY, ' ', scFg, rowBg, attr)
					}
				}
				scCol += rw
			}
		}
	}
}

func (m *AppModel) renderProfileDropdown(buf *buffer.Buffer, w, h int) {
	if !m.profileDropdownOpen || m.launchConfig == nil {
		return
	}
	box := m.BoxChars()

	menuX := 24
	for _, btn := range m.toolbarButtons {
		if btn.id == "profile_select" {
			menuX = btn.minX
			break
		}
	}
	if menuX+34 > w {
		menuX = max(1, w-35)
	}
	menuY := 1
	menuW := 34

	profiles := m.launchConfig.Configurations
	totalItems := len(profiles) + 2
	menuH := totalItems + 2

	bg := toColor(m.theme.PopupBg)
	fg := toColor(m.theme.Foreground)
	borderFg := toColor(m.theme.BorderColor)
	selBg := toColor(m.theme.PopupSelBg)
	selFg := toColor(m.theme.PopupSelFg)
	activeFg := toColor(m.theme.String)

	for y := menuY; y < menuY+menuH && y < h; y++ {
		for x := menuX; x < menuX+menuW && x < w; x++ {
			r := ' '
			f := fg
			b := bg
			if y == menuY && x == menuX {
				r = box.TopLeft
				f = borderFg
			} else if y == menuY && x == menuX+menuW-1 {
				r = box.TopRight
				f = borderFg
			} else if y == menuY+menuH-1 && x == menuX {
				r = box.BottomLeft
				f = borderFg
			} else if y == menuY+menuH-1 && x == menuX+menuW-1 {
				r = box.BottomRight
				f = borderFg
			} else if y == menuY || y == menuY+menuH-1 {
				r = box.Horiz
				f = borderFg
			} else if x == menuX || x == menuX+menuW-1 {
				r = box.Vert
				f = borderFg
			}
			buf.SetRune(x, y, r, f, b, cell.AttrNone)
		}
	}

	activeName := ""
	if act := m.launchConfig.GetActive(); act != nil {
		activeName = act.Name
	}

	for idx := 0; idx < totalItems; idx++ {
		rowY := menuY + 1 + idx
		if rowY >= h-1 {
			break
		}
		if idx < len(profiles) {
			p := profiles[idx]
			isSel := (idx == m.profileDropdownSel)
			rowBg := bg
			rowFg := fg
			attr := cell.AttrNone
			if isSel {
				rowBg = selBg
				rowFg = selFg
				attr = cell.AttrBold
			}
			for x := menuX + 1; x < menuX+menuW-1 && x < w; x++ {
				buf.SetRune(x, rowY, ' ', rowFg, rowBg, attr)
			}
			prefix := "   "
			if p.Name == activeName {
				prefix = " ✓ "
			}
			line := prefix + p.Name
			col := 0
			for charIdx, r := range []rune(line) {
				rw := runewidth.RuneWidth(r)
				if menuX+1+col+rw < menuX+menuW-1 {
					fColor := rowFg
					if p.Name == activeName && charIdx < 3 {
						fColor = activeFg
					}
					buf.SetRune(menuX+1+col, rowY, r, fColor, rowBg, attr)
					for extra := 1; extra < rw; extra++ {
						buf.SetRune(menuX+1+col+extra, rowY, ' ', fColor, rowBg, attr)
					}
				}
				col += rw
			}
		} else if idx == len(profiles) {
			for x := menuX + 1; x < menuX+menuW-1 && x < w; x++ {
				buf.SetRune(x, rowY, box.Horiz, borderFg, bg, cell.AttrNone)
			}
		} else {
			isSel := (idx == m.profileDropdownSel)
			rowBg := bg
			rowFg := fg
			attr := cell.AttrNone
			if isSel {
				rowBg = selBg
				rowFg = selFg
				attr = cell.AttrBold
			}
			for x := menuX + 1; x < menuX+menuW-1 && x < w; x++ {
				buf.SetRune(x, rowY, ' ', rowFg, rowBg, attr)
			}
			line := "   Edit configs..."
			col := 0
			for _, r := range []rune(line) {
				rw := runewidth.RuneWidth(r)
				if menuX+1+col+rw < menuX+menuW-1 {
					buf.SetRune(menuX+1+col, rowY, r, rowFg, rowBg, attr)
					for extra := 1; extra < rw; extra++ {
						buf.SetRune(menuX+1+col+extra, rowY, ' ', rowFg, rowBg, attr)
					}
				}
				col += rw
			}
		}
	}
}

