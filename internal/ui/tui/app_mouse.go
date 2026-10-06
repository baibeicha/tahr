package tui

import (
	"encoding/json"
	"fmt"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/baibeicha/goatui/pkg/core/buffer"
	"github.com/baibeicha/goatui/pkg/driver/input"
	"github.com/baibeicha/goatui/pkg/tea"
	"github.com/mattn/go-runewidth"

	"tahr/internal/core"
	corebuf "tahr/internal/core/buffer"
	"tahr/internal/core/clipboard"
	"tahr/internal/core/db"
	"tahr/internal/core/i18n"
	"tahr/internal/core/plugin"
)

func (m *AppModel) computeTermHeaderBtnHitboxes(w int) []termHeaderBtnHitbox {
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

	var hitboxes []termHeaderBtnHitbox
	curRightX := w - 2

	for _, b := range rButtons {
		btnW := runewidth.StringWidth(b.text)
		btnStartX := curRightX - btnW
		if btnStartX < 10 {
			break
		}
		hitboxes = append(hitboxes, termHeaderBtnHitbox{
			id:   b.id,
			minX: btnStartX,
			maxX: btnStartX + btnW - 1,
		})
		curRightX = btnStartX - 1 // 1 column spacing between buttons
	}
	return hitboxes
}

// handleMouse processes terminal mouse events with SGR-1006 coordinate mapping.
func (m *AppModel) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// 0. Startup splash screen click dismiss
	if m.splash != nil && m.splash.Active {
		m.splash.HandleMouse(msg.Mouse)
		return m, nil
	}

	// 0.0 Crash recovery dialog
	if m.crashDialog != nil && m.crashDialog.Open && msg.Action == input.MousePress {
		consumed, action := m.crashDialog.HandleMouse(msg.Mouse, m.width, m.height)
		if consumed {
			m.handleCrashDialogAction(action)
			return m, nil
		}
	}

	// 0.00 Log & Data Inspector
	if m.logInspector != nil && m.logInspector.Open {
		if m.logInspector.HandleMouse(msg.Mouse, m.width, m.height) {
			return m, nil
		}
	}

	// 0.000 Bug Reporter Modal
	if m.bugReportModal != nil && m.bugReportModal.Open && msg.Action == input.MousePress {
		consumed, action := m.bugReportModal.HandleMouse(msg.Mouse, m.width, m.height)
		if consumed {
			m.handleBugReportAction(action)
			return m, nil
		}
	}

	// 0.0001 Database Connection Modal
	if m.dbConnectModal != nil && m.dbConnectModal.Visible {
		consumed, action := m.dbConnectModal.HandleMouse(msg.Mouse, m.width, m.height)
		if consumed {
			if action == "connect" {
				p := m.dbConnectModal.CurrentProfile()
				m.activeDBConnection = &p
			}
			return m, nil
		}
	}

	// Sidebar dynamic width dragging
	if m.sidebarDragging {
		if msg.Action == input.MouseDrag {
			diff := msg.X - m.sidebarDragStartX
			if m.settings != nil && m.settings.Current.TreePosition == "right" {
				diff = m.sidebarDragStartX - msg.X
			}
			newW := m.sidebarDragStartW + diff
			minW := 12
			maxW := max(16, m.width/2)
			if newW < minW {
				newW = minW
			}
			if newW > maxW {
				newW = maxW
			}
			m.sidebarWidth = newW
			m.sidebarTargetWidth = float64(newW)
			m.sidebarAnimWidth = float64(newW)
			if m.settings != nil {
				m.settings.Current.SidebarWidth = newW
			}
			return m, nil
		}
		if msg.Action == input.MouseRelease {
			m.sidebarDragging = false
			if m.settings != nil {
				_ = m.settings.Save()
			}
			return m, nil
		}
	}
	// Right sidebar dynamic width dragging
	if m.rightSidebarDragging {
		if msg.Action == input.MouseDrag {
			diff := m.rightSidebarDragStartX - msg.X
			newW := m.rightSidebarDragStartW + diff
			minW := 16
			maxW := max(20, m.width*3/4)
			if newW < minW {
				newW = minW
			}
			if newW > maxW {
				newW = maxW
			}
			m.rightSidebarWidth = newW
			m.rightSidebarTargetWidth = float64(newW)
			m.rightSidebarAnimWidth = float64(newW)
			return m, nil
		}
		if msg.Action == input.MouseRelease {
			m.rightSidebarDragging = false
			return m, nil
		}
	}
	if msg.Action == input.MouseRelease {
		m.sidebarDragging = false
		m.rightSidebarDragging = false
		m.canvasDragging = false
	}

	// Terminal drawer border dragging
	if m.termDragging {
		if msg.Action == input.MouseDrag {
			diff := m.termDragStartY - msg.Y
			newH := m.termDragStartH + diff
			minH := 4
			maxH := max(4, m.height-6)
			if newH < minH {
				newH = minH
			}
			if newH > maxH {
				newH = maxH
			}
			if m.terminal != nil {
				m.terminal.Height = newH
			}
			return m, nil
		}
		if msg.Action == input.MouseRelease {
			m.termDragging = false
			return m, nil
		}
	}
	if msg.Action == input.MouseRelease {
		m.termDragging = false
	}

	// Split pane header close button ✕ click (takes priority over toast notifications)
	if m.splits != nil && m.splits.TotalPanes() > 1 && msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		for i := range m.splits.Panes {
			p := &m.splits.Panes[i]
			if p.Bounds.Width > 0 && p.Bounds.Height > 0 {
				if msg.Y == p.Bounds.Y && msg.X >= p.Bounds.X+p.Bounds.Width-3 && msg.X < p.Bounds.X+p.Bounds.Width {
					m.closeSplitPane(p.Index)
					return m, nil
				}
			}
		}
	}

	// Click to dismiss toast notifications
	if m.toasts != nil && m.toasts.Count() > 0 && msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		stripRightW := 0
		if m.width >= 70 {
			stripRightW = 3
		}
		rightSideW := 0
		if m.rightSidebarOpen {
			rightSideW = m.rightSidebarWidth
			if rightSideW <= 0 {
				rightSideW = 34
			}
		}
		isRightStrip := stripRightW > 0 && msg.X >= m.width-stripRightW
		isRightSidebar := m.rightSidebarOpen && rightSideW > 0 && msg.X >= m.width-stripRightW-rightSideW && msg.X < m.width-stripRightW
		if !isRightStrip && !isRightSidebar {
			if m.toasts.HandleClick(msg.X, msg.Y, buffer.Rect{X: 0, Y: 0, Width: m.width, Height: m.height}) {
				return m, nil
			}
		}
	}

	// Omnibar mouse wheel and click interaction
	if m.omnibarOpen {
		modalWidth := 60
		if modalWidth > m.width-4 {
			modalWidth = m.width - 4
		}
		modalHeight := 12
		startX := (m.width - modalWidth) / 2
		startY := (m.height - modalHeight) / 2
		maxItems := modalHeight - 4

		// Mouse wheel inside Omnibar
		if msg.X >= startX && msg.X < startX+modalWidth && msg.Y >= startY && msg.Y < startY+modalHeight {
			if msg.Button == input.MouseWheelUp {
				if m.omnibarSel > 0 {
					m.omnibarSel--
				}
				return m, nil
			}
			if msg.Button == input.MouseWheelDown {
				if m.omnibarSel+1 < len(m.omnibarItems) {
					m.omnibarSel++
				}
				return m, nil
			}
			// Click on item in list
			if msg.Action == input.MousePress && msg.Button == input.MouseLeft {
				listStartY := startY + 3
				if msg.Y >= listStartY && msg.Y < listStartY+maxItems {
					clickedIdx := m.omnibarScroll + (msg.Y - listStartY)
					if clickedIdx >= 0 && clickedIdx < len(m.omnibarItems) {
						m.omnibarSel = clickedIdx
						return m.handleOmnibarKey(input.Key{Type: input.KeyEnter})
					}
				}
			}
			return m, nil
		}
		// Click outside Omnibar closes it
		if msg.Action == input.MousePress && msg.Button == input.MouseLeft {
			m.omnibarOpen = false
			return m, nil
		}
	}

	// Find & Replace Modal mouse interaction
	if m.findReplaceModal != nil && m.findReplaceModal.Open && msg.Action == input.MousePress {
		consumed, action := m.findReplaceModal.HandleClick(msg.X, msg.Y, m.width, m.height)
		if consumed {
			return m.handleFindReplaceAction(action)
		}
	}

	// Rename Modal mouse interaction
	if m.renameModal != nil && m.renameModal.Open && msg.Action == input.MousePress {
		consumed, confirmed := m.renameModal.HandleClick(msg.X, msg.Y, m.width, m.height)
		if consumed {
			if confirmed {
				return m.executeSymbolRename(m.renameModal.OldName, m.renameModal.NewName, m.renameModal.Line, m.renameModal.Col, m.renameModal.URI)
			}
			return m, nil
		}
	}

	// New Project Modal mouse interaction
	if m.newProjectModal != nil && m.newProjectModal.Open && msg.Action == input.MousePress {
		consumed, _ := m.newProjectModal.HandleClick(msg.X, msg.Y, m.width, m.height)
		if consumed {
			return m, nil
		}
	}

	// DevTools Modal mouse interaction
	if m.devtoolsModal != nil && m.devtoolsModal.Open && msg.Action == input.MousePress {
		if m.devtoolsModal.HandleClick(msg.X, msg.Y) {
			return m, nil
		}
	}

	// Regex Modal mouse interaction
	if m.regexModal != nil && m.regexModal.Open && msg.Action == input.MousePress {
		if m.regexModal.HandleClick(msg.X, msg.Y) {
			return m, nil
		}
	}

	// Bookmarks Modal mouse interaction
	if m.bookmarksModal != nil && m.bookmarksModal.Open && msg.Action == input.MousePress {
		if m.bookmarksModal.HandleClick(msg.X, msg.Y) {
			return m, nil
		}
	}

	// Mouse motion updates hover tooltips and hover documentation
	if msg.Action == input.MouseMotion || (msg.Action == input.MouseDrag && msg.Button == input.MouseNone) {
		m.updateTooltip(msg.X, msg.Y)
		m.updateHoverDoc(msg.X, msg.Y)
		return m, nil
	}

	// Any mouse click dismisses lingering tooltip and hover documentation
	m.tooltipText = ""
	if m.hoverDoc != nil {
		m.hoverDoc.Dismiss()
	}

	// -0.3 Main Menu dropdown mouse clicks
	if m.mainMenuOpen && msg.Action == input.MousePress {
		if msg.Button == input.MouseLeft {
			menuW := 36
			items := getMainMenuItems()
			menuH := len(items) + 2
			if msg.X >= 1 && msg.X <= 1+menuW && msg.Y >= 1 && msg.Y < 1+menuH {
				idx := msg.Y - 2
				if idx >= 0 && idx < len(items) {
					m.mainMenuSel = idx
					m.executeMainMenuItem(items[idx].action)
					m.mainMenuOpen = false
					return m, nil
				}
			}
		}
		m.mainMenuOpen = false
		return m, nil
	}

	// -0.2 Profile selector dropdown mouse clicks
	if m.profileDropdownOpen && msg.Action == input.MousePress {
		if msg.Button == input.MouseLeft && m.launchConfig != nil {
			menuX := 24
			for _, btn := range m.toolbarButtons {
				if btn.id == "profile_select" {
					menuX = btn.minX
					break
				}
			}
			if menuX+34 > m.width {
				menuX = max(1, m.width-35)
			}
			menuW := 34
			profiles := m.launchConfig.Configurations
			totalItems := len(profiles) + 2
			menuH := totalItems + 2

			if msg.X >= menuX && msg.X <= menuX+menuW && msg.Y >= 1 && msg.Y < 1+menuH {
				idx := msg.Y - 2
				if idx >= 0 && idx < len(profiles) {
					m.launchConfig.SetActive(profiles[idx].Name)
					_ = m.launchConfig.Save()
					m.toasts.Info("PROFILE", fmt.Sprintf("Active: %s", profiles[idx].Name))
					m.profileDropdownOpen = false
					return m, nil
				} else if idx == len(profiles)+1 {
					m.profileDropdownOpen = false
					m.openLaunchConfigModal()
					return m, nil
				}
			}
		}
		m.profileDropdownOpen = false
		return m, nil
	}

	// -0.1 Launch Config modal mouse interception
	if m.launchModal != nil && m.launchModal.Open && msg.Action == input.MousePress {
		handled, shouldClose := m.launchModal.HandleClick(msg.X, msg.Y, m.width, m.height)
		if shouldClose {
			m.launchModal.Open = false
		}
		if handled {
			return m, nil
		}
	}

	// 0. Dismiss or execute open context menu
	if m.contextMenuOpen && msg.Action == input.MousePress {
		menuW := 22
		menuH := len(m.contextMenuItems) + 2
		if msg.Button == input.MouseLeft &&
			msg.X >= m.contextMenuX && msg.X < m.contextMenuX+menuW &&
			msg.Y >= m.contextMenuY && msg.Y < m.contextMenuY+menuH {
			clickedIdx := msg.Y - (m.contextMenuY + 1)
			if clickedIdx >= 0 && clickedIdx < len(m.contextMenuItems) {
				action := m.contextMenuItems[clickedIdx].action
				m.contextMenuOpen = false
				return m.executeContextMenuAction(action)
			}
		}
		m.contextMenuOpen = false
		return m, nil
	}

	editorTop := 2
	isTreeRight := (m.settings != nil && m.settings.Current.TreePosition == "right")
	stripLeftW := 0
	stripRightW := 0
	if m.width >= 70 {
		stripLeftW = 3
		stripRightW = 3
	}
	sideW := 0
	if m.sidebarOpen {
		sideW = m.sidebarWidth
	}
	rightSideW := 0
	if m.rightSidebarOpen {
		rightSideW = m.rightSidebarWidth
		if rightSideW <= 0 {
			rightSideW = 34
		}
	}
	treeStartX := stripLeftW
	if isTreeRight {
		treeStartX = m.width - stripRightW - sideW
	}

	editorLeft := stripLeftW
	editorRight := m.width - stripRightW
	if isTreeRight {
		if sideW > 0 {
			editorRight = treeStartX - 1
		}
	} else {
		if sideW > 0 {
			editorLeft = stripLeftW + sideW + 1
		}
		if rightSideW > 0 {
			editorRight = m.width - stripRightW - rightSideW - 1
		} else {
			editorRight = m.width - stripRightW
		}
	}

	// Sidebar boundary divider dragging initiation
	if sideW > 0 && msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		dividerX := stripLeftW + sideW
		if isTreeRight {
			dividerX = treeStartX - 1
		}
		if msg.X == dividerX && msg.Y >= editorTop && msg.Y < m.height-1 {
			m.sidebarDragging = true
			m.sidebarDragStartX = msg.X
			m.sidebarDragStartW = m.sidebarWidth
			return m, nil
		}
	}

	// Right sidebar boundary divider dragging initiation
	if rightSideW > 0 && msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		rightDividerX := m.width - stripRightW - rightSideW - 1
		if (msg.X == rightDividerX || msg.X == rightDividerX+1) && msg.Y >= editorTop && msg.Y < m.height-1 {
			m.rightSidebarDragging = true
			m.rightSidebarDragStartX = msg.X
			m.rightSidebarDragStartW = m.rightSidebarWidth
			return m, nil
		}
	}

	// 0.5. Right-Click Context Menu in Tree Area
	if msg.Button == input.MouseRight && msg.Action == input.MousePress {
		sidebarMaxY := editorTop + 1 + m.visibleTreeHeight()
		isTreeClick := (sideW > 0 && msg.X >= treeStartX && msg.X < treeStartX+sideW && msg.Y >= editorTop && msg.Y < sidebarMaxY)
		if isTreeClick {
			rowInSidebar := msg.Y - editorTop - 1
			nodeIdx := m.sidebarScrollY + rowInSidebar
			if rowInSidebar >= 0 && nodeIdx >= 0 && nodeIdx < len(m.treeFlat) {
				m.treeSel = nodeIdx
				node := m.treeFlat[nodeIdx]
				m.contextMenuTarget = node.Path
				m.contextMenuItems = []contextMenuItem{
					{label: i18n.T("ctx.new_file"), action: "new_file"},
					{label: i18n.T("ctx.new_dir"), action: "new_dir"},
					{label: i18n.T("ctx.rename"), action: "rename"},
					{label: i18n.T("ctx.move"), action: "move"},
					{label: i18n.T("ctx.copy_path"), action: "copy_path"},
					{label: i18n.T("ctx.delete"), action: "delete"},
				}
			} else {
				m.contextMenuTarget = ""
				m.contextMenuItems = []contextMenuItem{
					{label: i18n.T("ctx.new_file"), action: "new_file"},
					{label: i18n.T("ctx.new_dir"), action: "new_dir"},
					{label: i18n.T("ctx.refresh"), action: "refresh"},
				}
			}
			m.contextMenuOpen = true
			m.contextMenuSel = 0
			m.contextMenuX = msg.X
			if m.contextMenuX+22 >= m.width {
				m.contextMenuX = max(0, m.width-23)
			}
			m.contextMenuY = msg.Y
			if m.contextMenuY+len(m.contextMenuItems)+2 >= m.height {
				m.contextMenuY = max(1, m.height-len(m.contextMenuItems)-3)
			}
			return m, nil
		}
	}

	// 0.6 DAP HUD mouse clicks
	if m.dapHUD != nil && m.dapHUD.Open && msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		btnID := m.dapHUD.FindDAPHUDBtn(msg.X, msg.Y)
		if btnID != "" {
			switch btnID {
			case "dap_cont":
				if m.dapSession != nil && m.dapSession.IsStopped() {
					m.stoppedMarkerLine = -1
					_ = m.dapSession.Continue()
					m.toasts.Info("DEBUGGER", "Continue")
				}
			case "dap_over":
				if m.dapSession != nil && m.dapSession.IsStopped() {
					_ = m.dapSession.StepOver()
					m.toasts.Info("DEBUGGER", "Step Over")
				}
			case "dap_into":
				if m.dapSession != nil && m.dapSession.IsStopped() {
					_ = m.dapSession.StepInto()
					m.toasts.Info("DEBUGGER", "Step Into")
				}
			case "dap_out":
				if m.dapSession != nil && m.dapSession.IsStopped() {
					_ = m.dapSession.StepOut()
					m.toasts.Info("DEBUGGER", "Step Out")
				}
			case "dap_stop":
				if m.dapSession != nil {
					_ = m.dapSession.Stop()
					m.stoppedMarkerLine = -1
					m.dapHUD.Open = false
					m.toasts.Info("DEBUGGER", "Stopped")
				}
			case "dap_close":
				m.dapHUD.Open = false
			case "tab_variables":
				m.dapHUD.ActiveTab = HUDTabVariables
			case "tab_stack":
				m.dapHUD.ActiveTab = HUDTabStack
			case "tab_watch":
				m.dapHUD.ActiveTab = HUDTabWatch
			}
			return m, nil
		}
	}

	// 0.63 Tool Prompt modal intercepts mouse clicks
	if m.toolPrompt != nil && m.toolPrompt.Open {
		if msg.Action == input.MousePress {
			m.toolPrompt.HandleClick(msg.X, msg.Y, m.width, m.height)
		}
		return m, nil
	}

	// 0.64 Git modal intercepts mouse clicks, drag, and wheel
	if m.gitModal != nil && m.gitModal.Open {
		if msg.Action == input.MousePress {
			m.gitModal.HandleClick(msg.X, msg.Y, m.width, m.height)
			if m.gitModal.DetailOpen && m.gitModal.AnimProgress < 1.0 {
				return m, func() tea.Msg { return animTickMsg{} }
			}
		} else if msg.Action == input.MouseDrag {
			m.gitModal.HandleDrag(msg.X, msg.Y, m.width, m.height)
		} else if msg.Action == input.MouseRelease {
			m.gitModal.HandleRelease()
		} else if msg.Button == input.MouseWheelUp {
			m.gitModal.HandleWheel(msg.X, msg.Y, -1, 0, m.width, m.height)
		} else if msg.Button == input.MouseWheelDown {
			m.gitModal.HandleWheel(msg.X, msg.Y, 1, 0, m.width, m.height)
		} else if msg.Button == input.MouseWheelLeft {
			m.gitModal.HandleWheel(msg.X, msg.Y, 0, -4, m.width, m.height)
		} else if msg.Button == input.MouseWheelRight {
			m.gitModal.HandleWheel(msg.X, msg.Y, 0, 4, m.width, m.height)
		}
		return m, nil
	}

	// 0.642 Marketplace modal intercepts mouse clicks
	if m.marketplace != nil && m.marketplace.Open {
		if msg.Action == input.MousePress {
			m.marketplace.HandleClick(msg.X, msg.Y, m.width, m.height)
		}
		return m, nil
	}

	// 0.645 Settings modal intercepts mouse clicks and continuous drag
	if m.settings != nil && m.settings.Open {
		if msg.Action == input.MousePress {
			_, shouldClose := m.settings.HandleClick(msg.X, msg.Y, m.width, m.height)
			if shouldClose {
				m.applyCurrentSettings()
				m.toasts.Success("SETTINGS", "Settings applied")
			}
		} else if msg.Action == input.MouseDrag && m.settings.ColorPicker != nil && m.settings.ColorPicker.Open {
			m.settings.ColorPicker.HandleDrag(msg.X, msg.Y, m.width, m.height)
		}
		return m, nil
	}

	// 0.65 Quick Fix popup intercepts mouse clicks
	if m.quickFixModal != nil && m.quickFixModal.Open && msg.Action == input.MousePress {
		applied, action, closed := m.quickFixModal.HandleClick(msg.X, msg.Y)
		if closed {
			m.quickFixModal.Close()
		}
		if applied && action != nil {
			m.applyCodeAction(action)
		}
		return m, nil
	}

	// 0.66 Editor Color Picker modal intercepts mouse clicks and continuous drag
	if m.editorColorPicker != nil && m.editorColorPicker.Open {
		if msg.Action == input.MousePress {
			handled, shouldClose := m.editorColorPicker.HandleClick(msg.X, msg.Y, m.width, m.height)
			if shouldClose {
				m.editorColorPicker.Open = false
			}
			if handled {
				return m, nil
			}
		} else if msg.Action == input.MouseDrag {
			if m.editorColorPicker.HandleDrag(msg.X, msg.Y, m.width, m.height) {
				return m, nil
			}
		}
	}

	// 0.7 Terminal drawer mouse clicks
	if m.terminal != nil && m.terminal.Open && msg.Action == input.MousePress {
		termTop := m.height - 1 - m.terminal.Height
		if msg.X >= stripLeftW && msg.Y >= termTop && msg.Y < m.height-1 {
			m.terminalFocused = true
			m.sidebarFocused = false
			if msg.Y == termTop {
				// Mouse wheel on terminal header: resize height
				if msg.Button == input.MouseWheelUp {
					maxH := max(4, m.height-6)
					m.terminal.Height = min(maxH, m.terminal.Height+2)
					return m, nil
				}
				if msg.Button == input.MouseWheelDown {
					m.terminal.Height = max(4, m.terminal.Height-2)
					return m, nil
				}

				// Middle click to close tab
				if msg.Button == input.MouseMiddle {
					for _, hit := range m.termTabHitboxes {
						if hit.instanceIdx >= 0 && hit.instanceIdx < len(m.terminal.Instances) {
							if msg.X >= hit.minX && msg.X <= hit.maxX {
								m.terminal.CloseInstance(hit.instanceIdx)
								return m, nil
							}
						}
					}
					return m, nil
				}
				// Right click to start rename
				if msg.Button == input.MouseRight {
					for _, hit := range m.termTabHitboxes {
						if hit.instanceIdx >= 0 && hit.instanceIdx < len(m.terminal.Instances) {
							if msg.X >= hit.minX && msg.X <= hit.maxX {
								m.terminal.StartRename(hit.instanceIdx)
								return m, nil
							}
						}
					}
					return m, nil
				}
				// Left click
				if msg.Button == input.MouseLeft {
					if len(m.termHeaderBtnHitboxes) == 0 {
						m.termHeaderBtnHitboxes = m.computeTermHeaderBtnHitboxes(m.width)
					}
					handledBtn := false
					for _, hb := range m.termHeaderBtnHitboxes {
						if msg.X >= hb.minX && msg.X <= hb.maxX {
							handledBtn = true
							switch hb.id {
							case "close":
								m.terminal.Open = false
								m.terminalFocused = false
								return m, nil
							case "kill":
								m.terminal.Kill()
								m.toasts.Warn("TERMINAL", "Process terminated")
								return m, nil
							case "clear":
								m.terminal.Clear()
								m.toasts.Info("TERMINAL", "Output cleared")
								return m, nil
							case "down":
								m.terminal.Height = max(4, m.terminal.Height-3)
								return m, nil
							case "up":
								maxH := max(4, m.height-6)
								m.terminal.Height = min(maxH, m.terminal.Height+3)
								return m, nil
							}
						}
					}
					if !handledBtn {
						if msg.X >= m.width-22 && msg.X <= m.width-17 {
							maxH := max(4, m.height-6)
							m.terminal.Height = min(maxH, m.terminal.Height+3)
							return m, nil
						}
						if msg.X >= m.width-17 && msg.X <= m.width-15 {
							m.terminal.Height = max(4, m.terminal.Height-3)
							return m, nil
						}
					}

					now := time.Now()
					for _, hit := range m.termTabHitboxes {
						if hit.isAdd && msg.X >= hit.minX && msg.X <= hit.maxX {
							m.terminal.AddInstance("")
							m.toasts.Info("TERMINAL", "New terminal shell added")
							return m, tickTerm()
						}
						if hit.isSplit && msg.X >= hit.minX && msg.X <= hit.maxX {
							m.terminal.ToggleSplit()
							return m, nil
						}
						if hit.instanceIdx >= 0 && hit.instanceIdx < len(m.terminal.Instances) {
							if msg.X >= hit.closeMinX && msg.X <= hit.closeMaxX {
								m.terminal.CloseInstance(hit.instanceIdx)
								return m, nil
							}
							if msg.X >= hit.minX && msg.X <= hit.maxX {
								if m.lastTabClickIdx == hit.instanceIdx && now.Sub(m.lastTabClickTime) < 400*time.Millisecond {
									m.terminal.StartRename(hit.instanceIdx)
								} else {
									m.terminal.SwitchInstance(hit.instanceIdx)
								}
								m.lastTabClickTime = now
								m.lastTabClickIdx = hit.instanceIdx
								return m, nil
							}
						}
					}

					// Left click on header border outside of tabs/buttons initiates vertical resizing
					m.termDragging = true
					m.termDragStartY = msg.Y
					m.termDragStartH = m.terminal.Height
					return m, nil
				}
				return m, nil
			}

			// Mouse wheel inside terminal body: scroll terminal history
			if msg.Y > termTop {
				if msg.Button == input.MouseWheelUp {
					m.terminal.VTerm.Scroll(3)
					return m, nil
				}
				if msg.Button == input.MouseWheelDown {
					m.terminal.VTerm.Scroll(-3)
					return m, nil
				}
			}

			// Click inside terminal body: split pane focus
			m.termDragging = false
			if msg.Button == input.MouseLeft && msg.Y > termTop {
				innerW := max(1, m.width-2)
				innerH := max(1, m.terminal.Height-1)
				if m.terminal.SplitMode == TermSplitHorizontal && len(m.terminal.Instances) >= 2 {
					colW := (innerW - 1) / 2
					if msg.X > 1+colW {
						m.terminal.SwitchInstance(1)
					} else {
						m.terminal.SwitchInstance(0)
					}
				} else if m.terminal.SplitMode == TermSplitVertical && len(m.terminal.Instances) >= 2 {
					rowH := (innerH - 1) / 2
					if msg.Y > termTop+1+rowH {
						m.terminal.SwitchInstance(1)
					} else {
						m.terminal.SwitchInstance(0)
					}
				}
			}
			return m, nil
		}
	}

	// 0.8 Minimap click-to-scroll
	if msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		if !m.rightSidebarOpen || msg.X < m.width-m.rightSidebarWidth {
			for _, mm := range m.minimapHitboxes {
				if msg.X >= mm.minX && msg.X <= mm.maxX && msg.Y >= mm.minY && msg.Y <= mm.maxY {
					targetLine := MinimapHitTest(msg.Y, mm.minY, mm.maxY-mm.minY+1, mm.totalLines)
					if mm.paneIdx == m.splits.ActivePaneIndex() {
						m.viewportY = targetLine
					}
					if pane := m.splits.PaneAt(mm.paneIdx); pane != nil {
						pane.ViewportY = targetLine
					}
					return m, nil
				}
			}
		}
	}

	// 1. Mouse click on Top Header Toolbar (Row 0)
	if msg.Y == 0 && msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		for _, btn := range m.toolbarButtons {
			if msg.X >= btn.minX && msg.X <= btn.maxX {
				switch btn.id {
				case "menu":
					m.mainMenuOpen = !m.mainMenuOpen
					return m, nil
				case "find_files":
					m.openOmnibar("files")
					return m, nil
				case "tree":
					cmd := m.ToggleSidebar()
					m.statusMessage = fmt.Sprintf("Project Tree: %v", m.sidebarOpen)
					return m, cmd
				case "split":
					if m.splits != nil {
						m.splits.CycleLayout()
						m.toasts.Info("SPLIT", m.splits.ModeTitle())
						m.statusMessage = fmt.Sprintf("Split: %s", m.splits.ModeTitle())
					}
					return m, nil
				case "profile_select":
					m.profileDropdownOpen = !m.profileDropdownOpen
					return m, nil
				case "run":
					m.RunActive()
				case "build":
					m.BuildActive()
				case "term":
					if m.terminal != nil {
						m.terminal.Toggle()
						m.toasts.Info("TERMINAL", fmt.Sprintf("Terminal: %v", m.terminal.Open))
					}
					return m, nil
				case "hud":
					if m.launchConfig != nil && len(m.launchConfig.Configurations) > 0 {
						if act := m.launchConfig.GetActive(); act != nil {
							m.debugProfile(*act)
							return m, nil
						}
					}
					if m.dapHUD != nil {
						m.dapHUD.Toggle()
						m.toasts.Info("DEBUGGER", fmt.Sprintf("Debug HUD: %v", m.dapHUD.Open))
					}
					return m, nil
				case "settings":
					if m.settings != nil {
						m.settings.Open = true
					}
					return m, nil
				}
				return m, nil
			}
		}
		return m, nil
	}

	// 1.2 Tree header resize & dock buttons [◀][▶][⇄] (Row editorTop)
	if sideW > 0 && msg.Y == editorTop && msg.X >= treeStartX && msg.X < treeStartX+sideW {
		if msg.Action == input.MousePress {
			if msg.Button == input.MouseWheelUp {
				maxW := max(16, m.width/2)
				m.sidebarWidth = min(maxW, m.sidebarWidth+2)
				m.sidebarTargetWidth = float64(m.sidebarWidth)
				m.sidebarAnimWidth = float64(m.sidebarWidth)
				if m.settings != nil {
					m.settings.Current.SidebarWidth = m.sidebarWidth
					_ = m.settings.Save()
				}
				return m, nil
			}
			if msg.Button == input.MouseWheelDown {
				m.sidebarWidth = max(12, m.sidebarWidth-2)
				m.sidebarTargetWidth = float64(m.sidebarWidth)
				m.sidebarAnimWidth = float64(m.sidebarWidth)
				if m.settings != nil {
					m.settings.Current.SidebarWidth = m.sidebarWidth
					_ = m.settings.Save()
				}
				return m, nil
			}
			if msg.Button == input.MouseLeft {
				btnCount := 5
				if sideW < 18 {
					btnCount = 1
				}
				dockBtnStart := treeStartX + sideW - btnCount
				if sideW >= 18 && (msg.X == dockBtnStart || msg.X == dockBtnStart+1) { // ◀
					m.sidebarWidth = max(12, m.sidebarWidth-2)
					m.sidebarTargetWidth = float64(m.sidebarWidth)
					m.sidebarAnimWidth = float64(m.sidebarWidth)
					if m.settings != nil {
						m.settings.Current.SidebarWidth = m.sidebarWidth
						_ = m.settings.Save()
					}
					return m, nil
				}
				if sideW >= 18 && (msg.X == dockBtnStart+2 || msg.X == dockBtnStart+3) { // ▶
					maxW := max(16, m.width/2)
					m.sidebarWidth = min(maxW, m.sidebarWidth+2)
					m.sidebarTargetWidth = float64(m.sidebarWidth)
					m.sidebarAnimWidth = float64(m.sidebarWidth)
					if m.settings != nil {
						m.settings.Current.SidebarWidth = m.sidebarWidth
						_ = m.settings.Save()
					}
					return m, nil
				}
				if msg.X >= dockBtnStart+min(4, btnCount-1) && msg.X < treeStartX+sideW { // ⇄
					if m.settings != nil {
						if m.settings.Current.TreePosition == "left" {
							m.settings.Current.TreePosition = "right"
						} else {
							m.settings.Current.TreePosition = "left"
						}
						_ = m.settings.Save()
						m.toasts.Info("TREE DOCK", fmt.Sprintf("Docked to %s", m.settings.Current.TreePosition))
					}
					return m, nil
				}
			}
		}
	}

	// 1.5. Mouse click on Tab Bar (Row 1)
	if msg.Y == 1 && msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		for _, hit := range m.tabHitboxes {
			if msg.X >= hit.closeX-1 && msg.X <= hit.closeX+1 {
				m.eng.CloseBuffer(hit.docID)
				m.onActiveDocumentChanged()
				m.toasts.Info("CLOSED", "Buffer closed")
				return m, nil
			} else if msg.X >= hit.minX && msg.X <= hit.maxX {
				_ = m.eng.SwitchBuffer(hit.docID)
				m.onActiveDocumentChanged()
				return m, nil
			}
		}
		return m, nil
	}

	// 1.8. Side activity strip clicks (Left Strip)
	if stripLeftW > 0 && msg.X < stripLeftW && msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		switch msg.Y {
		case 2: // Project Explorer EX
			if !m.sidebarOpen {
				m.sidebarOpen = true
				m.sidebarMode = 0
			} else {
				if m.sidebarMode == 0 {
					m.sidebarOpen = false
				} else {
					m.sidebarMode = 0
				}
			}
			m.clampSidebar()
			return m, m.animateSidebar()
		case 4: // Structure ST
			if !m.sidebarOpen {
				m.sidebarOpen = true
				m.sidebarMode = 1
				m.refreshDocumentStructure()
			} else {
				if m.sidebarMode == 1 {
					m.sidebarOpen = false
				} else {
					m.sidebarMode = 1
					m.refreshDocumentStructure()
				}
			}
			return m, m.animateSidebar()
		case 6: // Version Control ⎇
			m.openGitModal()
			return m, nil
		case 8: // Plugins Marketplace PL
			if m.marketplace != nil {
				m.marketplace.Open = true
				m.marketplace.Refresh()
			}
			return m, nil
		default:
			if msg.Y >= m.height-3 && msg.Y <= m.height-1 { // Settings ⚙
				if m.settings != nil {
					m.settings.Open = true
				}
				return m, nil
			}
		}
	}

	// 1.85. Right Activity Strip clicks (Deduplicated tool windows from getRightStripItems)
	if stripRightW > 0 && msg.X >= m.width-stripRightW && msg.Action == input.MousePress && msg.Button == input.MouseLeft {
		items := m.getRightStripItems()
		if msg.Y >= 2 && msg.Y%2 == 0 {
			idx := (msg.Y - 2) / 2
			if idx >= 0 && idx < len(items) {
				return m, m.ToggleRightSidebar(items[idx].mode)
			}
		}
		if msg.Y >= m.height-3 && msg.Y <= m.height-1 { // Collapse/Expand toggle » / «
			return m, m.ToggleRightSidebar("")
		}
	}

	// 1.86. Right Sidebar Content & Header clicks
	if rightSideW > 0 && m.rightSidebarOpen {
		rightStartX := m.width - stripRightW - rightSideW
		editorH := m.height - 1 - editorTop
		if m.terminal != nil && m.terminal.Open {
			editorH = m.height - 1 - m.terminal.Height - editorTop
		}
		if msg.X >= rightStartX && msg.X < rightStartX+rightSideW && msg.Y >= editorTop && msg.Y < editorTop+editorH {
			// Header row: mousewheel resize
			if msg.Y == editorTop {
				maxW := max(24, m.width*3/4)
				minW := 18
				if msg.Button == input.MouseWheelUp {
					m.rightSidebarWidth = min(maxW, m.rightSidebarWidth+2)
					m.rightSidebarTargetWidth = float64(m.rightSidebarWidth)
					m.rightSidebarAnimWidth = float64(m.rightSidebarWidth)
					return m, nil
				} else if msg.Button == input.MouseWheelDown {
					m.rightSidebarWidth = max(minW, m.rightSidebarWidth-2)
					m.rightSidebarTargetWidth = float64(m.rightSidebarWidth)
					m.rightSidebarAnimWidth = float64(m.rightSidebarWidth)
					return m, nil
				}
			}

			if msg.Button == input.MouseWheelUp {
				if m.chatPanel != nil && (m.rightSidebarMode == "ai-chat" || m.rightSidebarMode == "ai") {
					m.chatPanel.ScrollOffset += 3
					return m, nil
				}
				if m.logPanel != nil && (m.rightSidebarMode == "log-viewer" || m.rightSidebarMode == "logs") {
					m.logPanel.HandleMouse(msg.Mouse, rightStartX, editorTop+2, rightSideW, editorH-2)
					return m, nil
				}
				if m.todoPanel != nil && (m.rightSidebarMode == "todo-tree" || m.rightSidebarMode == "todo") {
					m.todoPanel.Offset = max(0, m.todoPanel.Offset-3)
					return m, nil
				}
				if m.testRunnerPanel != nil && (m.rightSidebarMode == "test-runner" || m.rightSidebarMode == "tests") {
					m.testRunnerPanel.Offset = max(0, m.testRunnerPanel.Offset-3)
					return m, nil
				}
			} else if msg.Button == input.MouseWheelDown {
				if m.chatPanel != nil && (m.rightSidebarMode == "ai-chat" || m.rightSidebarMode == "ai") {
					if m.chatPanel.ScrollOffset > 3 {
						m.chatPanel.ScrollOffset -= 3
					} else {
						m.chatPanel.ScrollOffset = 0
					}
					return m, nil
				}
				if m.logPanel != nil && (m.rightSidebarMode == "log-viewer" || m.rightSidebarMode == "logs") {
					m.logPanel.HandleMouse(msg.Mouse, rightStartX, editorTop+2, rightSideW, editorH-2)
					return m, nil
				}
				if m.todoPanel != nil && (m.rightSidebarMode == "todo-tree" || m.rightSidebarMode == "todo") {
					m.todoPanel.Offset += 3
					return m, nil
				}
				if m.testRunnerPanel != nil && (m.rightSidebarMode == "test-runner" || m.rightSidebarMode == "tests") {
					m.testRunnerPanel.Offset += 3
					return m, nil
				}
			}
			if msg.Action == input.MousePress && msg.Button == input.MouseLeft {
				// Header buttons at editorTop: [◀] [▶] [×]
				if msg.Y == editorTop {
					// Close button × at top right
					if msg.X >= rightStartX+rightSideW-2 {
						return m, m.ToggleRightSidebar(m.rightSidebarMode)
					}
					// Resize buttons: ◀ (wider) and ▶ (narrower)
					if rightSideW >= 18 {
						maxW := max(24, m.width*3/4)
						minW := 18
						if msg.X == rightStartX+rightSideW-6 || msg.X == rightStartX+rightSideW-5 { // ◀ (widen left)
							m.rightSidebarWidth = min(maxW, m.rightSidebarWidth+2)
							m.rightSidebarTargetWidth = float64(m.rightSidebarWidth)
							m.rightSidebarAnimWidth = float64(m.rightSidebarWidth)
							return m, nil
						}
						if msg.X == rightStartX+rightSideW-4 || msg.X == rightStartX+rightSideW-3 { // ▶ (narrow right)
							m.rightSidebarWidth = max(minW, m.rightSidebarWidth-2)
							m.rightSidebarTargetWidth = float64(m.rightSidebarWidth)
							m.rightSidebarAnimWidth = float64(m.rightSidebarWidth)
							return m, nil
						}
					}
				}
				// Interactive action buttons inside right sidebar
				switch m.rightSidebarMode {
				case "ai-chat", "ai":
					if m.chatPanel != nil {
						if m.chatPanel.HandleClick(msg.X, msg.Y, rightStartX, editorTop, rightSideW, editorH) {
							return m, nil
						}
					}
					return m, nil
				case "db-inspector", "db":
					inspEnabled := true
					erdEnabled := true
					if m.pluginMgr != nil {
						inspEnabled = m.pluginMgr.IsEnabled("db-inspector")
						erdEnabled = m.pluginMgr.IsEnabled("db-er-diagram")
					}
					offsetY := 0
					if inspEnabled && erdEnabled {
						offsetY = 2
						// Tab header click at editorTop+2 (contentTop)
						if msg.Y == editorTop+2 {
							t1Len := 10 // " Таблицы "
							if msg.X >= rightStartX+1 && msg.X <= rightStartX+1+t1Len {
								m.dbSidebarTab = "tables"
								return m, nil
							}
							if msg.X >= rightStartX+1+t1Len+1 && msg.X <= rightStartX+rightSideW-1 {
								m.dbSidebarTab = "er-diagram"
								m.OpenERDTab()
								return m, nil
							}
						}
					}

					if m.dbSidebarTab == "er-diagram" {
						if msg.Y == editorTop+2+offsetY+3 { // Open ER Diagram Tab (F6)
							m.OpenERDTab()
							return m, nil
						}
						if msg.Y == editorTop+2+offsetY+4 { // Center Camera (C)
							if m.dagCanvasWidget != nil {
								m.dagCanvasWidget.PanX = 0
								m.dagCanvasWidget.PanY = 0
							}
							if m.toasts != nil {
								m.toasts.Info("ER DIAGRAM", "Camera centered")
							}
							return m, nil
						}
						if msg.Y == editorTop+2+offsetY+5 { // Export Mermaid ER
							schema := db.LoadHybridSchema(m.workspaceDir)
							mermaidCode := db.ExportMermaid(schema)
							_ = clipboard.Write(mermaidCode)
							if m.toasts != nil {
								m.toasts.Success("EXPORT", "Copied Mermaid ER diagram to clipboard")
							}
							return m, nil
						}
						if msg.Y == editorTop+2+offsetY+6 { // Export DDL Migration
							schema := db.LoadHybridSchema(m.workspaceDir)
							ddlCode := db.ExportFullDDL(schema)
							_ = clipboard.Write(ddlCode)
							if m.toasts != nil {
								m.toasts.Success("EXPORT", "Copied DDL schema migration to clipboard")
							}
							return m, nil
						}
					} else {
						// Tab "tables"
						if msg.Y == editorTop+2+offsetY+3 { // + Connect to Database...
							if m.dbConnectModal != nil {
								m.dbConnectModal.OpenDialog()
							}
							return m, nil
						}
						if msg.Y == editorTop+2+offsetY+4 { // Run Query Console
							m.OpenDBConsole()
							return m, nil
						}
						if msg.Y == editorTop+2+offsetY+5 { // Refresh Database Schema
							if m.toasts != nil {
								m.toasts.Info("DATABASE", "Schema refreshed")
							}
							return m, nil
						}
						// Table item click: open DataGrid
						for _, hb := range m.dbTableHitboxes {
							if msg.Y >= hb.startY && msg.Y <= hb.endY {
								m.OpenTableDataGrid(hb.tableName)
								return m, nil
							}
						}
					}
			case "project-graphs", "graphs":
				if msg.Y == editorTop+5 { // Open Interactive Canvas (F3)
					m.OpenProjectGraphInSplit()
					return m, nil
				}
				if msg.Y == editorTop+6 { // Cycle Hierarchy Mode
					if m.projectGraphPanel != nil {
						m.projectGraphPanel.CycleMode()
						m.statusMessage = fmt.Sprintf("Graph Mode: %s", m.projectGraphPanel.ModeTitle())
					}
					return m, nil
				}
			case "docker":
				if m.dockerPanel != nil {
					if m.dockerPanel.HandleClick(msg.X, msg.Y) {
						return m, nil
					}
				}
			case "rest-client", "rest":
				if m.restClientPanel != nil {
					if m.restClientPanel.HandleClick(msg.X, msg.Y) {
						return m, nil
					}
				}
			case "grpc":
				if m.grpcPanel != nil {
					rect := buffer.NewRect(rightStartX, editorTop+2, rightSideW, editorH-2)
					if m.grpcPanel.HandleClick(msg.X, msg.Y, rect) {
						return m, nil
					}
				}
			case "log-viewer", "logs":
				if m.logPanel != nil {
					if m.logPanel.HandleMouse(msg.Mouse, rightStartX, editorTop+2, rightSideW, editorH-2) {
						return m, nil
					}
				}
			case "task-runner", "tasks":
				if m.taskPanel != nil {
					if m.taskPanel.HandleMouse(msg.X, msg.Y, msg.Action, msg.Button) {
						return m, nil
					}
				}
			case "todo-tree", "todo":
				if m.todoPanel != nil {
					if m.todoPanel.HandleClick(msg.X, msg.Y) {
						return m, nil
					}
				}
			case "test-runner", "tests":
				if m.testRunnerPanel != nil {
					if m.testRunnerPanel.HandleClick(msg.X, msg.Y) {
						return m, nil
					}
				}
			case "jupyter-notebook", "jupyter":
				if m.jupyterPanel != nil {
					if m.jupyterPanel.HandleClick(msg.X, msg.Y) {
						return m, nil
					}
				}
			case "p2p-collab", "collab", "p2p":
				if m.p2pPanel != nil {
					if m.p2pPanel.HandleClick(msg.X, msg.Y) {
						return m, nil
					}
				}
			}
			return m, nil
		}
	}
}

	// 2. Mouse click inside Output Drawer
	if m.outputOpen {
		drawerH := m.outputHeight
		drawerTop := m.height - 1 - drawerH
		if msg.Y >= drawerTop && msg.Y < m.height-1 && msg.Action == input.MousePress {
			if msg.Y == drawerTop && msg.X >= m.width-12 {
				m.outputOpen = false
				return m, nil
			}
			return m, nil
		}
	}

	// 3. Mouse click inside Sidebar (Explorer or Structure)
	sidebarMaxY := editorTop + 1 + m.visibleTreeHeight()
	if sideW > 0 && msg.X >= treeStartX && msg.X < treeStartX+sideW && msg.Y >= editorTop && msg.Y < sidebarMaxY {
		if msg.Action == input.MousePress && msg.Button == input.MouseLeft {
			m.terminalFocused = false
			rowInSidebar := msg.Y - editorTop - 1 // -1 for header row
			if rowInSidebar >= 0 && rowInSidebar < m.visibleTreeHeight() {
				if m.sidebarMode == 1 {
					if m.structurePanel != nil {
						m.sidebarFocused = true
						if jumpTarget := m.structurePanel.HandleClick(rowInSidebar); jumpTarget != nil {
							m.jumpToLine(jumpTarget.Line, jumpTarget.Col)
							m.statusMessage = fmt.Sprintf("Jumped to %s (line %d)", jumpTarget.Name, jumpTarget.Line+1)
						}
					}
					return m, nil
				}
				m.clampSidebar()
				nodeIdx := m.sidebarScrollY + rowInSidebar
				if nodeIdx >= 0 && nodeIdx < len(m.treeFlat) {
					m.treeSel = nodeIdx
					m.sidebarFocused = true
					node := m.treeFlat[nodeIdx]
					if node.IsDir {
						node.IsOpen = !node.IsOpen
						m.treeFlat = make([]*FileNode, 0)
						FlattenTree(m.treeRoot, &m.treeFlat)
						m.clampSidebar()
					} else {
						_, err := m.eng.Open(node.Path)
						if err == nil {
							m.statusMessage = fmt.Sprintf("Opened %s", node.Name)
							m.sidebarFocused = false
							m.ensureCursorVisible()
							m.onActiveDocumentChanged()
						}
					}
				}
			}
			return m, nil
		}
	}

	// 4. Mouse wheel scrolling (Accelerated 5 to 12 lines per notch)
	if msg.Button == input.MouseWheelLeft || (msg.Button == input.MouseWheelUp && msg.HasShift()) {
		if doc := m.eng.ActiveDocument(); doc != nil && (filepath.Base(doc.FilePath) == "project.graph" || strings.HasSuffix(doc.FilePath, ".graph")) {
			if m.projectGraphPanel != nil && m.projectGraphPanel.Canvas != nil {
				m.projectGraphPanel.Canvas.Pan(4, 0)
			}
			return m, nil
		}
		if doc := m.eng.ActiveDocument(); doc != nil && (filepath.Base(doc.FilePath) == "schema.erd" || strings.HasSuffix(doc.FilePath, ".erd")) {
			if m.dagCanvasWidget != nil {
				m.dagCanvasWidget.Pan(4, 0)
			}
			return m, nil
		}
		if m.viewportX > 0 {
			m.viewportX -= 4
			if m.viewportX < 0 {
				m.viewportX = 0
			}
		}
		return m, nil
	}
	if msg.Button == input.MouseWheelRight || (msg.Button == input.MouseWheelDown && msg.HasShift()) {
		if doc := m.eng.ActiveDocument(); doc != nil && (filepath.Base(doc.FilePath) == "project.graph" || strings.HasSuffix(doc.FilePath, ".graph")) {
			if m.projectGraphPanel != nil && m.projectGraphPanel.Canvas != nil {
				m.projectGraphPanel.Canvas.Pan(-4, 0)
			}
			return m, nil
		}
		if doc := m.eng.ActiveDocument(); doc != nil && (filepath.Base(doc.FilePath) == "schema.erd" || strings.HasSuffix(doc.FilePath, ".erd")) {
			if m.dagCanvasWidget != nil {
				m.dagCanvasWidget.Pan(-4, 0)
			}
			return m, nil
		}
		m.viewportX += 4
		return m, nil
	}

	linesToScroll := 6
	now := time.Now()
	if now.Sub(m.lastWheelTime) < 160*time.Millisecond {
		linesToScroll = 16
	}
	m.lastWheelTime = now

	if msg.Button == input.MouseWheelUp {
		// Terminal header scroll: adjust height
		if m.terminal != nil && m.terminal.Open && msg.Y == m.height-1-m.terminal.Height {
			maxH := max(4, m.height-6)
			m.terminal.Height = min(maxH, m.terminal.Height+2)
			return m, nil
		}
		// Terminal body scroll: scroll terminal history
		if m.terminal != nil && m.terminal.Open && msg.Y > m.height-1-m.terminal.Height && msg.Y < m.height-1 {
			m.terminal.VTerm.Scroll(3)
			return m, nil
		}
		sidebarMaxY := editorTop + 1 + m.visibleTreeHeight()
		if (m.sidebarFocused || (sideW > 0 && msg.X >= treeStartX && msg.X < treeStartX+sideW && msg.Y < sidebarMaxY)) && m.sidebarOpen {
			if m.sidebarMode == 1 && m.structurePanel != nil {
				if m.structurePanel.ScrollY > 0 {
					m.structurePanel.ScrollY = max(0, m.structurePanel.ScrollY-linesToScroll/2)
				}
			} else {
				m.sidebarScrollY -= linesToScroll / 2
				m.clampSidebar()
			}
		} else {
			if m.splits != nil && m.splits.TotalPanes() > 1 {
				if paneIdx := m.splits.FindPaneAt(msg.X, msg.Y); paneIdx >= 0 {
					if pane := m.splits.PaneAt(paneIdx); pane != nil {
						if pane.IsView() {
							if (pane.ViewID == "dag-canvas" || pane.ViewID == "db-canvas") && m.dagCanvasWidget != nil {
								m.dagCanvasWidget.Pan(0, linesToScroll)
							}
							if pane.ViewID == "project-graph" && m.projectGraphPanel != nil && m.projectGraphPanel.Canvas != nil {
								m.projectGraphPanel.Canvas.Pan(0, linesToScroll)
							}
							return m, nil
						}
						if pane.ViewportY > 0 {
							pane.ViewportY -= linesToScroll
							if pane.ViewportY < 0 {
								pane.ViewportY = 0
							}
						}
						pane.TargetViewportY = float64(pane.ViewportY)
						pane.SmoothScrollY = float64(pane.ViewportY)
						pane.ScrollVelocity = 0
						if paneIdx == m.splits.ActivePaneIndex() {
							m.viewportY = pane.ViewportY
						}
						return m, nil
					}
				}
			}
			if pane := m.splits.ActivePane(); pane != nil && pane.IsView() {
				if (pane.ViewID == "dag-canvas" || pane.ViewID == "db-canvas") && m.dagCanvasWidget != nil {
					m.dagCanvasWidget.Pan(0, linesToScroll)
				}
				if pane.ViewID == "project-graph" && m.projectGraphPanel != nil && m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.Pan(0, linesToScroll)
				}
				return m, nil
			}
			if doc := m.eng.ActiveDocument(); doc != nil {
				if filepath.Base(doc.FilePath) == "project.graph" || strings.HasSuffix(doc.FilePath, ".graph") {
					if m.projectGraphPanel != nil && m.projectGraphPanel.Canvas != nil {
						m.projectGraphPanel.Canvas.Pan(0, linesToScroll)
					}
					return m, nil
				}
				if filepath.Base(doc.FilePath) == "schema.erd" || strings.HasSuffix(doc.FilePath, ".erd") {
					if m.dagCanvasWidget != nil {
						m.dagCanvasWidget.Pan(0, linesToScroll)
					}
					return m, nil
				}
				if strings.HasSuffix(doc.FilePath, ".datagrid") {
					tblName := strings.TrimSuffix(filepath.Base(doc.FilePath), ".datagrid")
					grid := m.getOrCreateDataGrid(tblName)
					grid.SelectPrevRow()
					return m, nil
				}
			}
			if m.viewportY > 0 {
				m.viewportY -= linesToScroll
				if m.viewportY < 0 {
					m.viewportY = 0
				}
				if pane := m.splits.ActivePane(); pane != nil {
					pane.ViewportY = m.viewportY
					pane.TargetViewportY = float64(m.viewportY)
					pane.SmoothScrollY = float64(m.viewportY)
					pane.ScrollVelocity = 0
				}
			}
		}
		return m, nil
	}
	if msg.Button == input.MouseWheelDown {
		// Terminal header scroll: adjust height
		if m.terminal != nil && m.terminal.Open && msg.Y == m.height-1-m.terminal.Height {
			m.terminal.Height = max(4, m.terminal.Height-2)
			return m, nil
		}
		// Terminal body scroll: scroll terminal history
		if m.terminal != nil && m.terminal.Open && msg.Y > m.height-1-m.terminal.Height && msg.Y < m.height-1 {
			m.terminal.VTerm.Scroll(-3)
			return m, nil
		}
		sidebarMaxY := editorTop + 1 + m.visibleTreeHeight()
		if (m.sidebarFocused || (sideW > 0 && msg.X >= treeStartX && msg.X < treeStartX+sideW && msg.Y < sidebarMaxY)) && m.sidebarOpen {
			if m.sidebarMode == 1 && m.structurePanel != nil {
				m.structurePanel.ScrollY += linesToScroll / 2
			} else {
				m.sidebarScrollY += linesToScroll / 2
				m.clampSidebar()
			}
		} else {
			if m.splits != nil && m.splits.TotalPanes() > 1 {
				if paneIdx := m.splits.FindPaneAt(msg.X, msg.Y); paneIdx >= 0 {
					if pane := m.splits.PaneAt(paneIdx); pane != nil {
						if pane.IsView() {
							if (pane.ViewID == "dag-canvas" || pane.ViewID == "db-canvas") && m.dagCanvasWidget != nil {
								m.dagCanvasWidget.Pan(0, -linesToScroll)
							}
							if pane.ViewID == "project-graph" && m.projectGraphPanel != nil && m.projectGraphPanel.Canvas != nil {
								m.projectGraphPanel.Canvas.Pan(0, -linesToScroll)
							}
							return m, nil
						}
						doc := m.eng.ActiveDocument()
						if doc != nil && pane.ViewportY+linesToScroll < doc.Buffer.TotalLines() {
							pane.ViewportY += linesToScroll
						}
						pane.TargetViewportY = float64(pane.ViewportY)
						pane.SmoothScrollY = float64(pane.ViewportY)
						pane.ScrollVelocity = 0
						if paneIdx == m.splits.ActivePaneIndex() {
							m.viewportY = pane.ViewportY
						}
						return m, nil
					}
				}
			}
			if pane := m.splits.ActivePane(); pane != nil && pane.IsView() {
				if (pane.ViewID == "dag-canvas" || pane.ViewID == "db-canvas") && m.dagCanvasWidget != nil {
					m.dagCanvasWidget.Pan(0, -linesToScroll)
				}
				if pane.ViewID == "project-graph" && m.projectGraphPanel != nil && m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.Pan(0, -linesToScroll)
				}
				return m, nil
			}
			doc := m.eng.ActiveDocument()
			if doc != nil {
				if filepath.Base(doc.FilePath) == "project.graph" || strings.HasSuffix(doc.FilePath, ".graph") {
					if m.projectGraphPanel != nil && m.projectGraphPanel.Canvas != nil {
						m.projectGraphPanel.Canvas.Pan(0, -linesToScroll)
					}
					return m, nil
				}
				if filepath.Base(doc.FilePath) == "schema.erd" || strings.HasSuffix(doc.FilePath, ".erd") {
					if m.dagCanvasWidget != nil {
						m.dagCanvasWidget.Pan(0, -linesToScroll)
					}
					return m, nil
				}
				if strings.HasSuffix(doc.FilePath, ".datagrid") {
					tblName := strings.TrimSuffix(filepath.Base(doc.FilePath), ".datagrid")
					grid := m.getOrCreateDataGrid(tblName)
					grid.SelectNextRow()
					return m, nil
				}
			}
			if doc != nil && m.viewportY+linesToScroll < doc.Buffer.TotalLines() {
				m.viewportY += linesToScroll
				if pane := m.splits.ActivePane(); pane != nil {
					pane.ViewportY = m.viewportY
					pane.TargetViewportY = float64(m.viewportY)
					pane.SmoothScrollY = float64(m.viewportY)
					pane.ScrollVelocity = 0
				}
			}
		}
		return m, nil
	}

	// 5. Split Panes / Editor clicks & drag selection
	editorBottom := m.height - 1
	if m.outputOpen {
		drawerH := m.outputHeight
		if drawerH > m.height-5 {
			drawerH = max(3, m.height-5)
		}
		editorBottom -= drawerH
	}
	if m.terminal != nil && m.terminal.Open {
		termH := m.terminal.Height
		if termH > editorBottom-3 {
			termH = max(3, editorBottom-3)
		}
		editorBottom -= termH
	}
	if m.dapHUD != nil && m.dapHUD.Open {
		hudH := m.dapHUD.Height
		if hudH > editorBottom-3 {
			hudH = max(3, editorBottom-3)
		}
		editorBottom -= hudH
	}

	if msg.Button == input.MouseLeft && (msg.Action == input.MousePress || msg.Action == input.MouseDrag) &&
		msg.X >= editorLeft && msg.X < editorRight &&
		msg.Y >= editorTop && msg.Y < editorBottom {
		m.sidebarFocused = false
		m.terminalFocused = false

		if msg.Action == input.MousePress {
			// Check if click hit any visible color swatch in editor
			for _, swatch := range m.editorColorSwatches {
				if swatch.ScreenX == msg.X && swatch.ScreenY == msg.Y {
					m.openEditorColorPicker(swatch)
					return m, nil
				}
			}

			// Check if click occurred in any split pane
			if m.splits != nil && m.splits.TotalPanes() > 1 {
				for i := range m.splits.Panes {
					p := &m.splits.Panes[i]
					if p.Bounds.Width > 0 && p.Bounds.Height > 0 {
						// Mini header row click
						if msg.Y == p.Bounds.Y && msg.X >= p.Bounds.X && msg.X < p.Bounds.X+p.Bounds.Width {
							// Close button ✕ at top right of pane header
							if msg.X >= p.Bounds.X+p.Bounds.Width-3 {
								m.closeSplitPane(p.Index)
								return m, nil
							}
							m.switchActivePane(p.Index)
							return m, nil
						}
					}
				}
				clickedPaneIdx := m.splits.FindPaneAt(msg.X, msg.Y)
				if clickedPaneIdx >= 0 {
					m.switchActivePane(clickedPaneIdx)
				}
			}
		}

		targetPane := m.splits.ActivePane()
		pBounds := buffer.NewRect(editorLeft, editorTop, editorRight-editorLeft, m.height-3)
		if targetPane != nil && targetPane.Bounds.Width > 0 && targetPane.Bounds.Height > 0 {
			pBounds = targetPane.Bounds
		}

		isMulti := (m.splits != nil && m.splits.TotalPanes() > 1)
		contentY := pBounds.Y
		if isMulti {
			contentY += 1 // skip mini header row
		}
		contentX := pBounds.X

		clickRow := msg.Y - contentY
		clickCol := msg.X - contentX

		// If click occurred on the pane's mini header row or below bottom bound, ignore
		if clickRow < 0 || (pBounds.Height > 0 && clickRow >= pBounds.Height) {
			return m, nil
		}

		// Handle canvas panning on mouse drag
		if msg.Action == input.MouseDrag && m.canvasDragging {
			dx := msg.X - m.canvasDragStartX
			dy := msg.Y - m.canvasDragStartY
			m.canvasDragStartX = msg.X
			m.canvasDragStartY = msg.Y

			if targetPane != nil && targetPane.IsView() {
				if targetPane.ViewID == "project-graph" && m.projectGraphPanel != nil && m.projectGraphPanel.Canvas != nil {
					m.projectGraphPanel.Canvas.Pan(dx, dy)
					return m, nil
				}
				if (targetPane.ViewID == "dag-canvas" || targetPane.ViewID == "db-canvas") && m.dagCanvasWidget != nil {
					m.dagCanvasWidget.Pan(dx, dy)
					return m, nil
				}
			}
			if doc := m.eng.ActiveDocument(); doc != nil {
				if filepath.Base(doc.FilePath) == "project.graph" || strings.HasSuffix(doc.FilePath, ".graph") {
					if m.projectGraphPanel != nil && m.projectGraphPanel.Canvas != nil {
						m.projectGraphPanel.Canvas.Pan(dx, dy)
					}
					return m, nil
				}
				if filepath.Base(doc.FilePath) == "schema.erd" || strings.HasSuffix(doc.FilePath, ".erd") {
					if m.dagCanvasWidget != nil {
						m.dagCanvasWidget.Pan(dx, dy)
					}
					return m, nil
				}
			}
		}

		if msg.Action == input.MousePress {
			m.canvasDragging = true
			m.canvasDragStartX = msg.X
			m.canvasDragStartY = msg.Y
		}

		if targetPane != nil && targetPane.IsView() {
			if targetPane.ViewID == "project-graph" && m.projectGraphPanel != nil {
				// Click on top mode bar row (clickRow == 0)
				if clickRow == 0 {
					// Close button on right: ✕ Закрыть (Esc)
					if clickCol >= pBounds.Width-18 {
						m.closeSplitPane(targetPane.Index)
						return m, nil
					}
					// Cycle mode on click
					m.projectGraphPanel.CycleMode()
					docToUse := m.eng.ActiveDocument()
					if docToUse == nil && len(m.splits.Panes) > 0 {
						docToUse = m.findDocument(m.splits.Panes[0].DocID)
					}
					m.projectGraphPanel.RebuildWithLSP(docToUse, m.workspaceDir, m.lspClient)
					m.statusMessage = fmt.Sprintf("Graph Mode: %s", m.projectGraphPanel.ModeTitle())
					return m, nil
				}
				if m.projectGraphPanel.Canvas != nil && m.projectGraphPanel.Canvas.Model != nil {
					canvasX := clickCol - m.projectGraphPanel.Canvas.PanX
					canvasY := clickRow - 1 - m.projectGraphPanel.Canvas.PanY
					now := time.Now()
					for id, node := range m.projectGraphPanel.Canvas.Model.Nodes {
						if canvasX >= node.X && canvasX < node.X+node.Width &&
							canvasY >= node.Y && canvasY < node.Y+node.Height {
							if now.Sub(m.lastEditorClickTime) < 350*time.Millisecond && m.projectGraphPanel.Canvas.SelectedNodeID == id {
								m.jumpToSymbolFromGraph(id)
								return m, nil
							}
							m.projectGraphPanel.Canvas.SelectedNodeID = id
							m.lastEditorClickTime = now
							break
						}
					}
				}
				return m, nil
			}
			if (targetPane.ViewID == "dag-canvas" || targetPane.ViewID == "db-canvas") && m.dagCanvasWidget != nil && m.dagCanvasWidget.Model != nil {
				canvasX := clickCol - m.dagCanvasWidget.PanX
				canvasY := clickRow - m.dagCanvasWidget.PanY
				for id, node := range m.dagCanvasWidget.Model.Nodes {
					if canvasX >= node.X && canvasX < node.X+node.Width &&
						canvasY >= node.Y && canvasY < node.Y+node.Height {
						m.dagCanvasWidget.SelectedNodeID = id
						break
					}
				}
			}
			return m, nil
		}

		doc := m.eng.ActiveDocument()
		if doc == nil {
			return m, nil
		}
		if filepath.Base(doc.FilePath) == "project.graph" || strings.HasSuffix(doc.FilePath, ".graph") {
			if m.projectGraphPanel != nil {
				// Click on top mode bar row (clickRow == 0)
				if clickRow == 0 {
					// Close button on right: ✕ Закрыть (Esc)
					if clickCol >= pBounds.Width-18 {
						m.eng.CloseBuffer(doc.ID)
						m.statusMessage = "Closed Project Graph tab"
						return m, nil
					}
					// Cycle mode on click
					m.projectGraphPanel.CycleMode()
					var docToUse *core.Document
					for _, d := range m.eng.Documents() {
						b := filepath.Base(d.FilePath)
						if b != "project.graph" && !strings.HasSuffix(b, ".graph") && b != "schema.erd" && !strings.HasSuffix(b, ".erd") {
							docToUse = d
							break
						}
					}
					m.projectGraphPanel.RebuildWithLSP(docToUse, m.workspaceDir, m.lspClient)
					m.statusMessage = fmt.Sprintf("Graph Mode: %s", m.projectGraphPanel.ModeTitle())
					return m, nil
				}
				if m.projectGraphPanel.Canvas != nil && m.projectGraphPanel.Canvas.Model != nil {
					canvasX := clickCol - m.projectGraphPanel.Canvas.PanX
					canvasY := clickRow - 1 - m.projectGraphPanel.Canvas.PanY
					now := time.Now()
					for id, node := range m.projectGraphPanel.Canvas.Model.Nodes {
						if canvasX >= node.X && canvasX < node.X+node.Width &&
							canvasY >= node.Y && canvasY < node.Y+node.Height {
							if now.Sub(m.lastEditorClickTime) < 350*time.Millisecond && m.projectGraphPanel.Canvas.SelectedNodeID == id {
								m.jumpToSymbolFromGraph(id)
								return m, nil
							}
							m.projectGraphPanel.Canvas.SelectedNodeID = id
							m.lastEditorClickTime = now
							break
						}
					}
				}
			}
			return m, nil
		}
		if filepath.Base(doc.FilePath) == "schema.erd" || strings.HasSuffix(doc.FilePath, ".erd") {
			if m.dagCanvasWidget != nil && m.dagCanvasWidget.Model != nil {
				canvasX := clickCol - m.dagCanvasWidget.PanX
				canvasY := clickRow - m.dagCanvasWidget.PanY
				for id, node := range m.dagCanvasWidget.Model.Nodes {
					if canvasX >= node.X && canvasX < node.X+node.Width &&
						canvasY >= node.Y && canvasY < node.Y+node.Height {
						m.dagCanvasWidget.SelectedNodeID = id
						break
					}
				}
			}
			return m, nil
		}
		if strings.HasSuffix(doc.FilePath, ".datagrid") {
			base := filepath.Base(doc.FilePath)
			tblName := strings.TrimSuffix(base, ".datagrid")
			grid := m.getOrCreateDataGrid(tblName)
			if clickRow == 0 {
				if clickCol >= 40 && clickCol <= 50 {
					grid.PrevPage()
				} else if clickCol >= 51 && clickCol <= 62 {
					grid.NextPage()
				} else if clickCol >= 63 && clickCol <= 78 {
					if m.toasts != nil {
						m.toasts.Info("DATAGRID", fmt.Sprintf("Refreshed table %s", grid.TableName))
					}
				} else if clickCol >= 79 {
					grid.ExportCSV(m.workspaceDir)
					if m.toasts != nil {
						m.toasts.Success("EXPORT CSV", fmt.Sprintf("Exported table %s to CSV", grid.TableName))
					}
				}
			} else if clickRow >= 4 {
				rIdx := clickRow - 4
				pageRows := grid.CurrentPageRows()
				if rIdx >= 0 && rIdx < len(pageRows) {
					grid.SelectedRow = rIdx
				}
			}
			return m, nil
		}
		if IsImageFile(doc.FilePath) {
			if msg.Action == input.MousePress && msg.Button == input.MouseLeft {
				iv := m.getOrCreateImageViewer(doc.FilePath)
				iv.CycleScaleMode()
				m.statusMessage = fmt.Sprintf("Image Scale: %s", iv.ScaleModeTitle())
				m.toasts.Info("IMAGE", fmt.Sprintf("Scale Mode: %s", iv.ScaleModeTitle()))
				return m, nil
			}
			return m, nil
		}
		if doc.Buffer.TotalLines() <= 0 {
			return m, nil
		}

		gutterWidth := m.gutterWidth()
		if pBounds.Width > 0 && gutterWidth > pBounds.Width-2 {
			gutterWidth = max(2, pBounds.Width-2)
		}

		vpY := m.viewportY
		vpX := m.viewportX
		if targetPane != nil {
			vpY = targetPane.ViewportY
			vpX = targetPane.ViewportX
		}

		// Gutter click: toggle breakpoint (only on MousePress)
		if clickCol >= 0 && clickCol < gutterWidth {
			if msg.Action == input.MousePress {
				lineIdx := vpY + clickRow
				if lineIdx >= 0 && lineIdx < doc.Buffer.TotalLines() {
					m.breakpoints[lineIdx] = !m.breakpoints[lineIdx]
					if !m.breakpoints[lineIdx] {
						delete(m.breakpoints, lineIdx)
						m.toasts.Info("BREAKPOINT", fmt.Sprintf("Removed at line %d", lineIdx+1))
					} else {
						m.toasts.Success("BREAKPOINT", fmt.Sprintf("Set at line %d", lineIdx+1))
					}
					if m.dapSession != nil && doc.FilePath != "" {
						var bpLines []int
						for bl := range m.breakpoints {
							bpLines = append(bpLines, bl)
						}
						m.dapSession.SetBreakpoints(doc.FilePath, bpLines)
					}
				}
			}
			return m, nil
		}

		// Text area click / drag
		targetLine := vpY + clickRow
		if targetLine < 0 {
			targetLine = 0
		}
		if targetLine >= doc.Buffer.TotalLines() {
			targetLine = doc.Buffer.TotalLines() - 1
		}

		targetVisualCol := vpX + (clickCol - gutterWidth)
		if targetVisualCol < 0 {
			targetVisualCol = 0
		}

		lineBytes, _ := doc.Buffer.GetLine(targetLine)
		runes := []rune(string(lineBytes))
		runesLen := len(runes)
		if runesLen > 0 && runes[runesLen-1] == '\n' {
			runesLen--
		}
		if runesLen > 0 && runes[runesLen-1] == '\r' {
			runesLen--
		}

		tabSize := 4
		if m.settings != nil && m.settings.Current.TabSize > 0 {
			tabSize = m.settings.Current.TabSize
		}
		runeCol := corebuf.LineVisualToRuneCol(lineBytes, targetVisualCol, tabSize)
		if runeCol > runesLen {
			runeCol = runesLen
		}

		targetByte, _ := doc.Buffer.ByteOffsetForLine(targetLine)
		if targetByte < 0 {
			targetByte = 0
		}
		for i := 0; i < runeCol && i < len(runes); i++ {
			targetByte += len(string(runes[i]))
		}

		newPos := corebuf.Position{
			Line:   targetLine,
			Column: runeCol,
			Byte:   targetByte,
		}

		m.terminalFocused = false
		m.sidebarFocused = false

		if msg.Action == input.MousePress && msg.Button == input.MouseLeft {
			now := time.Now()
			colDiff := m.lastEditorClickCol - runeCol
			if colDiff < 0 {
				colDiff = -colDiff
			}
			isDoubleClick := (now.Sub(m.lastEditorClickTime) < 400*time.Millisecond &&
				m.lastEditorClickLine == targetLine &&
				colDiff <= 1)
			m.lastEditorClickTime = now
			m.lastEditorClickLine = targetLine
			m.lastEditorClickCol = runeCol

			// Ctrl + Click: Go to Definition
			if msg.HasCtrl() {
				doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(newPos, newPos)})
				m.ensureCursorVisible()
				m.gotoDefinition()
				return m, nil
			}

			if isDoubleClick && !msg.HasShift() && !msg.HasAlt() && len(runes) > 0 {
				col := runeCol
				if col >= len(runes) {
					col = len(runes) - 1
				}
				isWordChar := func(r rune) bool {
					return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
				}
				startCol := col
				endCol := col
				if isWordChar(runes[col]) {
					for startCol > 0 && isWordChar(runes[startCol-1]) {
						startCol--
					}
					for endCol < len(runes) && isWordChar(runes[endCol]) {
						endCol++
					}
				} else if !unicode.IsSpace(runes[col]) {
					for startCol > 0 && !isWordChar(runes[startCol-1]) && !unicode.IsSpace(runes[startCol-1]) {
						startCol--
					}
					for endCol < len(runes) && !isWordChar(runes[endCol]) && !unicode.IsSpace(runes[endCol]) {
						endCol++
					}
				}

				startByte, _ := doc.Buffer.ByteOffsetForLine(targetLine)
				if startByte < 0 {
					startByte = 0
				}
				for i := 0; i < startCol && i < len(runes); i++ {
					startByte += len(string(runes[i]))
				}
				endByte := startByte
				for i := startCol; i < endCol && i < len(runes); i++ {
					endByte += len(string(runes[i]))
				}
				startPos := corebuf.Position{Line: targetLine, Column: startCol, Byte: startByte}
				endPos := corebuf.Position{Line: targetLine, Column: endCol, Byte: endByte}
				doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(startPos, endPos)})
				m.ensureCursorVisible()
				return m, nil
			}
		}

		if msg.Action == input.MouseDrag {
			// Mouse drag selection: keep anchor of primary cursor and extend head
			if sels := doc.Buffer.GetSelections(); len(sels) > 0 {
				anchor := sels[0].Anchor
				doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(anchor, newPos)})
			} else {
				doc.Buffer.SetSelections([]corebuf.Selection{corebuf.NewSelection(newPos, newPos)})
			}
		} else {
			newSel := corebuf.NewSelection(newPos, newPos)
			if msg.HasShift() && len(doc.Buffer.GetSelections()) > 0 {
				anchor := doc.Buffer.GetSelections()[0].Anchor
				extendedSel := corebuf.NewSelection(anchor, newPos)
				doc.Buffer.SetSelections([]corebuf.Selection{extendedSel})
			} else if msg.HasAlt() {
				// Alt+Click: Add multi-cursor
				doc.Buffer.SetSelections(append(doc.Buffer.GetSelections(), newSel))
			} else {
				// Regular click: Set single cursor
				doc.Buffer.SetSelections([]corebuf.Selection{newSel})
			}
		}
		m.ensureCursorVisible()
	}

	return m, nil
}

// updateTooltip computes hover badge text based on cursor position.
func (m *AppModel) updateTooltip(x, y int) {
	editorTop := 2
	stripLeftW := 0
	stripRightW := 0
	if m.width >= 70 {
		stripLeftW = 3
		stripRightW = 3
	}

	// 1. Row 0 - Header Toolbar
	if y == 0 {
		for _, btn := range m.toolbarButtons {
			if x >= btn.minX && x <= btn.maxX {
				switch btn.id {
				case "menu":
					m.setTooltip(i18n.T("tooltip.menu"), x, y)
				case "find_files":
					m.setTooltip(i18n.T("tooltip.find_files"), x, y)
				case "split":
					title := "Single Pane"
					if m.splits != nil {
						title = splitModeTitle(m.splits)
					}
					m.setTooltip(fmt.Sprintf(i18n.T("tooltip.split"), title), x, y)
				case "run":
					m.setTooltip(i18n.T("tooltip.run"), x, y)
				case "build":
					m.setTooltip(i18n.T("tooltip.build"), x, y)
				case "term":
					m.setTooltip(i18n.T("tooltip.term"), x, y)
				case "hud":
					m.setTooltip(i18n.T("tooltip.hud"), x, y)
				case "profile_select":
					m.setTooltip(i18n.T("tooltip.profile_select"), x, y)
				default:
					m.setTooltip(btn.id, x, y)
				}
				return
			}
		}
		m.setTooltip(i18n.T("tooltip.app_version"), x, y)
		return
	}

	// 2. Row 1 - Tab Bar
	if y == 1 {
		for _, hit := range m.tabHitboxes {
			if x >= hit.closeX-1 && x <= hit.closeX+1 {
				m.setTooltip(i18n.T("tooltip.tab_close"), x, y)
				return
			} else if x >= hit.minX && x <= hit.maxX {
				docName := filepath.Base(hit.docID)
				m.setTooltip(fmt.Sprintf(i18n.T("tooltip.tab_switch"), docName), x, y)
				return
			}
		}
	}

	// 3. Side Activity Strip (Left Strip)
	if stripLeftW > 0 && x < stripLeftW {
		switch y {
		case 2:
			m.setTooltip(i18n.T("tooltip.side_explorer"), x, y)
			return
		case 4:
			m.setTooltip(i18n.T("tooltip.side_outline"), x, y)
			return
		case 6:
			m.setTooltip(i18n.T("tooltip.side_git"), x, y)
			return
		case 8:
			m.setTooltip(i18n.T("tooltip.side_plugins"), x, y)
			return
		default:
			if y >= m.height-3 && y <= m.height-1 {
				m.setTooltip(i18n.T("tooltip.side_settings"), x, y)
				return
			}
		}
	}

	// 3.1. Secondary Activity Strip (Right Strip)
	if stripRightW > 0 && x >= m.width-stripRightW {
		switch y {
		case 2:
			m.setTooltip("AI Assistant (Ctrl+L)", x, y)
			return
		case 4:
			m.setTooltip("Database Inspector & DAG Canvas (F6)", x, y)
			return
		case 6:
			m.setTooltip("Project Graphs & Call Hierarchy (F3)", x, y)
			return
		default:
			if y >= 8 && y < m.height-3 && y%2 == 0 {
				idx := (y - 8) / 2
				var activeRightTools []plugin.ActiveToolWindow
				if m.pluginMgr != nil {
					for _, tw := range m.pluginMgr.ActiveToolWindows() {
						if tw.Position == "right" {
							activeRightTools = append(activeRightTools, tw)
						}
					}
				}
				if idx >= 0 && idx < len(activeRightTools) {
					m.setTooltip(activeRightTools[idx].Title, x, y)
					return
				}
			}
			if y >= m.height-3 && y <= m.height-1 {
				m.setTooltip("Toggle Secondary Sidebar (Ctrl+Alt+B)", x, y)
				return
			}
		}
	}

	// 3.5. Project Tree Header Dock Toggle [⇄]
	sideW := 0
	if m.sidebarOpen {
		sideW = m.sidebarWidth
	}
	isTreeRight := (m.settings != nil && m.settings.Current.TreePosition == "right")
	treeStartX := stripLeftW
	if isTreeRight {
		treeStartX = m.width - sideW
	}
	if sideW >= 18 && y == editorTop {
		dockBtnStart := treeStartX + sideW - 5
		if x >= dockBtnStart && x <= dockBtnStart+1 {
			m.setTooltip(i18n.T("tooltip.tree_dec_width"), x, y)
			return
		}
		if x >= dockBtnStart+2 && x <= dockBtnStart+3 {
			m.setTooltip(i18n.T("tooltip.tree_inc_width"), x, y)
			return
		}
		if x >= dockBtnStart+4 && x < treeStartX+sideW {
			m.setTooltip(i18n.T("tooltip.tree_dock"), x, y)
			return
		}
	} else if sideW > 0 && y == editorTop && x >= treeStartX+sideW-2 && x < treeStartX+sideW {
		m.setTooltip(i18n.T("tooltip.tree_dock"), x, y)
		return
	}

	// 4. Gutter
	gutterWidth := m.gutterWidth()
	editorLeft := stripLeftW
	if !isTreeRight && m.sidebarOpen {
		editorLeft = stripLeftW + m.sidebarWidth + 1
	}
	if x >= editorLeft && x < editorLeft+gutterWidth && y >= editorTop && y < m.height-1 {
		line := m.viewportY + (y - editorTop) + 1
		m.setTooltip(fmt.Sprintf(i18n.T("tooltip.breakpoint"), line), x, y)
		return
	}

	m.tooltipText = ""
}

func (m *AppModel) setTooltip(text string, x, y int) {
	m.tooltipText = text
	m.tooltipX = x + 1
	if y == 0 {
		m.tooltipY = 2
	} else {
		m.tooltipY = y + 1
	}
	textW := buffer.StringWidth(text)
	if m.tooltipX+textW+2 >= m.width {
		m.tooltipX = max(0, m.width-textW-2)
	}
	if m.tooltipY >= m.height-1 {
		m.tooltipY = max(0, y-1)
	}
}

// updateHoverDoc checks if mouse is hovering over an identifier/symbol in the editor area and schedules hover documentation.
func (m *AppModel) updateHoverDoc(screenX, screenY int) {
	if m.hoverDoc == nil {
		return
	}

	editorTop := 2
	stripLeftW := 0
	if m.width >= 70 {
		stripLeftW = 3
	}
	sideW := 0
	if m.sidebarOpen {
		sideW = m.sidebarWidth
	}
	isTreeRight := (m.settings != nil && m.settings.Current.TreePosition == "right")
	editorLeft := stripLeftW
	editorRight := m.width
	if isTreeRight && m.sidebarOpen {
		editorRight = m.width - sideW
	} else if m.sidebarOpen {
		editorLeft = stripLeftW + sideW
	}
	editorBottom := m.height - 1
	if m.terminal != nil && m.terminal.Open {
		editorBottom = m.height - 1 - m.terminal.Height
	}

	// If mouse is outside editor area
	if screenX < editorLeft || screenX >= editorRight || screenY < editorTop || screenY >= editorBottom {
		m.hoverDoc.Dismiss()
		return
	}

	// Find active split pane and document
	pane := m.splits.ActivePane()
	if m.splits != nil && m.splits.TotalPanes() > 1 {
		pIdx := m.splits.FindPaneAt(screenX, screenY)
		if pIdx >= 0 {
			pane = m.splits.PaneAt(pIdx)
		}
	}
	doc := m.eng.ActiveDocument()
	if pane != nil && pane.DocID != "" {
		for _, d := range m.eng.Documents() {
			if d.ID == pane.DocID {
				doc = d
				break
			}
		}
	}
	if doc == nil || doc.Buffer.TotalLines() <= 0 {
		m.hoverDoc.Dismiss()
		return
	}

	pBounds := buffer.NewRect(editorLeft, editorTop, editorRight-editorLeft, editorBottom-editorTop)
	if pane != nil && pane.Bounds.Width > 0 && pane.Bounds.Height > 0 {
		pBounds = pane.Bounds
	}
	contentY := pBounds.Y
	if m.splits != nil && m.splits.TotalPanes() > 1 {
		contentY += 1
	}
	clickRow := screenY - contentY
	clickCol := screenX - pBounds.X
	gutterW := m.gutterWidth()
	if clickCol < gutterW || clickRow < 0 {
		m.hoverDoc.Dismiss()
		return
	}

	vpY := m.viewportY
	vpX := m.viewportX
	if pane != nil {
		vpY = pane.ViewportY
		vpX = pane.ViewportX
	}

	docLine := vpY + clickRow
	if docLine < 0 || docLine >= doc.Buffer.TotalLines() {
		m.hoverDoc.Dismiss()
		return
	}

	lineBytes, err := doc.Buffer.GetLine(docLine)
	if err != nil {
		m.hoverDoc.Dismiss()
		return
	}

	tabSize := 4
	if m.settings != nil && m.settings.Current.TabSize > 0 {
		tabSize = m.settings.Current.TabSize
	}
	targetVisualCol := clickCol - gutterW + vpX
	runeCol := corebuf.LineVisualToRuneCol(lineBytes, targetVisualCol, tabSize)
	runes := []rune(string(lineBytes))
	if len(runes) == 0 {
		m.hoverDoc.Dismiss()
		return
	}
	if runeCol >= len(runes) {
		runeCol = len(runes) - 1
	}

	word := extractWordAtRuneCol(runes, runeCol)
	if word == "" {
		m.hoverDoc.Dismiss()
		return
	}

	// 400ms delay per user requirement
	m.hoverDoc.Start(word, docLine, runeCol, screenX, screenY, 400*time.Millisecond)
}

func (m *AppModel) checkHoverDocTrigger(now time.Time) {
	if m.hoverDoc == nil || m.hoverDoc.Open || m.hoverDoc.TriggerTime.IsZero() {
		return
	}
	if now.Before(m.hoverDoc.TriggerTime) {
		return
	}

	word := m.hoverDoc.PendingWord
	line := m.hoverDoc.PendingLine
	col := m.hoverDoc.PendingCol
	doc := m.eng.ActiveDocument()
	if doc == nil || word == "" {
		m.hoverDoc.Dismiss()
		return
	}

	m.hoverDoc.Symbol = word
	m.hoverDoc.ScreenX = m.hoverDoc.PendingX
	m.hoverDoc.ScreenY = m.hoverDoc.PendingY

	// 1. Try LSP Hover if client is connected
	var foundLSP bool
	if m.lspClient != nil && doc.FilePath != "" {
		uri := "file:///" + filepath.ToSlash(doc.FilePath)
		h, err := m.lspClient.Hover(uri, line, col)
		if err == nil && h != nil && h.Contents != nil {
			var docStr string
			switch val := h.Contents.(type) {
			case string:
				docStr = val
			case map[string]any:
				if v, ok := val["value"].(string); ok {
					docStr = v
				}
			default:
				bytes, _ := json.Marshal(val)
				docStr = string(bytes)
			}
			lines := strings.Split(docStr, "\n")
			var cleanLines []string
			for _, l := range lines {
				l = strings.TrimSpace(l)
				if l != "" && !strings.HasPrefix(l, "```") {
					cleanLines = append(cleanLines, l)
				}
			}
			if len(cleanLines) > 0 {
				m.hoverDoc.Signature = cleanLines[0]
				if len(cleanLines) > 1 {
					m.hoverDoc.DocLines = cleanLines[1:]
				}
				m.hoverDoc.Source = "LSP"
				foundLSP = true
			}
		}
	}

	// 2. Fallback: Local AST / comments or Image preview
	if !foundLSP {
		ext := strings.ToLower(filepath.Ext(doc.FilePath))
		isLanguageInactive := ext != "" && m.pluginMgr != nil && !m.pluginMgr.IsExtensionActive(ext)
		if !isLanguageInactive {
			sig, docs, thumb, src := ExtractLocalSymbolDocumentation(doc, word, m.workspaceDir)
			if sig != "" || len(docs) > 0 || thumb != nil {
				m.hoverDoc.Signature = sig
				m.hoverDoc.DocLines = docs
				m.hoverDoc.Thumbnail = thumb
				m.hoverDoc.Source = src
			} else {
				m.hoverDoc.Dismiss()
				return
			}
		} else {
			// Plain text or disabled language plugin: only image previews are supported
			sig, docs, thumb, src := ExtractLocalSymbolDocumentation(doc, word, m.workspaceDir)
			if thumb != nil {
				m.hoverDoc.Signature = sig
				m.hoverDoc.DocLines = docs
				m.hoverDoc.Thumbnail = thumb
				m.hoverDoc.Source = src
			} else {
				m.hoverDoc.Dismiss()
				return
			}
		}
	}

	m.hoverDoc.Open = true
	m.hoverDoc.TriggerTime = time.Time{}
}

func extractWordAtRuneCol(runes []rune, col int) string {
	if len(runes) == 0 || col < 0 || col >= len(runes) {
		return ""
	}

	// Check if cursor is on an image path or string literal e.g. "path/to/img.png"
	qStart := col
	for qStart > 0 && runes[qStart-1] != '"' && runes[qStart-1] != '\'' && runes[qStart-1] != '(' && runes[qStart-1] != '`' && runes[qStart-1] != ' ' {
		qStart--
	}
	qEnd := col
	for qEnd < len(runes) && runes[qEnd] != '"' && runes[qEnd] != '\'' && runes[qEnd] != ')' && runes[qEnd] != '`' && runes[qEnd] != ' ' {
		qEnd++
	}
	candidatePath := string(runes[qStart:qEnd])
	if IsImageFile(candidatePath) {
		return candidatePath
	}

	isWordChar := func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
	}
	if !isWordChar(runes[col]) {
		return ""
	}
	start := col
	for start > 0 && isWordChar(runes[start-1]) {
		start--
	}
	end := col
	for end < len(runes) && isWordChar(runes[end]) {
		end++
	}
	return string(runes[start:end])
}

// executeContextMenuAction dispatches right-click menu actions.
func (m *AppModel) executeContextMenuAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "new_file":
		m.openTreePrompt("new_file")
	case "new_dir":
		m.openTreePrompt("new_dir")
	case "rename":
		m.openTreePrompt("rename")
	case "move":
		m.openTreePrompt("move")
	case "copy_path":
		target := m.contextMenuTarget
		if target == "" && len(m.treeFlat) > 0 && m.treeSel < len(m.treeFlat) {
			target = m.treeFlat[m.treeSel].Path
		}
		if target != "" {
			rel, err := filepath.Rel(m.workspaceDir, target)
			if err != nil {
				rel = target
			}
			_ = clipboard.Write(rel)
			m.clipboardText = rel
			m.toasts.Success("COPIED", rel)
		}
	case "delete":
		m.openTreePrompt("delete")
	case "refresh":
		m.refreshProjectTree()
		m.toasts.Info("TREE", "Explorer refreshed")
	}
	return m, nil
}

